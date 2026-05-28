package notifysummary

// DefaultTemplate is the markdown-card prompt the renderer falls back to when
// a workspace has not configured its own template.
//
// Extra template functions available: contains, hasPrefix, hasSuffix,
// toLower, toUpper.
//
// Template variables use Go-struct PascalCase ({{.IssueTitle}}).
const DefaultTemplate = `You have received {{.NotificationCount}} Multica issue notification(s) for issue {{.IssueID}}.
You must report the current issue progress to the user. Output strictly follows the output template below — nothing else.

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

Hard constraints:
- All text in Chinese except identifiers, URLs, and the status tag line.
- "最新进展" section: at most 3 items. Focus on concrete actions and outcomes (user feedback, agent actions, PR/MR activity). Do NOT include issue status transitions (e.g. "状态从 todo 变为 in_progress") — those are already shown in the table.
- No preamble, no greeting, no meta commentary — only the templated output.

Notification context:
` + "```text" + `
{{.CombinedContent}}
` + "```" + `

Output template:
` + "```" + `
## {{.IssueIdentifier}} [{{.IssueTitle}}]({{.AoneIssueURL}})

一句话摘要：一句话概括当前状态和下一步，例如 "代码工作已全部完成，等 PR 批准合入即可关闭"。

| Issue 状态 | 任务状态 | 已运行时间 |
|------------|---------|-----------|
| {{.IssueStatus}} | {{.IssueStatusTagShort}} | ⏱{{.IssueElapsed}} |

## 最新进展

1. 用户反馈子仓 PR 遗漏 → Agent 已补创建子仓 MR
2. 用户提出额外需求：将 web-ui-test/ 根目录下平铺的测试文件重组为 testcase/ + results/ 两个子目录
3. Agent 已完成目录重组并重跑测试，结果正常

## 当前阻塞点 (可选，如果有的话。例如需要批准 Spec, 需要批准 PR, 需要澄清需求才能继续时）

- 待审批 PR（父仓）: https://code.alibaba-inc.com/agentloop/agentloop_dev/codereview/27648985
- 待审批 PR（子仓）: https://code.alibaba-inc.com/agentloop/agentloop-console-web/codereview/27653573
- 待澄清需求： xxxx

## Spec 总结 （可选，仅当 AgentStatus = "等待Spec批准" 时）

[Spec Summary, No more than 150 words]

` + "```" + `
`
