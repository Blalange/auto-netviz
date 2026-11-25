package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/blalange/auto-netviz/internal/collector"
	"github.com/blalange/auto-netviz/internal/collector/config"
)

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	cfgPath := flag.String("config", envOrDefault("AUTONETVIZ_COLLECTOR_CONFIG", ""), "Path to collector.yaml")
	serverWS := flag.String("server-ws", envOrDefault("AUTONETVIZ_SERVER_WS_URL", ""), "Override backend server WebSocket URL")
	collectorID := flag.String("id", envOrDefault("AUTONETVIZ_COLLECTOR_ID", ""), "Override collector_id")
	site := flag.String("site", envOrDefault("AUTONETVIZ_SITE", ""), "Override site slug")
	once := flag.Bool("once", false, "Run all enabled sources once, send observations, and exit")
	demo := flag.Bool("demo", false, "Emit deterministic simulated multi-source observations (no live hardware needed)")
	logLevel := flag.String("log-level", envOrDefault("AUTONETVIZ_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.Parse()

	lvl := slog.LevelInfo
	switch *logLevel {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))

	cfg, err := config.LoadConfig(*cfgPath)
	if err != nil {
		logger.Error("failed loading collector config", "err", err)
		os.Exit(1)
	}
	if *serverWS != "" {
		cfg.ServerWSURL = *serverWS
	}
	if *collectorID != "" {
		cfg.CollectorID = *collectorID
	}
	if *site != "" {
		cfg.Site = *site
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	agent := collector.NewAgent(cfg, *cfgPath, *once, *demo, logger)
	logger.Info("starting auto-netviz collector",
		"collector_id", cfg.CollectorID,
		"site", cfg.Site,
		"server_ws", cfg.ServerWSURL,
		"demo", *demo,
		"once", *once,
	)

	if err := agent.Run(ctx); err != nil {
		logger.Error("collector exited with error", "err", err)
		os.Exit(1)
	}
}
