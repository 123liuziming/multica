package notifysummary

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"
)

// QueuedNotification is one inbox event captured by the dispatcher. The
// renderer reads only string fields — UUIDs are pre-formatted by the
// dispatcher before enqueue.
type QueuedNotification struct {
	Platform   string
	UserID     string // bare staff ID at this layer
	Title      string
	Content    string
	Metadata   map[string]string
	ReceivedAt time.Time
}

// TemplateData is the typed view exposed to operator templates.
// PascalCase field names map directly to {{.FieldName}} in Go text/template.
type TemplateData struct {
	StaffID             string
	UserID              string
	Platform            string
	SessionKey          string
	SummaryLength       int
	NotificationCount   int
	CombinedContent     string
	WorkspaceID         string
	IssueID             string
	IssueIdentifier     string
	IssueURL            string
	AoneIssueURL        string
	IssueTitle          string
	IssueStatus         string
	IssueStatusTag      string
	IssueStatusTagShort string
	IssueCreateTime     string
	IssueElapsed        string
	IssuePullRequests   string
	InboxType           string
}

// BuildTemplateData assembles the renderer input from a bucket of queued
// notifications + the settings the bucket was created with + a wall-clock
// "now" so elapsed is computed deterministically (tests pin "now").
func BuildTemplateData(batch []QueuedNotification, settings Settings, staffID, sessionKey string, now time.Time) TemplateData {
	issueTitle := latestMetadataValue(batch, "issue_title")
	if issueTitle == "" && len(batch) > 0 {
		issueTitle = batch[len(batch)-1].Title
	}
	statusTag := latestMetadataValue(batch, "issue_status_tag")
	createTime := latestMetadataValue(batch, "issue_create_time")
	platform := ""
	userID := staffID
	if len(batch) > 0 {
		platform = batch[0].Platform
		if batch[0].UserID != "" {
			userID = batch[0].UserID
		}
	}
	return TemplateData{
		StaffID:             staffID,
		UserID:              userID,
		Platform:            platform,
		SessionKey:          sessionKey,
		SummaryLength:       settings.SummaryLength,
		NotificationCount:   len(batch),
		CombinedContent:     combinedNotifyContent(batch),
		WorkspaceID:         latestMetadataValue(batch, "workspace_id"),
		IssueID:             latestMetadataValue(batch, "issue_id"),
		IssueIdentifier:     latestMetadataValue(batch, "issue_identifier"),
		IssueURL:            latestMetadataValue(batch, "issue_url"),
		AoneIssueURL:        latestMetadataValue(batch, "aone_issue_url"),
		IssueTitle:          issueTitle,
		IssueStatus:         latestMetadataValue(batch, "issue_status"),
		IssueStatusTag:      statusTag,
		IssueStatusTagShort: StripStatusPrefix(statusTag),
		IssueCreateTime:     createTime,
		IssueElapsed:        HumanizeIssueElapsed(createTime, now),
		IssuePullRequests:   latestMetadataValue(batch, "issue_pull_requests"),
		InboxType:           latestMetadataValue(batch, "inbox_type"),
	}
}

// templateFuncs extends Go text/template with string helpers so operators
// can write status-conditional sections like:
//
//	{{if contains .IssueStatusTagShort "PR"}}## 当前阻塞点{{end}}
var templateFuncs = template.FuncMap{
	"contains":  strings.Contains,
	"hasPrefix": strings.HasPrefix,
	"hasSuffix": strings.HasSuffix,
	"toLower":   strings.ToLower,
	"toUpper":   strings.ToUpper,
}

// Render parses tmpl (falling back to DefaultTemplate when empty) and
// executes it against data. Output is trimmed of leading/trailing
// whitespace.
func Render(tmpl string, data TemplateData) (string, error) {
	if strings.TrimSpace(tmpl) == "" {
		tmpl = DefaultTemplate
	}
	t, err := template.New("notify_summary").Funcs(templateFuncs).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("notifysummary: parse template: %w", err)
	}
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return "", fmt.Errorf("notifysummary: execute template: %w", err)
	}
	rendered := strings.TrimSpace(out.String())
	if rendered == "" {
		return "", fmt.Errorf("notifysummary: template rendered empty prompt")
	}
	return rendered, nil
}

// ValidateTemplate parses (but does not execute) a template string. Used by
// the REST PUT handler to reject malformed templates with a 400 instead of
// a runtime error in the dispatcher.
func ValidateTemplate(tmpl string) error {
	if strings.TrimSpace(tmpl) == "" {
		return nil
	}
	if _, err := template.New("notify_summary").Funcs(templateFuncs).Parse(tmpl); err != nil {
		return fmt.Errorf("notifysummary: template parse error: %w (template variables use PascalCase e.g. {{.IssueTitle}}; available funcs: contains, hasPrefix, hasSuffix, toLower, toUpper)", err)
	}
	return nil
}

// StripStatusPrefix returns the label value with a leading "status:" (case
// insensitive) trimmed off, e.g. "Status:Code-Review" -> "Code-Review".
// Returns the input unchanged when no prefix matches.
func StripStatusPrefix(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	if idx := strings.Index(tag, ":"); idx > 0 {
		if strings.EqualFold(tag[:idx], "status") {
			return strings.TrimSpace(tag[idx+1:])
		}
	}
	return tag
}

// HumanizeIssueElapsed renders the time since createTime as a short Chinese
// duration. Returns "" when createTime is empty or unparseable so the
// template can omit the line. Future timestamps clamp to "刚刚".
func HumanizeIssueElapsed(createTime string, now time.Time) string {
	createTime = strings.TrimSpace(createTime)
	if createTime == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, createTime)
	if err != nil {
		return ""
	}
	d := now.Sub(t)
	if d < 0 {
		return "刚刚"
	}
	days := int(d / (24 * time.Hour))
	hours := int(d/time.Hour) % 24
	minutes := int(d/time.Minute) % 60
	switch {
	case days > 0:
		if hours > 0 {
			return fmt.Sprintf("%d 天 %d 小时", days, hours)
		}
		return fmt.Sprintf("%d 天", days)
	case hours > 0:
		if minutes > 0 {
			return fmt.Sprintf("%d 小时 %d 分钟", hours, minutes)
		}
		return fmt.Sprintf("%d 小时", hours)
	case minutes > 0:
		return fmt.Sprintf("%d 分钟", minutes)
	default:
		return "刚刚"
	}
}

// combinedNotifyContent renders the batched notifications as a single
// human-readable block, separated by `---`. Each block lists title,
// content, and sorted metadata.
func combinedNotifyContent(batch []QueuedNotification) string {
	var b strings.Builder
	for i, n := range batch {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		if n.Title != "" {
			b.WriteString("Title: ")
			b.WriteString(n.Title)
			b.WriteString("\n")
		}
		if n.Content != "" {
			b.WriteString("Content:\n")
			b.WriteString(n.Content)
			b.WriteString("\n")
		}
		if len(n.Metadata) > 0 {
			b.WriteString("Metadata:\n")
			keys := make([]string, 0, len(n.Metadata))
			for k := range n.Metadata {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				b.WriteString("- ")
				b.WriteString(k)
				b.WriteString(": ")
				b.WriteString(n.Metadata[k])
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// latestMetadataValue scans the batch in reverse and returns the first
// non-empty value for the given metadata key. "Latest" reflects the most
// recently received notification — useful for status-like fields.
func latestMetadataValue(batch []QueuedNotification, key string) string {
	for i := len(batch) - 1; i >= 0; i-- {
		if v := strings.TrimSpace(batch[i].Metadata[key]); v != "" {
			return v
		}
	}
	return ""
}
