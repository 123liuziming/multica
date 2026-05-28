package notifysummary

import (
	"encoding/json"
	"testing"
)

func TestFromWorkspaceSettings_AbsentReturnsDefault(t *testing.T) {
	cases := [][]byte{nil, []byte(""), []byte(`{}`), []byte(`{"aone_project_id":"x"}`)}
	for _, raw := range cases {
		got, err := FromWorkspaceSettings(raw)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", raw, err)
		}
		if got != Default() {
			t.Errorf("FromWorkspaceSettings(%q) = %+v; want default", raw, got)
		}
	}
}

func TestFromWorkspaceSettings_ParsesEmbeddedBlock(t *testing.T) {
	raw := []byte(`{"aone_project_id":"x","notify_summary":{"enabled":true,"template":"T","idle_wait_secs":3,"max_wait_secs":12,"summary_length":150}}`)
	got, err := FromWorkspaceSettings(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Settings{Enabled: true, Template: "T", IdleWaitSecs: 3, MaxWaitSecs: 12, SummaryLength: 150}
	if got != want {
		t.Errorf("got %+v; want %+v", got, want)
	}
}

func TestNormalizeClampsMaxBelowIdle(t *testing.T) {
	got := Normalize(Settings{IdleWaitSecs: 30, MaxWaitSecs: 5})
	if got.MaxWaitSecs < got.IdleWaitSecs {
		t.Errorf("MaxWaitSecs=%d should clamp to >= IdleWaitSecs=%d", got.MaxWaitSecs, got.IdleWaitSecs)
	}
}

func TestNormalizeFillsZeroes(t *testing.T) {
	got := Normalize(Settings{Enabled: true})
	d := Default()
	if got.IdleWaitSecs != d.IdleWaitSecs || got.MaxWaitSecs != d.MaxWaitSecs || got.SummaryLength != d.SummaryLength {
		t.Errorf("Normalize did not fill defaults; got %+v", got)
	}
}

func TestMergeIntoWorkspaceSettings_PreservesOtherKeys(t *testing.T) {
	existing := []byte(`{"aone_project_id":"keep-me","other_field":42}`)
	merged, err := MergeIntoWorkspaceSettings(existing, Settings{Enabled: true, IdleWaitSecs: 5, MaxWaitSecs: 15, SummaryLength: 200, Template: "T"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(merged, &top); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	if top["aone_project_id"] != "keep-me" {
		t.Errorf("aone_project_id should survive merge: %v", top["aone_project_id"])
	}
	if _, ok := top["notify_summary"]; !ok {
		t.Error("notify_summary should be present after merge")
	}

	// Round-trip back through FromWorkspaceSettings.
	round, err := FromWorkspaceSettings(merged)
	if err != nil {
		t.Fatalf("FromWorkspaceSettings round-trip: %v", err)
	}
	want := Settings{Enabled: true, IdleWaitSecs: 5, MaxWaitSecs: 15, SummaryLength: 200, Template: "T"}
	if round != want {
		t.Errorf("round trip got %+v; want %+v", round, want)
	}
}

func TestMergeIntoWorkspaceSettings_EmptyInput(t *testing.T) {
	merged, err := MergeIntoWorkspaceSettings(nil, Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(merged) == 0 {
		t.Fatal("expected non-empty JSON")
	}
}
