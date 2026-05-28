package notifysummary

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// SessionPromptSender is the outbound interface the dispatcher uses to
// deliver rendered prompts. multica's CCConnectClient satisfies it; tests
// supply a fake.
type SessionPromptSender interface {
	SendSessionPrompt(ctx context.Context, userID, prompt string) error
}

// Dispatcher batches inbox notifications per (staff_id, issue_id) bucket
// with idle + max-wait timers. On flush it renders the bucket's settings
// template and POSTs the prompt to cc-connect via the injector.
//
// Modeled on server/internal/handler/heartbeat_scheduler.go (per-key
// coalescing batcher with explicit Stop drain).
type Dispatcher struct {
	injector SessionPromptSender

	mu      sync.Mutex
	buckets map[string]*bucket

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopped  bool

	// now is overridable for tests; production uses time.Now.
	now func() time.Time
}

type bucket struct {
	key           string
	staffID       string
	issueID       string
	sessionKey    string // optional; recorded as "" until a session is found
	settings      Settings
	notifications []QueuedNotification
	idleTimer     *time.Timer
	maxTimer      *time.Timer
	createdAt     time.Time
}

// NewDispatcher returns a ready-to-use Dispatcher. Stop() should be called
// during graceful shutdown after the HTTP server has drained so no late
// Enqueue can race the final flush.
func NewDispatcher(injector SessionPromptSender) *Dispatcher {
	return &Dispatcher{
		injector: injector,
		buckets:  make(map[string]*bucket),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		now:      time.Now,
	}
}

// Enqueue adds n to the bucket for (staffID, issueID). First enqueue creates
// the bucket and arms a max-wait timer (settings.MaxWaitSecs). Every enqueue
// resets the idle timer (settings.IdleWaitSecs). Bucket-creation settings
// stay frozen for the bucket's lifetime — admin edits apply to subsequent
// buckets only.
//
// Safe to call after Stop(): becomes a no-op so late event handlers don't
// panic during shutdown.
func (d *Dispatcher) Enqueue(staffID, issueID string, settings Settings, n QueuedNotification) {
	if d == nil || !settings.Enabled || staffID == "" || issueID == "" {
		return
	}

	key := bucketKey(staffID, issueID)

	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	b, ok := d.buckets[key]
	if !ok {
		now := d.now()
		b = &bucket{
			key:       key,
			staffID:   staffID,
			issueID:   issueID,
			settings:  settings,
			createdAt: now,
		}
		d.buckets[key] = b
		b.maxTimer = time.AfterFunc(time.Duration(settings.MaxWaitSecs)*time.Second, func() {
			d.flush(key, "max_wait")
		})
	}
	b.notifications = append(b.notifications, n)
	if b.idleTimer != nil {
		b.idleTimer.Stop()
	}
	b.idleTimer = time.AfterFunc(time.Duration(b.settings.IdleWaitSecs)*time.Second, func() {
		d.flush(key, "idle_wait")
	})
	d.mu.Unlock()
}

// flush removes the bucket and renders + ships its prompt. On any error
// (render or inject) the bucket is dropped with a warning log — the inbox
// row is durable in DB; a stuck workspace can flip enabled=false.
func (d *Dispatcher) flush(key, reason string) {
	d.mu.Lock()
	b, ok := d.buckets[key]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.buckets, key)
	if b.idleTimer != nil {
		b.idleTimer.Stop()
	}
	if b.maxTimer != nil {
		b.maxTimer.Stop()
	}
	d.mu.Unlock()

	if len(b.notifications) == 0 {
		return
	}
	d.deliver(b, reason)
}

// deliver renders the prompt and calls the injector with a fresh 30 s
// context (the originating http handler ctx is long gone by the time a
// timer fires).
func (d *Dispatcher) deliver(b *bucket, reason string) {
	data := BuildTemplateData(b.notifications, b.settings, b.staffID, b.sessionKey, d.now())
	prompt, err := Render(b.settings.Template, data)
	if err != nil {
		slog.Warn("notify-summary: render failed, dropping bucket",
			"staff_id", b.staffID, "issue_id", b.issueID, "count", len(b.notifications), "reason", reason, "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.injector.SendSessionPrompt(ctx, b.staffID, prompt); err != nil {
		slog.Warn("notify-summary: inject failed, dropping bucket",
			"staff_id", b.staffID, "issue_id", b.issueID, "count", len(b.notifications), "reason", reason, "error", err)
		return
	}
	slog.Info("notify-summary: prompt injected",
		"staff_id", b.staffID, "issue_id", b.issueID, "count", len(b.notifications), "reason", reason)
}

// Stop drains every pending bucket synchronously then signals completion.
// Idempotent — safe to call once from graceful shutdown.
func (d *Dispatcher) Stop() {
	d.stopOnce.Do(func() {
		d.mu.Lock()
		d.stopped = true
		buckets := make([]*bucket, 0, len(d.buckets))
		for _, b := range d.buckets {
			if b.idleTimer != nil {
				b.idleTimer.Stop()
			}
			if b.maxTimer != nil {
				b.maxTimer.Stop()
			}
			buckets = append(buckets, b)
		}
		d.buckets = map[string]*bucket{}
		d.mu.Unlock()

		for _, b := range buckets {
			if len(b.notifications) == 0 {
				continue
			}
			d.deliver(b, "shutdown")
		}
		close(d.stopCh)
		close(d.doneCh)
		slog.Info("notify-summary: dispatcher stopped", "buckets_flushed", len(buckets))
	})
}

// Done returns a channel that closes after Stop completes. Mirrors
// BatchedHeartbeatScheduler so the main shutdown sequence can wait on
// dispatcher drain before closing the DB pool.
func (d *Dispatcher) Done() <-chan struct{} { return d.doneCh }

func bucketKey(staffID, issueID string) string {
	return staffID + "\x00" + issueID
}
