package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/lzjever/mbos-wvs/internal/executorclient"
	"github.com/lzjever/mbos-wvs/internal/observability"
	"github.com/lzjever/mbos-wvs/internal/store"
	"github.com/lzjever/mbos-wvs/internal/worker"
)

func main() {
	var cfg worker.Config
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	log, _ := observability.NewLogger(cfg.LogLevel)
	defer log.Sync()

	reg := prometheus.DefaultRegisterer
	observability.RegisterAll(reg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := store.NewPool(ctx, cfg.DBDSN)
	if err != nil {
		log.Fatal("db connect failed", zap.Error(err))
	}
	defer pool.Close()

	exec, err := executorclient.New(cfg.ExecutorAddrs)
	if err != nil {
		log.Fatal("executor connect failed", zap.Error(err))
	}
	defer exec.Close()

	// Metrics server
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	go func() {
		log.Info("metrics server starting", zap.String("addr", cfg.MetricsAddr))
		if err := http.ListenAndServe(cfg.MetricsAddr, mux); err != nil {
			log.Fatal("metrics server failed", zap.Error(err))
		}
	}()

	w := worker.New(pool, exec, cfg, log)
	w.Run(ctx)
}
