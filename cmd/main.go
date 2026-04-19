package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"os"
	"os/signal"
	"syscall"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"

	operatordb "github.com/ccvass/swarmex/swarmex-operator-db"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Error("failed to create Docker client", "error", err)
		os.Exit(1)
	}
	defer cli.Close()

	op := operatordb.New(cli, logger)

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "ok, managing %d services", op.ManagedServices())
		})
		logger.Info("health endpoint", "addr", ":8080")
		http.ListenAndServe(":8080", nil)
	}()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("swarmex-operator-db starting")
	go op.RunReconcileLoop(ctx)

	msgCh, errCh := cli.Events(ctx, events.ListOptions{})
	for {
		select {
		case event := <-msgCh:
			op.HandleEvent(ctx, event)
		case err := <-errCh:
			if ctx.Err() != nil {
				logger.Info("shutdown complete")
				return
			}
			logger.Error("event stream error", "error", err)
			return
		case <-ctx.Done():
			logger.Info("shutdown complete")
			return
		}
	}
}
