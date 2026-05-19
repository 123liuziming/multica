package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Work with workspaces",
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workspaces you belong to",
	RunE:  runWorkspaceList,
}

var workspaceGetCmd = &cobra.Command{
	Use:   "get [workspace-id]",
	Short: "Get workspace details",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceGet,
}

var workspaceMembersCmd = &cobra.Command{
	Use:   "members [workspace-id]",
	Short: "List workspace members",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceMembers,
}

var workspaceUpdateCmd = &cobra.Command{
	Use:   "update [workspace-id]",
	Short: "Update workspace metadata (admin/owner only)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceUpdate,
}

var workspaceCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new workspace",
	RunE:  runWorkspaceCreate,
}

var workspaceAoneSyncCmd = &cobra.Command{
	Use:   "aone-sync [workspace-id]",
	Short: "Trigger a one-time Aone→Multica issue sync for a workspace",
	RunE:  runWorkspaceAoneSync,
}

func init() {
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceGetCmd)
	workspaceCmd.AddCommand(workspaceMembersCmd)
	workspaceCmd.AddCommand(workspaceUpdateCmd)
	workspaceCmd.AddCommand(workspaceCreateCmd)
	workspaceCmd.AddCommand(workspaceAoneSyncCmd)

	workspaceGetCmd.Flags().String("output", "json", "Output format: table or json")
	workspaceMembersCmd.Flags().String("output", "table", "Output format: table or json")

	workspaceCreateCmd.Flags().String("name", "", "Workspace name (required)")
	workspaceCreateCmd.Flags().String("slug", "", "Workspace slug (required)")
	workspaceCreateCmd.Flags().String("description", "", "Workspace description")
	workspaceCreateCmd.Flags().String("aone-project-id", "", "Aone project ID to enable periodic sync")
	workspaceCreateCmd.Flags().String("output", "json", "Output format: table or json")

	workspaceUpdateCmd.Flags().String("name", "", "New workspace name")
	workspaceUpdateCmd.Flags().String("description", "", "New description (decodes \\n, \\r, \\t, \\\\; pipe via --description-stdin to preserve literal backslashes)")
	workspaceUpdateCmd.Flags().Bool("description-stdin", false, "Read description from stdin (preserves multi-line content verbatim)")
	workspaceUpdateCmd.Flags().String("context", "", "New workspace context (decodes \\n, \\r, \\t, \\\\; pipe via --context-stdin to preserve literal backslashes)")
	workspaceUpdateCmd.Flags().Bool("context-stdin", false, "Read context from stdin (preserves multi-line content verbatim)")
	workspaceUpdateCmd.Flags().String("issue-prefix", "", "New issue prefix (uppercased server-side)")
	workspaceUpdateCmd.Flags().String("aone-project-id", "", "Aone project ID (set to enable sync, empty to disable)")
	workspaceUpdateCmd.Flags().String("output", "json", "Output format: table or json")
}

func runWorkspaceCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	slug, _ := cmd.Flags().GetString("slug")
	if name == "" || slug == "" {
		return fmt.Errorf("--name and --slug are required")
	}

	body := map[string]any{
		"name": name,
		"slug": slug,
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if cmd.Flags().Changed("aone-project-id") {
		v, _ := cmd.Flags().GetString("aone-project-id")
		body["aone_project_id"] = v
	}

	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}

	client := cli.NewAPIClient(serverURL, "", token)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ws map[string]any
	if err := client.PostJSON(ctx, "/api/workspaces", body, &ws); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "SLUG"}
		rows := [][]string{{
			strVal(ws, "id"),
			strVal(ws, "name"),
			strVal(ws, "slug"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, ws)
}

func runWorkspaceList(cmd *cobra.Command, _ []string) error {
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}

	client := cli.NewAPIClient(serverURL, "", token)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var workspaces []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	if len(workspaces) == 0 {
		fmt.Fprintln(os.Stderr, "No workspaces found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME")
	for _, ws := range workspaces {
		fmt.Fprintf(w, "%s\t%s\n", ws.ID, ws.Name)
	}
	return w.Flush()
}

func workspaceIDFromArgs(cmd *cobra.Command, args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return resolveWorkspaceID(cmd)
}

func runWorkspaceGet(cmd *cobra.Command, args []string) error {
	wsID := workspaceIDFromArgs(cmd, args)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ws map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		desc := strVal(ws, "description")
		if utf8.RuneCountInString(desc) > 60 {
			runes := []rune(desc)
			desc = string(runes[:57]) + "..."
		}
		wsContext := strVal(ws, "context")
		if utf8.RuneCountInString(wsContext) > 60 {
			runes := []rune(wsContext)
			wsContext = string(runes[:57]) + "..."
		}
		headers := []string{"ID", "NAME", "SLUG", "DESCRIPTION", "CONTEXT"}
		rows := [][]string{{
			strVal(ws, "id"),
			strVal(ws, "name"),
			strVal(ws, "slug"),
			desc,
			wsContext,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, ws)
}

// buildWorkspaceUpdateBody assembles the PATCH payload from the flags the
// caller actually set, mirroring server/internal/handler/workspace.go's
// UpdateWorkspaceRequest. Only fields whose flag is Changed() are emitted, so
// the caller cannot accidentally clobber a field they did not pass.
func buildWorkspaceUpdateBody(cmd *cobra.Command) (map[string]any, error) {
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		body["name"] = v
	}
	if cmd.Flags().Changed("description") || cmd.Flags().Changed("description-stdin") {
		desc, _, err := resolveTextFlag(cmd, "description")
		if err != nil {
			return nil, err
		}
		body["description"] = desc
	}
	if cmd.Flags().Changed("context") || cmd.Flags().Changed("context-stdin") {
		ctxText, _, err := resolveTextFlag(cmd, "context")
		if err != nil {
			return nil, err
		}
		body["context"] = ctxText
	}
	if cmd.Flags().Changed("issue-prefix") {
		v, _ := cmd.Flags().GetString("issue-prefix")
		// The handler silently skips an empty prefix (workspace.go:274), so
		// `--issue-prefix ""` would otherwise return 200 without changing
		// anything. Reject it here so the failure is visible.
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("--issue-prefix cannot be empty; clearing the prefix is not supported")
		}
		body["issue_prefix"] = v
	}
	if cmd.Flags().Changed("aone-project-id") {
		v, _ := cmd.Flags().GetString("aone-project-id")
		body["aone_project_id"] = v
	}
	return body, nil
}

func runWorkspaceUpdate(cmd *cobra.Command, args []string) error {
	wsID := workspaceIDFromArgs(cmd, args)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass as argument or set MULTICA_WORKSPACE_ID")
	}

	body, err := buildWorkspaceUpdateBody(cmd)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --name, --description, --context, or --issue-prefix")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ws map[string]any
	if err := client.PatchJSON(ctx, "/api/workspaces/"+wsID, body, &ws); err != nil {
		return fmt.Errorf("update workspace: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		desc := strVal(ws, "description")
		if utf8.RuneCountInString(desc) > 60 {
			runes := []rune(desc)
			desc = string(runes[:57]) + "..."
		}
		wsContext := strVal(ws, "context")
		if utf8.RuneCountInString(wsContext) > 60 {
			runes := []rune(wsContext)
			wsContext = string(runes[:57]) + "..."
		}
		headers := []string{"ID", "NAME", "SLUG", "DESCRIPTION", "CONTEXT"}
		rows := [][]string{{
			strVal(ws, "id"),
			strVal(ws, "name"),
			strVal(ws, "slug"),
			desc,
			wsContext,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, ws)
}

func runWorkspaceAoneSync(cmd *cobra.Command, args []string) error {
	wsID := workspaceIDFromArgs(cmd, args)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/workspaces/"+wsID+"/aone-sync", nil, &result); err != nil {
		return fmt.Errorf("aone sync: %w", err)
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runWorkspaceMembers(cmd *cobra.Command, args []string) error {
	wsID := workspaceIDFromArgs(cmd, args)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID+"/members", &members); err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, members)
	}

	headers := []string{"USER ID", "NAME", "EMAIL", "ROLE"}
	rows := make([][]string, 0, len(members))
	for _, m := range members {
		rows = append(rows, []string{
			strVal(m, "user_id"),
			strVal(m, "name"),
			strVal(m, "email"),
			strVal(m, "role"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
