package notifysummary

// DefaultTemplate is the markdown-card prompt the renderer falls back to when
// a workspace has not configured its own template.
//
// Output shape:
//   - H2 heading: identifier + linked title
//   - One-line Chinese summary
//   - Status table (Issue status / task status / elapsed) with emoji
//   - "最新进展" numbered list (required)
//   - Optional "当前阻塞点" / "Spec 总结" sections
//
// Template variables use Go-struct PascalCase ({{.IssueTitle}}).
const DefaultTemplate = `You have received {{.NotificationCount}} Multica issue notification(s) for issue {{.IssueID}}.

Cached issue context (do NOT re-fetch unless the context below is insufficient):
- Identifier: {{.IssueIdentifier}}
- URL: {{.IssueURL}}
- Title: "{{.IssueTitle}}"
- DB status: {{.IssueStatus}}
- Status tag: {{.IssueStatusTag}}
- Status tag (no prefix): {{.IssueStatusTagShort}}
- Created at: {{.IssueCreateTime}}
- Elapsed: {{.IssueElapsed}}
- Linked pull/merge requests:
{{.IssuePullRequests}}

Allowed tool calls (each at most once, only if the cached context above is insufficient):
- ` + "`multica issue get {{.IssueID}}`" + ` — full issue metadata (assignee, labels, parents).
- ` + "`multica issue comment list {{.IssueID}}`" + ` — comment thread.

Output EXACTLY the markdown structure below. Replace every [placeholder] with real content derived from the notification context (and optionally the CLI reads). Do NOT add anything before or after the structure.

---BEGIN STRUCTURE---

## {{.IssueIdentifier}} [{{.IssueTitle}}]({{.IssueURL}})

[一句话摘要]

| Issue 状态 | 任务状态 | 已运行时间 |
|------------|---------|-----------|
| [ISSUE_STATUS_CELL] | [TASK_STATUS_CELL] | ⏱{{.IssueElapsed}} |

## 最新进展

[PROGRESS_LIST]

[OPTIONAL_SECTIONS]

---END STRUCTURE---

Placeholder rules (must follow ALL):

1. [一句话摘要]: one Chinese sentence (~30 chars) describing the current state and what comes next. No bullet, no heading, no emoji.

2. [ISSUE_STATUS_CELL]: pick ONE emoji + Chinese label that matches {{.IssueStatus}}:
   - todo → 📋待办
   - in_progress → 🏃‍♂️进行中
   - in_review → 🔍评审中
   - done → ✅已完成
   - cancelled → ❌已取消
   If the status is not in this list, use 🔵 + the raw status.

3. [TASK_STATUS_CELL]: pick a fitting emoji + {{.IssueStatusTagShort}} in Chinese. Examples:
   - 等待Spec批准 → 🚦等待Spec批准
   - 代码编写中 → ✍️代码编写中
   - 等待PR批准 → ✍️等待PR批准
   - Code-Review → 🔍Code-Review
   If no status tag, use "—".

4. [PROGRESS_LIST]: a numbered list (1. 2. 3. …) of recent events extracted from the notification context. Most recent first. Each item is one concise Chinese sentence. Required — always include at least one item.

5. [OPTIONAL_SECTIONS]: include zero or more of these sections ONLY when relevant data exists:
   - ` + "`## 当前阻塞点`" + ` — list blocking PR/MR links with status (待审批 / 待合并). Include when PRs are linked AND not yet merged.
   - ` + "`## Spec 总结`" + ` — brief spec summary with a link. Include only if the comment thread (from CLI read) contains a spec document.
   If no optional section is relevant, omit entirely (no empty headings).

Hard constraints:
- All text in Chinese except identifiers, URLs, and code references.
- The H2 heading line format is exactly: ` + "`## ID [Title](URL)`" + ` — do not change it.
- The table must have exactly 3 columns with the pipe format shown. Do not add or remove columns.
- DO NOT call shell, grep, rg, find, ls, cat, sed, awk, git, curl, or ANY command other than the two ` + "`multica`" + ` reads listed above.
- No preamble, no greeting, no meta phrases ("以下是总结", "Summary:", "好的", "I will now…").

Notification context:
{{.CombinedContent}}`
