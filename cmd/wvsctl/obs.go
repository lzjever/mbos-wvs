package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

var obsCmd = &cobra.Command{
	Use:   "obs",
	Short: "Observability commands (query VictoriaMetrics)",
}

var vmsingleURL string

type VMResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

var obsSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Show system summary metrics",
	Run: func(cmd *cobra.Command, args []string) {
		url := vmsingleURL
		if url == "" {
			url = "http://localhost:8428"
		}

		queries := map[string]string{
			"Task Success Rate": `sum(rate(wvs_task_total{status="SUCCEEDED"}[5m])) / sum(rate(wvs_task_total[5m])) * 100`,
			"HTTP Request Rate": `sum(rate(wvs_http_requests_total[5m]))`,
			"Queue Depth":       `wvs_task_queue_depth`,
			"Active Requests":   `wvs_active_requests`,
		}

		for name, query := range queries {
			val := queryVM(url, query)
			fmt.Printf("%s: %s\n", name, val)
		}
	},
}

var obsLatencyCmd = &cobra.Command{
	Use:   "latency",
	Short: "Show latency metrics",
	Run: func(cmd *cobra.Command, args []string) {
		url := vmsingleURL
		if url == "" {
			url = "http://localhost:8428"
		}

		queries := map[string]string{
			"HTTP P50": `histogram_quantile(0.5, sum(rate(wvs_http_request_duration_seconds_bucket[5m])) by (le))`,
			"HTTP P95": `histogram_quantile(0.95, sum(rate(wvs_http_request_duration_seconds_bucket[5m])) by (le))`,
			"HTTP P99": `histogram_quantile(0.99, sum(rate(wvs_http_request_duration_seconds_bucket[5m])) by (le))`,
		}

		for name, query := range queries {
			val := queryVM(url, query)
			fmt.Printf("%s: %s\n", name, val)
		}
	},
}

var obsQueueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Show queue metrics",
	Run: func(cmd *cobra.Command, args []string) {
		url := vmsingleURL
		if url == "" {
			url = "http://localhost:8428"
		}

		queries := map[string]string{
			"Queue Depth":      `wvs_task_queue_depth`,
			"Empty Poll Rate":  `rate(wvs_dequeue_empty_total[5m])`,
			"Lock Wait P95":    `histogram_quantile(0.95, sum(rate(wvs_lock_wait_seconds_bucket[5m])) by (le))`,
		}

		for name, query := range queries {
			val := queryVM(url, query)
			fmt.Printf("%s: %s\n", name, val)
		}
	},
}

var obsQuiesceCmd = &cobra.Command{
	Use:   "quiesce",
	Short: "Show quiesce metrics",
	Run: func(cmd *cobra.Command, args []string) {
		url := vmsingleURL
		if url == "" {
			url = "http://localhost:8428"
		}

		queries := map[string]string{
			"Quiesce Wait P95":    `histogram_quantile(0.95, sum(rate(wvs_quiesce_wait_seconds_bucket[5m])) by (le))`,
			"Quiesce Timeout Rate": `rate(wvs_quiesce_timeout_total[5m])`,
		}

		for name, query := range queries {
			val := queryVM(url, query)
			fmt.Printf("%s: %s\n", name, val)
		}
	},
}

func queryVM(baseURL, query string) string {
	url := baseURL + "/api/v1/query?query=" + query
	resp, err := http.Get(url)
	if err != nil {
		return "error: " + err.Error()
	}
	defer resp.Body.Close()

	var vmResp VMResponse
	if err := json.NewDecoder(resp.Body).Decode(&vmResp); err != nil {
		return "parse error"
	}

	if len(vmResp.Data.Result) == 0 {
		return "no data"
	}

	result := vmResp.Data.Result[0]
	if len(result.Value) >= 2 {
		return fmt.Sprintf("%v", result.Value[1])
	}
	return "no value"
}

func init() {
	obsCmd.PersistentFlags().StringVar(&vmsingleURL, "vm-url", "http://localhost:8428", "VictoriaMetrics URL")
	obsCmd.AddCommand(obsSummaryCmd, obsLatencyCmd, obsQueueCmd, obsQuiesceCmd)
	rootCmd.AddCommand(obsCmd)
}
