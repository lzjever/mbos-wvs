package worker

import "time"

type Config struct {
	DBDSN           string        `envconfig:"WVS_DB_DSN" required:"true"`
	ExecutorAddrs   string        `envconfig:"EXECUTOR_ADDRS" required:"true"`
	MetricsAddr     string        `envconfig:"WVS_METRICS_ADDR" default:"0.0.0.0:9091"`
	LogLevel        string        `envconfig:"WVS_LOG_LEVEL" default:"info"`
	PollInterval    time.Duration `envconfig:"WORKER_POLL_INTERVAL" default:"1s"`
	IdleBackoff     time.Duration `envconfig:"WORKER_IDLE_BACKOFF" default:"5s"`
	ShutdownTimeout time.Duration `envconfig:"WORKER_SHUTDOWN_TIMEOUT" default:"120s"`
}
