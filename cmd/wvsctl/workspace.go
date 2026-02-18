package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type WorkspaceRow struct {
	WSID              string `json:"wsid"`
	State             string `json:"state"`
	Owner             string `json:"owner"`
	CurrentSnapshotID string `json:"current_snapshot_id"`
	CreatedAt         string `json:"created_at"`
}

type WorkspaceListResponse struct {
	Workspaces []WorkspaceRow `json:"workspaces"`
	NextCursor string         `json:"next_cursor"`
}

type TaskRef struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	StatusURL string `json:"status_href"`
}

var workspaceCmd = &cobra.Command{
	Use:     "workspace",
	Aliases: []string{"ws"},
	Short:   "Workspace management commands",
}

var wsCreateCmd = &cobra.Command{
	Use:   "create <wsid> <root-path> <owner>",
	Short: "Create a new workspace",
	Args:  cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		wsid := args[0]
		rootPath := args[1]
		owner := args[2]

		idempotencyKey := uuid.New().String()
		client := NewClient(apiURL)

		var resp TaskRef
		req := map[string]string{
			"wsid":      wsid,
			"root_path": rootPath,
			"owner":     owner,
		}

		err := postWithHeaders(client, "/v1/workspaces", req, &resp, map[string]string{
			"Idempotency-Key": idempotencyKey,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Workspace creation task created.\n")
		fmt.Printf("Task ID: %s\n", resp.TaskID)
		fmt.Printf("Check status: wvsctl task get %s\n", resp.TaskID)
	},
}

var wsGetCmd = &cobra.Command{
	Use:   "get <wsid>",
	Short: "Get workspace details",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		wsid := args[0]
		client := NewClient(apiURL)

		var ws WorkspaceRow
		if err := client.Get("/v1/workspaces/"+wsid, &ws); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		printResult(ws)
	},
}

var wsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workspaces",
	Run: func(cmd *cobra.Command, args []string) {
		client := NewClient(apiURL)

		var resp WorkspaceListResponse
		if err := client.Get("/v1/workspaces", &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		printResult(resp.Workspaces)
	},
}

var wsDisableCmd = &cobra.Command{
	Use:   "disable <wsid>",
	Short: "Disable a workspace",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		wsid := args[0]
		client := NewClient(apiURL)

		var ws WorkspaceRow
		if err := client.Delete("/v1/workspaces/"+wsid, &ws); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Workspace %s disabled.\n", ws.WSID)
	},
}

var wsRetryInitCmd = &cobra.Command{
	Use:   "retry-init <wsid>",
	Short: "Retry failed workspace initialization",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		wsid := args[0]
		client := NewClient(apiURL)

		var resp TaskRef
		if err := client.Post("/v1/workspaces/"+wsid+"/retry-init", nil, &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Retry task created.\n")
		fmt.Printf("Task ID: %s\n", resp.TaskID)
	},
}

func init() {
	workspaceCmd.AddCommand(wsCreateCmd, wsGetCmd, wsListCmd, wsDisableCmd, wsRetryInitCmd)
	rootCmd.AddCommand(workspaceCmd)
}

func postWithHeaders(client *Client, path string, body interface{}, out interface{}, headers map[string]string) error {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req, _ := http.NewRequest("POST", client.baseURL+path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return parseResponse(resp, out)
}
