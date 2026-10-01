package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/smford/camstop/internal/api"
	"github.com/smford/camstop/internal/camera"
	"github.com/smford/camstop/internal/config"
	"github.com/smford/camstop/internal/mqtt"
)

// Injected by ldflags during build / release
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	configPath := flag.String("config", "camtap.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Print version information and exit")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("camtap version %s (commit: %s, built at: %s)\n", version, commit, date)
		os.Exit(0)
	}

	// Setup structured logging with slog
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	slog.Info("starting camtap edge daemon",
		"version", version,
		"commit", commit,
		"built_at", date,
	)

	// Fallback to config.yaml if camtap.yaml doesn't exist
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		if _, err := os.Stat("config.yaml"); err == nil {
			*configPath = "config.yaml"
		}
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load configuration", "path", *configPath, "err", err)
		os.Exit(1)
	}

	slog.Info("configuration loaded",
		"path", *configPath,
		"camera_count", len(cfg.Cameras),
		"mqtt_enabled", cfg.MQTT.Enabled,
	)

	// Context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Initialize MQTT publisher
	mqttPub, err := mqtt.NewPublisher(cfg.MQTT)
	if err != nil {
		slog.Warn("failed to initialize MQTT publisher", "err", err)
	} else if cfg.MQTT.Enabled {
		defer mqttPub.Close()
	}

	// Initialize Camera Manager
	mgr := camera.NewManager(cfg, mqttPub)
	if err := mgr.Start(ctx); err != nil {
		slog.Error("failed to start camera manager", "err", err)
		os.Exit(1)
	}

	// Initialize HTTP Server
	server := api.NewServer(cfg.Server, mgr)

	// Start server in background
	serverErrChan := make(chan error, 1)
	go func() {
		serverErrChan <- server.Start()
	}()

	// Wait for termination signal or server error
	select {
	case <-ctx.Done():
		slog.Info("shutting down daemon gracefully...")
	case err := <-serverErrChan:
		if err != nil {
			slog.Error("fatal HTTP server error", "err", err)
		}
	}

	// Clean shutdown with 5s timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown error", "err", err)
	}

	slog.Info("camtap daemon terminated cleanly")
}
