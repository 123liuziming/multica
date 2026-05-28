package notifysummary

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSender struct {
	mu    sync.Mutex
	calls []struct {
		UserID string
		Prompt string
	}
	err error
}

func (f *fakeSender) SendSessionPrompt(_ context.Context, userID, prompt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		UserID string
		Prompt string
	}{userID, prompt})
	return f.err
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeSender) lastPrompt() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return ""
	}
	return f.calls[len(f.calls)-1].Prompt
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func quickSettings() Settings {
	return Normalize(Settings{Enabled: true, IdleWaitSecs: 1, MaxWaitSecs: 2, SummaryLength: 50, Template: "STAFF={{.StaffID}} ISSUE={{.IssueID}} COUNT={{.NotificationCount}} CONTENT={{.CombinedContent}}"})
}

func mkNotification(title, content string, md map[string]string) QueuedNotification {
	if md == nil {
		md = map[string]string{}
	}
	return QueuedNotification{
		Platform: "dingtalk", UserID: "1001",
		Title: title, Content: content, Metadata: md, ReceivedAt: time.Now(),
	}
}

func TestEnqueue_IdleFlushFiresOnce(t *testing.T) {
	sender := &fakeSender{}
	d := NewDispatcher(sender)
	defer d.Stop()

	d.Enqueue("1001", "issue-1", quickSettings(), mkNotification("first", "body", map[string]string{"issue_id": "issue-1"}))
	waitFor(t, 3*time.Second, func() bool { return sender.count() == 1 })
	if !strings.Contains(sender.lastPrompt(), "STAFF=1001") || !strings.Contains(sender.lastPrompt(), "ISSUE=issue-1") {
		t.Errorf("prompt missing expected fields: %s", sender.lastPrompt())
	}
}

func TestEnqueue_MaxWaitFiresWhenIdleKeepsResetting(t *testing.T) {
	sender := &fakeSender{}
	d := NewDispatcher(sender)
	defer d.Stop()
	s := quickSettings() // idle 1s, max 2s

	start := time.Now()
	d.Enqueue("1001", "issue-1", s, mkNotification("a", "a", map[string]string{"issue_id": "issue-1"}))
	// Keep resetting the idle timer just under IdleWaitSecs.
	for i := 0; i < 3; i++ {
		time.Sleep(800 * time.Millisecond)
		d.Enqueue("1001", "issue-1", s, mkNotification("nudge", "n", map[string]string{"issue_id": "issue-1"}))
	}
	waitFor(t, 4*time.Second, func() bool { return sender.count() == 1 })
	elapsed := time.Since(start)
	if elapsed < 2*time.Second {
		t.Errorf("flush fired too early: %v (expected ~max_wait)", elapsed)
	}
}

func TestEnqueue_BucketsPerStaffAndPerIssue(t *testing.T) {
	sender := &fakeSender{}
	d := NewDispatcher(sender)
	defer d.Stop()
	s := quickSettings()

	// Two distinct (staff, issue) tuples → two buckets → two flushes.
	d.Enqueue("1001", "issue-1", s, mkNotification("a", "a", map[string]string{"issue_id": "issue-1"}))
	d.Enqueue("1001", "issue-2", s, mkNotification("b", "b", map[string]string{"issue_id": "issue-2"}))
	d.Enqueue("2002", "issue-1", s, mkNotification("c", "c", map[string]string{"issue_id": "issue-1"}))

	waitFor(t, 3*time.Second, func() bool { return sender.count() == 3 })
}

func TestStop_DrainsPendingBuckets(t *testing.T) {
	sender := &fakeSender{}
	d := NewDispatcher(sender)
	s := quickSettings()

	d.Enqueue("1001", "issue-1", s, mkNotification("a", "a", map[string]string{"issue_id": "issue-1"}))
	// Don't wait for the timer; force a drain via Stop.
	d.Stop()

	if sender.count() != 1 {
		t.Errorf("Stop should have flushed pending bucket; got %d sends", sender.count())
	}
	// Late enqueues after Stop should be no-ops.
	d.Enqueue("1001", "issue-2", s, mkNotification("late", "late", nil))
	time.Sleep(50 * time.Millisecond)
	if sender.count() != 1 {
		t.Errorf("post-Stop enqueue should be ignored; got %d sends", sender.count())
	}
}

func TestEnqueue_NoopWhenDisabled(t *testing.T) {
	sender := &fakeSender{}
	d := NewDispatcher(sender)
	defer d.Stop()
	disabled := quickSettings()
	disabled.Enabled = false
	d.Enqueue("1001", "issue-1", disabled, mkNotification("a", "a", nil))
	time.Sleep(1500 * time.Millisecond)
	if sender.count() != 0 {
		t.Errorf("disabled dispatch should not fire; got %d sends", sender.count())
	}
}
