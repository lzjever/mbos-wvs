package main

import (
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type CurrentResponse struct {
	WSID              string `json:"wsid"`
	CurrentSnapshotID string `json:"current_snapshot_id"`
	CurrentPath       string `json:"current_path"`
}

var currentCmd = &cobra.Command{
	Use:   "current",
	Short: "Current snapshot management commands",
}

var currentGetCmd = &cobra.Command{
	Use:   "get <wsid>",
	Short: "Get current snapshot for a workspace",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		wsid := args[0]
		client := NewClient(apiURL)

		var resp CurrentResponse
		if err := client.Get("/v1/workspaces/"+wsid+"/current", &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Current Snapshot: %s\n", resp.CurrentSnapshotID)
		fmt.Printf("Current Path: %s\n", resp.CurrentPath)
	},
}

var currentSetCmd = &cobra.Command{
	Use:   "set <wsid> <snapshot-id>",
	Short: "Set current snapshot for a workspace",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		wsid := args[0]
		snapshotID := args[1]

		idempotencyKey := uuid.New().String()
		client := NewClient(apiURL)

		var resp TaskRef
		req := map[string]string{"snapshot_id": snapshotID}

		err := postWithHeaders(client, "/v1/workspaces/"+wsid+"/current:set", req, &resp, map[string]string{
			"Idempotency-Key": idempotencyKey,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Set current task created.\n")
		fmt.Printf("Task ID: %s\n", resp.TaskID)
		fmt.Printf("Check status: wvsctl task get %s\n", resp.TaskID)
	},
}

func init() {
	currentCmd.AddCommand(currentGetCmd, currentSetCmd)
	rootCmd.AddCommand(currentCmd)
}
