package notifysummary

// DefaultTemplate is the markdown-card prompt the renderer falls back to
// when a workspace has not configured its own template. Smoke-validated on
// the live deployment: ~10 s turn, tools=0 (LLM follows the structure
// without invoking external tools), Chinese 简介 paragraph + fixed envelope.
//
// Template variable names use Go-struct PascalCase ({{.IssueTitle}}, not
// {{.issueTitle}}). Operators editing the template via the REST endpoint
// must use the same convention; the PUT handler surfaces parse errors back.
const DefaultTemplate = "You have received {{.NotificationCount}} Multica issue notification(s) for issue {{.IssueID}}.\n\n" +
	"Cached issue context (already known, do NOT re-fetch unless context is insufficient):\n" +
	"- Identifier: {{.IssueIdentifier}}\n" +
	"- URL: {{.IssueURL}}\n" +
	"- Title: \"{{.IssueTitle}}\"\n" +
	"- DB status: {{.IssueStatus}}\n" +
	"- Status tag (raw): {{.IssueStatusTag}}\n" +
	"- Status tag (no prefix): {{.IssueStatusTagShort}}\n" +
	"- Created at: {{.IssueCreateTime}}\n" +
	"- Elapsed since creation: {{.IssueElapsed}}\n" +
	"- Linked pull/merge requests:\n" +
	"{{.IssuePullRequests}}\n\n" +
	"Allowed tool calls (each at most once, only if the cached context is insufficient):\n" +
	"- `multica issue get {{.IssueID}}` — fetch full issue metadata (assignee, labels, milestone, parents).\n" +
	"- `multica issue comment list {{.IssueID}}` — fetch the comment thread.\n\n" +
	"Output EXACTLY this markdown structure, replacing [简介] with one paragraph of about {{.SummaryLength}} Chinese characters describing the current progress and runtime status (derived from the notification context below; reference recent state transitions, comments, and linked PR/MR activity where relevant). Do NOT add anything before or after.\n\n" +
	"# Title\n\n" +
	"**[{{.IssueIdentifier}} - {{.IssueTitle}}]({{.IssueURL}})**\n\n" +
	"- 状态：{{.IssueStatus}}\n" +
	"- 任务状态：{{.IssueStatusTagShort}}\n\n" +
	"[简介]\n\n" +
	"> 已运行时间：{{.IssueElapsed}}\n\n" +
	"Hard constraints (must follow ALL):\n" +
	"- The heading line is literally `# Title` (English). Everything else is Chinese.\n" +
	"- Keep the linked title line, the two bullet lines (`状态` / `任务状态`), and the trailing `已运行时间` quote line exactly as templated. Only the [简介] paragraph is freeform.\n" +
	"- DO NOT call shell, grep, rg, find, ls, cat, sed, awk, git, curl, or ANY command other than the two `multica` reads above. DO NOT search the filesystem or read any file.\n" +
	"- No preamble, no greeting, no meta phrases (e.g. \"以下是总结\", \"Summary:\", \"好的\", \"I will now…\").\n\n" +
	"Notification context:\n" +
	"{{.CombinedContent}}"
