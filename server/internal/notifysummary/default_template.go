package notifysummary

// DefaultTemplate is the markdown-card prompt the renderer falls back to when
// a workspace has not configured its own template.
//
// Uses Go text/template {{if}} to render status-conditional sections so the
// LLM only fills in the freeform placeholders. Extra template functions
// available: contains, hasPrefix, hasSuffix, toLower, toUpper.
//
// Template variables use Go-struct PascalCase ({{.IssueTitle}}).
const DefaultTemplate = `You have received {{.NotificationCount}} Multica issue notification(s) for issue {{.IssueID}}.

Cached issue context (do NOT re-fetch unless the notification context below is insufficient):
- Identifier: {{.IssueIdentifier}}
- Aone URL: {{.AoneIssueURL}}
- Multica URL: {{.IssueURL}}
- Title: "{{.IssueTitle}}"
- DB status: {{.IssueStatus}}
- Status tag: {{.IssueStatusTag}}
- Status tag (no prefix): {{.IssueStatusTagShort}}
- Created at: {{.IssueCreateTime}}
- Elapsed: {{.IssueElapsed}}
- Linked pull/merge requests:
{{.IssuePullRequests}}

Allowed tool calls (each at most once, only if the cached context above is insufficient):
- ` + "`multica issue get {{.IssueID}}`" + ` — full issue metadata.
- ` + "`multica issue comment list {{.IssueID}}`" + ` — comment thread.

Output EXACTLY the markdown below. Replace every [placeholder] with real content. Do NOT add anything before or after.

---BEGIN OUTPUT---
{{if .IssueStatusTag}}
{{.IssueStatusTag}}
{{end}}
## {{.IssueIdentifier}} [{{.IssueTitle}}]({{.AoneIssueURL}})

[一句话摘要：一句中文概括当前状态和下一步]

| Issue 状态 | 任务状态 | 已运行时间 |
|------------|---------|-----------|
| [ISSUE_STATUS_CELL] | [TASK_STATUS_CELL] | ⏱{{.IssueElapsed}} |

## 最新进展

[PROGRESS_LIST]
{{if or (contains .IssueStatusTagShort "PR") (contains .IssueStatusTagShort "Code") (contains .IssueStatusTagShort "Review") (contains .IssueStatusTagShort "审批") (contains .IssueStatusTagShort "合并")}}
## 当前阻塞点

[BLOCKING_LIST: list each pending PR/MR with its review link and status (待审批/待合并). Use the linked PRs from cached context.]
{{end}}{{if or (contains .IssueStatusTagShort "Spec") (contains .IssueStatusTagShort "spec") (contains .IssueStatusTagShort "设计")}}
## Spec 总结

[SPEC_SUMMARY: call ` + "`multica issue comment list {{.IssueID}}`" + ` to find the spec document. Output the spec PR link + a brief summary of the spec content. If no spec found, omit this entire section.]
{{end}}
---END OUTPUT---

Placeholder rules (must follow ALL):

1. [一句话摘要]: one Chinese sentence (~30 chars). No bullet, no heading, no emoji.

2. [ISSUE_STATUS_CELL]: pick ONE emoji + Chinese label matching {{.IssueStatus}}:
   todo → 📋待办 | in_progress → 🏃‍♂️进行中 | in_review → 🔍评审中 | done → ✅已完成 | cancelled → ❌已取消
   Unknown status → 🔵 + raw status.

3. [TASK_STATUS_CELL]: pick a fitting emoji + {{.IssueStatusTagShort}}. Examples:
   等待Spec批准 → 🚦等待Spec批准 | 代码编写中 → ✍️代码编写中 | 等待PR批准 → ✍️等待PR批准 | Code-Review → 🔍Code-Review
   No tag → "—".

4. [PROGRESS_LIST]: numbered list (1. 2. 3. …), most recent first, one concise Chinese sentence each. Always include at least one item.

5. [BLOCKING_LIST]: only rendered when the status tag contains PR/Code/Review/审批/合并. List each pending PR/MR link from cached context with status.

6. [SPEC_SUMMARY]: only rendered when the status tag contains Spec/spec/设计. Requires a CLI read. If no spec found in comments, output nothing (the section header is already rendered by the template).

Hard constraints:
- All text in Chinese except identifiers, URLs, and the status tag line.
- The table must have exactly 3 columns with the pipe format shown.
- DO NOT call shell, grep, rg, find, ls, cat, or ANY command other than the two ` + "`multica`" + ` reads.
- No preamble, no greeting, no meta phrases.

Notification context:
{{.CombinedContent}}`
