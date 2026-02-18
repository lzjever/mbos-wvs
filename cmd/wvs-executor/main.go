package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/lzjever/mbos-wvs/internal/executor"
	"github.com/lzjever/mbos-wvs/internal/observability"
	pb "github.com/lzjever/mbos-wvs/gen/go/executor/v1"
)

func main() {
	var cfg executor.Config
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	log, _ := observability.NewLogger(os.Getenv("WVS_LOG_LEVEL"))
	defer log.Sync()

	reg := prometheus.DefaultRegisterer
	observability.RegisterAll(reg)

	// Metrics HTTP server
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	go func() {
		log.Info("metrics server starting", zap.String("addr", cfg.MetricsAddr))
		if err := http.ListenAndServe(cfg.MetricsAddr, mux); err != nil {
			log.Fatal("metrics server failed", zap.Error(err))
		}
	}()

	// gRPC server
	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		log.Fatal("listen failed", zap.Error(err))
	}

	srv := grpc.NewServer()
	pb.RegisterExecutorServiceServer(srv, executor.NewServer(cfg, log))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		log.Info("gRPC server starting", zap.String("addr", cfg.GRPCAddr))
		if err := srv.Serve(lis); err != nil {
			log.Fatal("grpc serve failed", zap.Error(err))
		}
	}()

	<-ctx.Done()
	log.Info("shutting down executor")
	srv.GracefulStop()
}
