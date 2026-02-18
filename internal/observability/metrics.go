package observability

import "github.com/prometheus/client_golang/prometheus"

var (
	// wvs-api metrics
	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "wvs_http_requests_total",
		Help: "Total HTTP requests",
	}, []string{"route", "method", "code"})

	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "wvs_http_request_duration_seconds",
		Help:    "HTTP request latency",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method"})

	ActiveRequests = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "wvs_active_requests",
		Help: "Current in-flight requests",
	})

	// wvs-worker metrics
	TaskTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "wvs_task_total",
		Help: "Task completion count",
	}, []string{"op", "status"})

	TaskDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "wvs_task_duration_seconds",
		Help:    "Task end-to-end duration",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
	}, []string{"op"})

	TaskQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "wvs_task_queue_depth",
		Help: "Pending + retryable FAILED tasks",
	})

	TaskRetryTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "wvs_task_retry_total",
		Help: "Task retry count",
	}, []string{"op"})

	LockWaitSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "wvs_lock_wait_seconds",
		Help:    "Advisory lock wait time",
		Buckets: []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	DequeueEmptyTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "wvs_dequeue_empty_total",
		Help: "Empty poll count",
	})

	WorkspaceStateTransitions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "wvs_workspace_state_transitions_total",
		Help: "Workspace state transition count",
	}, []string{"from", "to"})

	// executor metrics
	CloneDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "wvs_clone_duration_seconds",
		Help:    "JuiceFS clone duration",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
	}, []string{"op"})

	CloneEntriesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "wvs_clone_entries_total",
		Help: "Clone directory entry count",
	}, []string{"op"})

	CloneFailTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "wvs_clone_fail_total",
		Help: "Clone failure count",
	}, []string{"reason"})

	QuiesceWaitSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "wvs_quiesce_wait_seconds",
		Help:    "Wait for agent ack duration",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
	})

	QuiesceTimeoutTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "wvs_quiesce_timeout_total",
		Help: "Quiesce timeout count",
	})

	SwitchDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "wvs_switch_duration_seconds",
		Help:    "Symlink switch duration",
		Buckets: []float64{0.0001, 0.001, 0.01, 0.1},
	})

	ExecutorActiveTasks = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "wvs_executor_active_tasks",
		Help: "Currently executing tasks",
	})
)

func RegisterAll(reg prometheus.Registerer) {
	reg.MustRegister(
		HTTPRequestsTotal, HTTPRequestDuration, ActiveRequests,
		TaskTotal, TaskDuration, TaskQueueDepth, TaskRetryTotal,
		LockWaitSeconds, DequeueEmptyTotal, WorkspaceStateTransitions,
		CloneDuration, CloneEntriesTotal, CloneFailTotal,
		QuiesceWaitSeconds, QuiesceTimeoutTotal, SwitchDuration, ExecutorActiveTasks,
	)
}
