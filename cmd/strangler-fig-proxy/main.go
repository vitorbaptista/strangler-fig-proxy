package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vitorbaptista/strangler-fig-proxy/pkg/proxy"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	config, err := LoadConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	database, err := proxy.InitDatabase(config.DatabasePath)
	if err != nil {
		slog.Error("failed to initialize database", "path", config.DatabasePath, "error", err)
		os.Exit(1)
	}
	defer database.Close()

	stopMaintenance := database.StartMaintenance(config.DatabaseRetentionDays, config.DatabaseMaxSizeMB, time.Hour)
	defer stopMaintenance()

	handler := proxy.NewHandler(config, database)

	server := &http.Server{
		Addr:    ":" + config.Port,
		Handler: handler,
	}

	go func() {
		slog.Info("starting strangler fig proxy",
			"port", config.Port,
			"main_server", config.MainServerURL,
			"new_server", config.NewServerURL,
			"database", config.DatabasePath,
			"sampling_rate", config.SamplingRate,
			"dashboard", "http://localhost:"+config.Port+"/__strangler_fig")

		if routes := handler.Routes().Routes(); len(routes) > 0 {
			for _, route := range routes {
				slog.Info("route configured", "prefix", route.Prefix, "percentage", route.Percentage)
			}
		} else {
			slog.Info("no routes configured - all traffic served by main server")
		}

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server exited")
}
