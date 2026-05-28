package notifysummary

import (
	"strings"
	"testing"
	"time"
)

func TestStripStatusPrefix(t *testing.T) {
	tests := map[string]string{
		"":                   "",
		"  ":                 "",
		"status:code-review": "code-review",
		"Status:Code-Review": "Code-Review",
		"STATUS:Blocked":     "Blocked",
		"area:backend":       "area:backend", // not a status prefix
		"status:":            "",             // empty after prefix
		"statusoid:foo":      "statusoid:foo",
	}
	for in, want := range tests {
		if got := StripStatusPrefix(in); got != want {
			t.Errorf("StripStatusPrefix(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestHumanizeIssueElapsed(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"not-a-date", ""},
		{"2026-05-28T11:59:30Z", "刚刚"},
		{"2026-05-28T11:45:00Z", "15 分钟"},
		{"2026-05-28T08:30:00Z", "3 小时 30 分钟"},
		{"2026-05-28T11:00:00Z", "1 小时"},
		{"2026-05-26T11:00:00Z", "2 天 1 小时"},
		{"2026-05-26T12:00:00Z", "2 天"},
		{"2026-05-28T12:00:30Z", "刚刚"}, // future clamps
	}
	for _, tt := range tests {
		if got := HumanizeIssueElapsed(tt.in, now); got != tt.want {
			t.Errorf("HumanizeIssueElapsed(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestRender_DefaultTemplateProducesCardStructure(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	batch := []QueuedNotification{
		{
			Platform: "dingtalk", UserID: "1001",
			Title: "PR linked", Content: "linked PR https://example.com/pr/1",
			Metadata: map[string]string{
				"workspace_id":        "wid",
				"issue_id":            "iid-1",
				"issue_identifier":    "SHUIZHAO-GH-7",
				"issue_url":           "https://multica.example.com/shuizhao-gh/issues/SHUIZHAO-GH-7",
				"issue_title":         "示例 issue",
				"issue_status":        "in_progress",
				"issue_status_tag":    "status:code-review",
				"issue_create_time":   "2026-05-26T09:00:00Z",
				"issue_pull_requests": "https://example.com/pr/1",
				"inbox_type":          "pr_linked",
			},
			ReceivedAt: now,
		},
	}
	data := BuildTemplateData(batch, Normalize(Settings{Enabled: true, SummaryLength: 150}), "1001", "dingtalk:d:c1:1001", now)
	out, err := Render("", data)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	wantSubstrings := []string{
		"## SHUIZHAO-GH-7 [示例 issue]()",
		"| Issue 状态 | 任务状态 | 已运行时间 |",
		"| in_progress | code-review | ⏱2 天 3 小时 |",
		"## 最新进展",
		"Notification context:",
		"linked PR https://example.com/pr/1",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q.\nFull output:\n%s", want, out)
		}
	}
}

func TestValidateTemplate_RejectsBadSyntax(t *testing.T) {
	if err := ValidateTemplate(""); err != nil {
		t.Errorf("empty template should be allowed (renderer falls back to default): %v", err)
	}
	if err := ValidateTemplate("{{ .IssueTitle }}"); err != nil {
		t.Errorf("valid template should pass: %v", err)
	}
	if err := ValidateTemplate("{{ end }}"); err == nil {
		t.Error("malformed template should be rejected")
	}
}
