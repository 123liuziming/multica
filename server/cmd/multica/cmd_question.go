package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var questionCmd = &cobra.Command{
	Use:   "question",
	Short: "Inspect and answer pending AskUserQuestion items from agents",
	Long: `Agents with allow_ask_user_question enabled can call AskUserQuestion during
a task. Each call is persisted as one or more questions for users (or other
agents) to answer. This subcommand lists, shows, and answers those questions.`,
}

var questionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List questions in the workspace",
	RunE:  runQuestionList,
}

var questionShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a single question with its options and current state",
	Args:  cobra.ExactArgs(1),
	RunE:  runQuestionShow,
}

var questionAnswerCmd = &cobra.Command{
	Use:   "answer <id>",
	Short: "Answer a pending question (flag-driven, no interactive prompt)",
	Long: `Answer a question by passing --option (0-indexed, repeatable) and/or
--custom "free-form text". At least one of the two is required.

Examples:
  multica question answer Q123 --option 0
  multica question answer Q123 --option 1 --option 2 --custom "also consider X"
  multica question answer Q123 --custom "none of the above; do Y"`,
	Args: cobra.ExactArgs(1),
	RunE: runQuestionAnswer,
}

func init() {
	questionListCmd.Flags().String("status", "pending", "Filter by status: pending|answered|cancelled|all")
	questionListCmd.Flags().Int("limit", 100, "Maximum number of questions to return")
	questionListCmd.Flags().String("output", "table", "Output format: table or json")

	questionShowCmd.Flags().String("output", "table", "Output format: table or json")

	questionAnswerCmd.Flags().IntSlice("option", nil, "0-indexed option to select (repeat for multi-select)")
	questionAnswerCmd.Flags().String("custom", "", "Free-form custom text answer (combined with --option)")
	questionAnswerCmd.Flags().String("output", "json", "Output format: table or json")

	questionCmd.AddCommand(questionListCmd)
	questionCmd.AddCommand(questionShowCmd)
	questionCmd.AddCommand(questionAnswerCmd)

	questionCmd.GroupID = groupAdditional
	rootCmd.AddCommand(questionCmd)
}

// questionWire mirrors the server's QuestionResponse shape. Keeping a local
// struct (not importing the handler package) keeps the CLI binary slim.
type questionWire struct {
	ID                  string            `json:"id"`
	WorkspaceID         string            `json:"workspace_id"`
	TaskID              string            `json:"task_id"`
	AgentID             string            `json:"agent_id"`
	IssueID             *string           `json:"issue_id,omitempty"`
	Header              string            `json:"header"`
	Question            string            `json:"question"`
	Options             []questionOptWire `json:"options"`
	MultiSelect         bool              `json:"multi_select"`
	Status              string            `json:"status"`
	AnswerOptionIndices []int             `json:"answer_option_indices,omitempty"`
	AnswerCustomText    string            `json:"answer_custom_text,omitempty"`
	AnsweredByUserID    *string           `json:"answered_by_user_id,omitempty"`
	AnsweredAt          *string           `json:"answered_at,omitempty"`
	CreatedAt           string            `json:"created_at"`
}

type questionOptWire struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

func runQuestionList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	params := url.Values{}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		params.Set("status", v)
	}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	path := "/api/questions"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var rows []questionWire
	if err := client.GetJSON(ctx, path, &rows); err != nil {
		return fmt.Errorf("list questions: %w", err)
	}

	out, _ := cmd.Flags().GetString("output")
	if out == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	if len(rows) == 0 {
		fmt.Println("No questions.")
		return nil
	}
	headers := []string{"ID", "STATUS", "HEADER", "AGENT", "ISSUE", "CREATED"}
	tbl := make([][]string, 0, len(rows))
	for _, q := range rows {
		issueID := ""
		if q.IssueID != nil {
			issueID = shortID(*q.IssueID)
		}
		tbl = append(tbl, []string{
			shortID(q.ID),
			q.Status,
			truncate(q.Header, 40),
			shortID(q.AgentID),
			issueID,
			shortTime(q.CreatedAt),
		})
	}
	cli.PrintTable(os.Stdout, headers, tbl)
	return nil
}

func runQuestionShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var q questionWire
	if err := client.GetJSON(ctx, "/api/questions/"+args[0], &q); err != nil {
		return fmt.Errorf("get question: %w", err)
	}

	out, _ := cmd.Flags().GetString("output")
	if out == "json" {
		return cli.PrintJSON(os.Stdout, q)
	}
	fmt.Printf("ID:        %s\n", q.ID)
	fmt.Printf("Status:    %s\n", q.Status)
	fmt.Printf("Header:    %s\n", q.Header)
	fmt.Printf("Question:  %s\n", q.Question)
	fmt.Printf("Multi:     %v\n", q.MultiSelect)
	fmt.Printf("Agent:     %s\n", q.AgentID)
	fmt.Printf("Task:      %s\n", q.TaskID)
	if q.IssueID != nil {
		fmt.Printf("Issue:     %s\n", *q.IssueID)
	}
	fmt.Printf("Created:   %s\n", q.CreatedAt)
	if len(q.Options) > 0 {
		fmt.Println("Options:")
		for i, opt := range q.Options {
			fmt.Printf("  [%d] %s", i, opt.Label)
			if opt.Description != "" {
				fmt.Printf(" — %s", opt.Description)
			}
			fmt.Println()
		}
	}
	if q.Status == "answered" {
		fmt.Println("Answer:")
		if len(q.AnswerOptionIndices) > 0 {
			labels := make([]string, 0, len(q.AnswerOptionIndices))
			for _, idx := range q.AnswerOptionIndices {
				if idx >= 0 && idx < len(q.Options) {
					labels = append(labels, fmt.Sprintf("[%d] %s", idx, q.Options[idx].Label))
				}
			}
			fmt.Printf("  Selected: %s\n", strings.Join(labels, ", "))
		}
		if q.AnswerCustomText != "" {
			fmt.Printf("  Custom: %s\n", q.AnswerCustomText)
		}
		if q.AnsweredAt != nil {
			fmt.Printf("  At: %s\n", *q.AnsweredAt)
		}
		if q.AnsweredByUserID != nil {
			fmt.Printf("  By user: %s\n", *q.AnsweredByUserID)
		}
	}
	return nil
}

func runQuestionAnswer(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	opts, _ := cmd.Flags().GetIntSlice("option")
	custom, _ := cmd.Flags().GetString("custom")
	if len(opts) == 0 && custom == "" {
		return fmt.Errorf("at least one of --option or --custom is required")
	}

	body := map[string]any{
		"option_indices": opts,
		"custom_text":    custom,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var resp questionWire
	if err := client.PostJSON(ctx, "/api/questions/"+args[0]+"/answer", body, &resp); err != nil {
		return fmt.Errorf("answer question: %w", err)
	}

	out, _ := cmd.Flags().GetString("output")
	if out == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Printf("Question %s answered.\n", resp.ID)
	return nil
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

func shortTime(s string) string {
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		if len(s) >= 16 {
			return s[:16]
		}
		return s
	}
	return t.Local().Format("2006-01-02 15:04")
}

// jsonString is a tiny helper so we don't have to import encoding/json from
// the rest of the file. Currently unused; reserved for future commands.
var _ = json.RawMessage(nil)
