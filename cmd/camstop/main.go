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
	"github.com/smford/camstop/internal/discovery"
	"github.com/smford/camstop/internal/mqtt"
)

// Injected by ldflags during build / release
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	configPath := flag.String("config", "camstop.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Print version information and exit")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	scanNetwork := flag.Bool("scan", false, "Scan local network for ONVIF and RTSP cameras")
	scanTimeout := flag.Duration("scan-timeout", 3*time.Second, "Discovery scan timeout")
	genConfig := flag.Bool("generate-config", false, "Generate sample YAML config from discovered cameras")
	flag.Parse()

	if *showVersion {
		fmt.Printf("camstop version %s (commit: %s, built at: %s)\n", version, commit, date)
		os.Exit(0)
	}

	// Network scanning mode
	if *scanNetwork {
		runScan(*scanTimeout, *genConfig)
		os.Exit(0)
	}

	// Daemon mode
	runDaemon(*configPath, *logLevel)
}

func runScan(timeout time.Duration, generateConfig bool) {
	fmt.Printf("Scanning local network for ONVIF and RTSP cameras (timeout: %v)...\n", timeout)

	ctx, cancel := context.WithTimeout(context.Background(), timeout+1*time.Second)
	defer cancel()

	opts := discovery.DefaultScanOptions()
	opts.Timeout = timeout

	devices, err := discovery.Scan(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning network: %v\n", err)
		os.Exit(1)
	}

	discovery.PrintTable(devices)

	if len(devices) > 0 {
		if generateConfig {
			fmt.Println(discovery.GenerateSampleConfig(devices))
		} else {
			fmt.Println("Tip: Run 'camstop --scan --generate-config' to generate ready-to-use YAML configuration.")
		}
	}
}

func runDaemon(configPath, logLevelStr string) {
	var level slog.Level
	switch logLevelStr {
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

	slog.Info("starting camstop edge daemon",
		"version", version,
		"commit", commit,
		"built_at", date,
	)

	// Fallback to camtap.yaml or config.yaml if camstop.yaml doesn't exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if _, err := os.Stat("camtap.yaml"); err == nil {
			configPath = "camtap.yaml"
		} else if _, err := os.Stat("config.yaml"); err == nil {
			configPath = "config.yaml"
		}
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load configuration", "path", configPath, "err", err)
		os.Exit(1)
	}

	slog.Info("configuration loaded",
		"path", configPath,
		"camera_count", len(cfg.Cameras),
		"mqtt_enabled", cfg.MQTT.Enabled,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mqttPub, err := mqtt.NewPublisher(cfg.MQTT)
	if err != nil {
		slog.Warn("failed to initialize MQTT publisher", "err", err)
	} else if cfg.MQTT.Enabled {
		defer mqttPub.Close()
	}

	mgr := camera.NewManager(cfg, mqttPub)
	if err := mgr.Start(ctx); err != nil {
		slog.Error("failed to start camera manager", "err", err)
		os.Exit(1)
	}

	server := api.NewServer(cfg.Server, mgr)

	serverErrChan := make(chan error, 1)
	go func() {
		serverErrChan <- server.Start()
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down daemon gracefully...")
	case err := <-serverErrChan:
		if err != nil {
			slog.Error("fatal HTTP server error", "err", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown error", "err", err)
	}

	slog.Info("camstop daemon terminated cleanly")
}
