package aone

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeRefresher captures every call so each test can assert which rows
// were enriched, touched, or skipped.
type fakeRefresher struct {
	mu       sync.Mutex
	rows     []db.AonePullRequest
	updates  []db.UpdateAonePullRequestEnrichmentParams
	touches  []pgtype.UUID
	listErr  error
}

func (f *fakeRefresher) ListAonePullRequestsForRefresh(_ context.Context, _ db.ListAonePullRequestsForRefreshParams) ([]db.AonePullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]db.AonePullRequest, len(f.rows))
	copy(out, f.rows)
	return out, nil
}

func (f *fakeRefresher) UpdateAonePullRequestEnrichment(_ context.Context, arg db.UpdateAonePullRequestEnrichmentParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, arg)
	return nil
}

func (f *fakeRefresher) TouchAonePullRequestEnrichment(_ context.Context, id pgtype.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touches = append(f.touches, id)
	return nil
}

func uuid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

func TestRefreshOnce_UpdatesChangedRows(t *testing.T) {
	f := &fakeRefresher{rows: []db.AonePullRequest{
		{
			ID:        uuid(1),
			HtmlUrl:   "https://code.alibaba-inc.com/owner/repo/codereview/1",
			Title:     "old",
			State:     "unknown",
			RepoOwner: pgtype.Text{String: "owner", Valid: true},
			RepoName:  pgtype.Text{String: "repo", Valid: true},
			PrNumber:  pgtype.Int4{Int32: 1, Valid: true},
		},
	}}
	opts := RefreshOptions{
		Enrich: func(context.Context, string, string, int32) (Enrichment, error) {
			return Enrichment{Title: "new", State: "open"}, nil
		},
	}
	refreshOnce(context.Background(), f, applyDefaults(opts))

	if len(f.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(f.updates))
	}
	if f.updates[0].Title != "new" || f.updates[0].State != "open" {
		t.Errorf("update = %+v, want title=new state=open", f.updates[0])
	}
	if len(f.touches) != 0 {
		t.Errorf("touches = %d, want 0 (changed row shouldn't be touched)", len(f.touches))
	}
}

func TestRefreshOnce_NoChangeOnlyTouches(t *testing.T) {
	f := &fakeRefresher{rows: []db.AonePullRequest{
		{
			ID:        uuid(2),
			HtmlUrl:   "https://code.alibaba-inc.com/owner/repo/codereview/2",
			Title:     "same",
			State:     "open",
			RepoOwner: pgtype.Text{String: "owner", Valid: true},
			RepoName:  pgtype.Text{String: "repo", Valid: true},
			PrNumber:  pgtype.Int4{Int32: 2, Valid: true},
		},
	}}
	opts := RefreshOptions{
		Enrich: func(context.Context, string, string, int32) (Enrichment, error) {
			return Enrichment{Title: "same", State: "open"}, nil
		},
	}
	refreshOnce(context.Background(), f, applyDefaults(opts))

	if len(f.updates) != 0 {
		t.Errorf("updates = %d, want 0 (no-change row)", len(f.updates))
	}
	if len(f.touches) != 1 {
		t.Errorf("touches = %d, want 1", len(f.touches))
	}
}

func TestRefreshOnce_SkipsOnEnrichError(t *testing.T) {
	f := &fakeRefresher{rows: []db.AonePullRequest{
		{
			ID:        uuid(3),
			HtmlUrl:   "x",
			Title:     "t",
			State:     "unknown",
			RepoOwner: pgtype.Text{String: "owner", Valid: true},
			RepoName:  pgtype.Text{String: "repo", Valid: true},
			PrNumber:  pgtype.Int4{Int32: 3, Valid: true},
		},
	}}
	opts := RefreshOptions{
		Enrich: func(context.Context, string, string, int32) (Enrichment, error) {
			return Enrichment{}, errors.New("boom")
		},
	}
	refreshOnce(context.Background(), f, applyDefaults(opts))
	if len(f.updates) != 0 || len(f.touches) != 0 {
		t.Errorf("expected no writes on enrich error, got updates=%d touches=%d", len(f.updates), len(f.touches))
	}
}

func TestRefreshOnce_NotConfiguredShortCircuits(t *testing.T) {
	f := &fakeRefresher{rows: []db.AonePullRequest{
		{
			ID: uuid(4), HtmlUrl: "a", Title: "x", State: "unknown",
			RepoOwner: pgtype.Text{String: "o", Valid: true},
			RepoName:  pgtype.Text{String: "r", Valid: true},
			PrNumber:  pgtype.Int4{Int32: 4, Valid: true},
		},
		{
			ID: uuid(5), HtmlUrl: "b", Title: "y", State: "unknown",
			RepoOwner: pgtype.Text{String: "o", Valid: true},
			RepoName:  pgtype.Text{String: "r", Valid: true},
			PrNumber:  pgtype.Int4{Int32: 5, Valid: true},
		},
	}}
	opts := RefreshOptions{
		Enrich: func(context.Context, string, string, int32) (Enrichment, error) {
			return Enrichment{}, ErrAoneNotConfigured
		},
	}
	refreshOnce(context.Background(), f, applyDefaults(opts))
	if len(f.updates) != 0 || len(f.touches) != 0 {
		t.Errorf("not-configured should short-circuit, got updates=%d touches=%d", len(f.updates), len(f.touches))
	}
}

func TestRefreshOnce_TouchesRowsMissingIdentifiers(t *testing.T) {
	f := &fakeRefresher{rows: []db.AonePullRequest{
		{ID: uuid(6), HtmlUrl: "weird", Title: "x", State: "unknown"},
	}}
	enrichCalled := false
	opts := RefreshOptions{
		Enrich: func(context.Context, string, string, int32) (Enrichment, error) {
			enrichCalled = true
			return Enrichment{}, nil
		},
	}
	refreshOnce(context.Background(), f, applyDefaults(opts))
	if enrichCalled {
		t.Error("enrich called on row with missing repo identifiers")
	}
	if len(f.touches) != 1 {
		t.Errorf("touches = %d, want 1", len(f.touches))
	}
}
