package aone

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Refresher is the subset of *db.Queries the refresh loop actually needs.
// Pulled into an interface so tests can swap in an in-memory fake without
// standing up a real Postgres.
type Refresher interface {
	ListAonePullRequestsForRefresh(ctx context.Context, arg db.ListAonePullRequestsForRefreshParams) ([]db.AonePullRequest, error)
	UpdateAonePullRequestEnrichment(ctx context.Context, arg db.UpdateAonePullRequestEnrichmentParams) error
	TouchAonePullRequestEnrichment(ctx context.Context, id pgtype.UUID) error
}

// EnrichFunc is the runtime hook into `a1`. Tests pass a fake that returns
// canned data; production passes aone.Enrich.
type EnrichFunc func(ctx context.Context, owner, repo string, number int32) (Enrichment, error)

// PublishFunc fires a realtime event after a row's title/state actually
// changes. nil is allowed — the loop simply skips the broadcast.
type PublishFunc func(eventType, workspaceID, actorType, actorID string, payload any)

// RefreshOptions configures the periodic loop. Sensible defaults are
// applied for any zero-valued field so callers can pass {} to get the
// production behavior.
type RefreshOptions struct {
	Interval     time.Duration // default 30m — how often to wake up
	StaleAfter   time.Duration // default 30m — minimum age of last_enriched_at
	BatchSize    int32         // default 25 — max rows touched per tick
	Enrich       EnrichFunc    // default aone.Enrich
	Publish      PublishFunc   // optional realtime broadcaster
}

// RunRefreshLoop is the long-running goroutine spawned by main.go. It
// exits cleanly when ctx is cancelled. Each tick is best-effort — a
// failed enrichment skips that row but never breaks the loop.
func RunRefreshLoop(ctx context.Context, q Refresher, opts RefreshOptions) {
	opts = applyDefaults(opts)
	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()

	// Run once immediately so a freshly linked PR doesn't wait 30m for its
	// first enrichment attempt.
	refreshOnce(ctx, q, opts)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshOnce(ctx, q, opts)
		}
	}
}

func refreshOnce(ctx context.Context, q Refresher, opts RefreshOptions) {
	rows, err := q.ListAonePullRequestsForRefresh(ctx, db.ListAonePullRequestsForRefreshParams{
		StaleAfterSeconds: int32(opts.StaleAfter.Seconds()),
		Limit:             opts.BatchSize,
	})
	if err != nil {
		slog.Warn("aone refresh: list failed", "err", err)
		return
	}
	for _, row := range rows {
		if !row.RepoOwner.Valid || !row.RepoName.Valid || !row.PrNumber.Valid {
			// Missing identifiers — likely a manual external URL that
			// landed in aone_pull_request via a hostname rule that didn't
			// extract owner/repo. We can't ask a1 about it; bump the
			// timestamp so we don't keep retrying.
			if err := q.TouchAonePullRequestEnrichment(ctx, row.ID); err != nil {
				slog.Warn("aone refresh: touch failed", "err", err)
			}
			continue
		}
		e, err := opts.Enrich(ctx, row.RepoOwner.String, row.RepoName.String, row.PrNumber.Int32)
		if err != nil {
			if errors.Is(err, ErrAoneNotConfigured) {
				// No PRIVATE-TOKEN configured — the rest of the batch will
				// hit the same wall, so short-circuit instead of logging
				// the same warning for every row.
				slog.Debug("aone refresh: not configured, skipping batch")
				return
			}
			slog.Warn("aone refresh: enrich failed", "err", err, "url", row.HtmlUrl)
			continue
		}
		if e.Title == row.Title && e.State == row.State {
			// Nothing changed — bump last_enriched_at so the row drops out
			// of the stale window but skip broadcasting (no subscriber
			// needs to refetch a list whose contents are identical).
			if err := q.TouchAonePullRequestEnrichment(ctx, row.ID); err != nil {
				slog.Warn("aone refresh: touch failed", "err", err)
			}
			continue
		}
		if err := q.UpdateAonePullRequestEnrichment(ctx, db.UpdateAonePullRequestEnrichmentParams{
			ID:    row.ID,
			Title: e.Title,
			State: e.State,
		}); err != nil {
			slog.Warn("aone refresh: update failed", "err", err)
			continue
		}
		if opts.Publish != nil {
			opts.Publish("pull_request:updated", util.UUIDToString(row.WorkspaceID), "system", "", map[string]any{
				"pull_request_id": util.UUIDToString(row.ID),
				"title":           e.Title,
				"state":           e.State,
			})
		}
	}
}

func applyDefaults(o RefreshOptions) RefreshOptions {
	if o.Interval <= 0 {
		o.Interval = 30 * time.Minute
	}
	if o.StaleAfter <= 0 {
		o.StaleAfter = 30 * time.Minute
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 25
	}
	if o.Enrich == nil {
		o.Enrich = Enrich
	}
	return o
}
