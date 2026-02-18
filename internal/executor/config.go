package executor

import "time"

type Config struct {
	MountPath       string        `envconfig:"EXECUTOR_MOUNT_PATH" default:"/ws"`
	GRPCAddr        string        `envconfig:"EXECUTOR_GRPC_ADDR" default:"0.0.0.0:7070"`
	MetricsAddr     string        `envconfig:"EXECUTOR_METRICS_ADDR" default:"0.0.0.0:9092"`
	TaskTimeout     time.Duration `envconfig:"EXECUTOR_TASK_TIMEOUT" default:"300s"`
	QuiesceTimeout  time.Duration `envconfig:"EXECUTOR_QUIESCE_TIMEOUT" default:"30s"`
	ShutdownTimeout time.Duration `envconfig:"EXECUTOR_SHUTDOWN_TIMEOUT" default:"300s"`
	JFSMetaURL      string        `envconfig:"JFS_META_URL" required:"true"`
	MinioEndpoint   string        `envconfig:"MINIO_ENDPOINT" required:"true"`
	MinioAccessKey  string        `envconfig:"MINIO_ACCESS_KEY" required:"true"`
	MinioSecretKey  string        `envconfig:"MINIO_SECRET_KEY" required:"true"`
	MinioBucket     string        `envconfig:"MINIO_BUCKET" default:"jfs-data"`
}
