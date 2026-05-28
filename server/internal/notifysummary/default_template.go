package notifysummary

// DefaultTemplate is the markdown-card prompt the renderer falls back to when
// a workspace has not configured its own template.
//
// Extra template functions available: contains, hasPrefix, hasSuffix,
// toLower, toUpper.
//
// Template variables use Go-struct PascalCase ({{.IssueTitle}}).
const DefaultTemplate = `You have received {{.NotificationCount}} Multica issue notification(s) for issue {{.IssueID}}.
You must report the current issue progress to the user.

Issue context:
- Identifier: {{.IssueIdentifier}}
- Aone URL: {{.AoneIssueURL}}
- Multica URL: {{.IssueURL}}
- Title: "{{.IssueTitle}}"
- IssueStatus: {{.IssueStatus}}
- AgentStatus: {{.IssueStatusTagShort}}
- Created at: {{.IssueCreateTime}}
- Elapsed: {{.IssueElapsed}}
- Linked pull/merge requests: {{.IssuePullRequests}}

Allowed tool calls to get detailed information:
- ` + "`multica issue get {{.IssueID}}`" + ` — full issue metadata.
- ` + "`multica issue comment list {{.IssueID}}`" + ` — issue comment thread.

Notification context:
` + "```text" + `
{{.CombinedContent}}
` + "```" + `

Instructions:
1. Read the notification context and use the allowed tool calls above to get the full issue details.
2. Write a progress summary in the format below.
3. All text must be in Chinese (except identifiers, URLs, and status values).
4. "最新进展": at most 3 bullet points. Focus on concrete actions and outcomes. Do NOT include status transitions.
5. "当前阻塞点": only include if there is an actual blocker (e.g. a PR pending review, a requirement needing clarification). Omit this section entirely if there are no blockers.
6. Output ONLY the summary below — no preamble, no greeting, no meta commentary.

Output format:

## {{.IssueIdentifier}} [{{.IssueTitle}}]({{.AoneIssueURL}})

一句话摘要：<one sentence summarizing current state and next step>

| Issue 状态 | 任务状态 | 已运行时间 |
|------------|---------|-----------|
| {{.IssueStatus}} | {{.IssueStatusTagShort}} | ⏱{{.IssueElapsed}} |

## 最新进展

- <concrete action or outcome 1>
- <concrete action or outcome 2>
- <concrete action or outcome 3>

## 当前阻塞点

- <actual blocker, if any>
`
