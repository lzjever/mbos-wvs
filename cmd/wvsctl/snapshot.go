package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type SnapshotRow struct {
	SnapshotID string `json:"snapshot_id"`
	WSID       string `json:"wsid"`
	FSPath     string `json:"fs_path"`
	Message    string `json:"message"`
	CreatedAt  string `json:"created_at"`
}

type SnapshotListResponse struct {
	Snapshots  []SnapshotRow `json:"snapshots"`
	NextCursor string        `json:"next_cursor"`
}

var snapshotCmd = &cobra.Command{
	Use:     "snapshot",
	Aliases: []string{"snap"},
	Short:   "Snapshot management commands",
}

var snapCreateCmd = &cobra.Command{
	Use:   "create <wsid> [message]",
	Short: "Create a snapshot",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		wsid := args[0]
		message := ""
		if len(args) > 1 {
			message = args[1]
		}

		idempotencyKey := uuid.New().String()
		client := NewClient(apiURL)

		var resp TaskRef
		req := map[string]string{"message": message}

		err := postWithHeaders(client, "/v1/workspaces/"+wsid+"/snapshots", req, &resp, map[string]string{
			"Idempotency-Key": idempotencyKey,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Snapshot creation task created.\n")
		fmt.Printf("Task ID: %s\n", resp.TaskID)
		fmt.Printf("Check status: wvsctl task get %s\n", resp.TaskID)
	},
}

var snapListCmd = &cobra.Command{
	Use:   "list <wsid>",
	Short: "List snapshots for a workspace",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		wsid := args[0]
		client := NewClient(apiURL)

		var resp SnapshotListResponse
		if err := client.Get("/v1/workspaces/"+wsid+"/snapshots", &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		printResult(resp.Snapshots)
	},
}

var snapDropCmd = &cobra.Command{
	Use:   "drop <wsid> <snapshot-id>",
	Short: "Drop a snapshot",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		wsid := args[0]
		snapshotID := args[1]

		idempotencyKey := uuid.New().String()
		client := NewClient(apiURL)

		var resp TaskRef

		// Need to do DELETE with Idempotency-Key header
		req, _ := http.NewRequest("DELETE", client.baseURL+"/v1/workspaces/"+wsid+"/snapshots/"+snapshotID, nil)
		req.Header.Set("Idempotency-Key", idempotencyKey)
		httpResp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer httpResp.Body.Close()
		parseResponse(httpResp, &resp)

		fmt.Printf("Snapshot drop task created.\n")
		fmt.Printf("Task ID: %s\n", resp.TaskID)
	},
}

func init() {
	snapshotCmd.AddCommand(snapCreateCmd, snapListCmd, snapDropCmd)
	rootCmd.AddCommand(snapshotCmd)
}
