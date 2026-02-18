package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/lzjever/mbos-wvs/internal/api"
	"github.com/lzjever/mbos-wvs/internal/observability"
	"github.com/lzjever/mbos-wvs/internal/store"
)

func main() {
	var cfg api.Config
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	log, _ := observability.NewLogger(cfg.LogLevel)
	defer log.Sync()

	// Replace global logger
	zap.ReplaceGlobals(log)

	reg := prometheus.DefaultRegisterer
	observability.RegisterAll(reg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := store.NewPool(ctx, cfg.DBDSN)
	if err != nil {
		log.Fatal("db connect failed", zap.Error(err))
	}
	defer pool.Close()

	// Main API server
	apiHandler := api.NewAPI(pool, log)
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      apiHandler.Router(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Metrics server
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	metricsSrv := &http.Server{
		Addr:    cfg.MetricsAddr,
		Handler: mux,
	}

	go func() {
		log.Info("metrics server starting", zap.String("addr", cfg.MetricsAddr))
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("metrics server failed", zap.Error(err))
		}
	}()

	go func() {
		log.Info("API server starting", zap.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("API server failed", zap.Error(err))
		}
	}()

	<-ctx.Done()
	log.Info("shutting down API server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	_ = srv.Shutdown(shutdownCtx)
	_ = metricsSrv.Shutdown(shutdownCtx)

	log.Info("API server stopped")
}
