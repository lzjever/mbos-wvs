package api

import "time"

type Config struct {
	HTTPAddr        string        `envconfig:"WVS_HTTP_ADDR" default:"0.0.0.0:8080"`
	DBDSN           string        `envconfig:"WVS_DB_DSN" required:"true"`
	MetricsAddr     string        `envconfig:"WVS_METRICS_ADDR" default:"0.0.0.0:9090"`
	LogLevel        string        `envconfig:"WVS_LOG_LEVEL" default:"info"`
	ShutdownTimeout time.Duration `envconfig:"WVS_SHUTDOWN_TIMEOUT" default:"30s"`
}
