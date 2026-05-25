package aone

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// withFakeAone spins up an httptest.Server that responds to the project /
// merge_request lookup path and points `Enrich` at it for the test.
// Returns the test server so the caller can inspect captured request
// headers / path if needed.
func withFakeAone(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	prev := aoneAPIBase
	aoneAPIBase = srv.URL
	t.Setenv("MULTICA_AONE_PRIVATE_TOKEN", "test-token")
	t.Cleanup(func() {
		aoneAPIBase = prev
		srv.Close()
	})
	return srv
}

func TestEnrich_HappyPath(t *testing.T) {
	var gotToken, gotPath string
	withFakeAone(t, func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		gotPath = r.URL.EscapedPath()
		fmt.Fprintln(w, `{"title":"fix bug","state":"opened"}`)
	})

	e, err := Enrich(context.Background(), "peida.lpd", "sls_reg", 27300570)
	if err != nil {
		t.Fatalf("enrich: %v", err)
	}
	if e.Title != "fix bug" {
		t.Errorf("title = %q, want %q", e.Title, "fix bug")
	}
	if e.State != "open" {
		t.Errorf("state = %q, want open", e.State)
	}
	if gotToken != "test-token" {
		t.Errorf("PRIVATE-TOKEN header = %q", gotToken)
	}
	if gotPath != "/api/v4/projects/peida.lpd%2Fsls_reg/merge_request/27300570" {
		t.Errorf("request path = %q", gotPath)
	}
}

func TestEnrich_NotConfigured(t *testing.T) {
	t.Setenv("MULTICA_AONE_PRIVATE_TOKEN", "")
	_, err := Enrich(context.Background(), "owner", "repo", 1)
	if !errors.Is(err, ErrAoneNotConfigured) {
		t.Errorf("err = %v, want ErrAoneNotConfigured", err)
	}
}

func TestEnrich_RequestFailed(t *testing.T) {
	withFakeAone(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	})
	_, err := Enrich(context.Background(), "owner", "repo", 1)
	if !errors.Is(err, ErrAoneRequestFailed) {
		t.Errorf("err = %v, want ErrAoneRequestFailed", err)
	}
}

func TestEnrich_ParseFailed(t *testing.T) {
	withFakeAone(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "not-json")
	})
	_, err := Enrich(context.Background(), "owner", "repo", 1)
	if !errors.Is(err, ErrAoneParseFailed) {
		t.Errorf("err = %v, want ErrAoneParseFailed", err)
	}
}

func TestEnrich_EmptyTitleIsParseFailure(t *testing.T) {
	withFakeAone(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"title":"","state":"opened"}`)
	})
	_, err := Enrich(context.Background(), "owner", "repo", 1)
	if !errors.Is(err, ErrAoneParseFailed) {
		t.Errorf("err = %v, want ErrAoneParseFailed", err)
	}
}

func TestMapAoneState(t *testing.T) {
	cases := map[string]string{
		"merged":       "merged",
		"closed":       "closed",
		"rejected":     "closed",
		"draft":        "draft",
		"accepted":     "open",
		"under_review": "open",
		"opened":       "open",
		"reviewing":    "open",
		"OPEN":         "open",
		"surprise":     "unknown",
		"":             "unknown",
	}
	for in, want := range cases {
		if got := mapAoneState(in); got != want {
			t.Errorf("mapAoneState(%q) = %q, want %q", in, got, want)
		}
	}
}
