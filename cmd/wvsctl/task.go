package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

type TaskRow struct {
	TaskID          string                 `json:"task_id"`
	WSID            string                 `json:"wsid"`
	Op              string                 `json:"op"`
	Status          string                 `json:"status"`
	IdempotencyKey  string                 `json:"idempotency_key"`
	CreatedAt       string                 `json:"created_at"`
	Attempt         int32                  `json:"attempt"`
	MaxAttempts     int32                  `json:"max_attempts"`
	Params          map[string]interface{} `json:"params"`
	Result          map[string]interface{} `json:"result,omitempty"`
	Error           map[string]interface{} `json:"error,omitempty"`
}

type TaskListResponse struct {
	Tasks      []TaskRow `json:"tasks"`
	NextCursor string    `json:"next_cursor"`
}

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Task management commands",
}

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	Run: func(cmd *cobra.Command, args []string) {
		client := NewClient(apiURL)

		var resp TaskListResponse
		if err := client.Get("/v1/tasks", &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		printResult(resp.Tasks)
	},
}

var taskGetCmd = &cobra.Command{
	Use:   "get <task-id>",
	Short: "Get task details",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		taskID := args[0]
		client := NewClient(apiURL)

		var resp TaskRow
		if err := client.Get("/v1/tasks/"+taskID, &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		printResult(resp)
	},
}

var taskWatchCmd = &cobra.Command{
	Use:   "watch <task-id>",
	Short: "Watch task until completion",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		taskID := args[0]
		client := NewClient(apiURL)

		for {
			var resp TaskRow
			if err := client.Get("/v1/tasks/"+taskID, &resp); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Task %s: %s (attempt %d/%d)\n", taskID[:8], resp.Status, resp.Attempt, resp.MaxAttempts)

			if resp.Status == "SUCCEEDED" || resp.Status == "FAILED" || resp.Status == "CANCELED" || resp.Status == "DEAD" {
				if resp.Result != nil {
					fmt.Printf("Result: %v\n", resp.Result)
				}
				if resp.Error != nil {
					fmt.Printf("Error: %v\n", resp.Error)
				}
				break
			}

			time.Sleep(1 * time.Second)
		}
	},
}

var taskCancelCmd = &cobra.Command{
	Use:   "cancel <task-id>",
	Short: "Cancel a task",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		taskID := args[0]
		client := NewClient(apiURL)

		var resp TaskRow
		if err := client.Post("/v1/tasks/"+taskID+":cancel", nil, &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Task %s status: %s\n", taskID, resp.Status)
	},
}

func init() {
	taskCmd.AddCommand(taskListCmd, taskGetCmd, taskWatchCmd, taskCancelCmd)
	rootCmd.AddCommand(taskCmd)
}
