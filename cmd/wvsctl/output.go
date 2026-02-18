package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
)

func printResult(v interface{}) {
	if output == "json" {
		json.NewEncoder(os.Stdout).Encode(v)
		return
	}
	printTable(v)
}

func printTable(v interface{}) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	switch data := v.(type) {
	case []WorkspaceRow:
		if len(data) == 0 {
			fmt.Println("No workspaces found.")
			return
		}
		fmt.Fprintln(w, "WSID\tSTATE\tOWNER\tCURRENT SNAPSHOT\tCREATED")
		for _, ws := range data {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", ws.WSID, ws.State, ws.Owner, ws.CurrentSnapshotID, ws.CreatedAt)
		}
	case []SnapshotRow:
		if len(data) == 0 {
			fmt.Println("No snapshots found.")
			return
		}
		fmt.Fprintln(w, "SNAPSHOT ID\tMESSAGE\tCREATED")
		for _, s := range data {
			fmt.Fprintf(w, "%s\t%s\t%s\n", s.SnapshotID, truncate(s.Message, 40), s.CreatedAt)
		}
	case []TaskRow:
		if len(data) == 0 {
			fmt.Println("No tasks found.")
			return
		}
		fmt.Fprintln(w, "TASK ID\tOP\tSTATUS\tATTEMPT\tCREATED")
		for _, t := range data {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\t%s\n", t.TaskID[:8], t.Op, t.Status, t.Attempt, t.MaxAttempts, t.CreatedAt)
		}
	case TaskRow:
		fmt.Fprintf(w, "Task ID:\t%s\n", data.TaskID)
		fmt.Fprintf(w, "WSID:\t%s\n", data.WSID)
		fmt.Fprintf(w, "Op:\t%s\n", data.Op)
		fmt.Fprintf(w, "Status:\t%s\n", data.Status)
		fmt.Fprintf(w, "Attempt:\t%d/%d\n", data.Attempt, data.MaxAttempts)
		if data.Result != nil {
			fmt.Fprintf(w, "Result:\t%v\n", data.Result)
		}
		if data.Error != nil {
			fmt.Fprintf(w, "Error:\t%v\n", data.Error)
		}
	default:
		json.NewEncoder(os.Stdout).Encode(v)
	}
	w.Flush()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
