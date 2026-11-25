package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/blalange/auto-netviz/internal/db"
	"github.com/blalange/auto-netviz/internal/manual"
	"github.com/blalange/auto-netviz/internal/server"
)

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	listen := flag.String("listen", envOrDefault("AUTONETVIZ_LISTEN", ":8080"), "HTTP/WebSocket listen address")
	dbPath := flag.String("db", envOrDefault("AUTONETVIZ_DB_PATH", "./data/auto-netviz.db"), "SQLite database path")
	manualDir := flag.String("manual", envOrDefault("AUTONETVIZ_MANUAL_DIR", "./examples/manual"), "Manual YAML directory")
	retentionEnv := envOrDefault("AUTONETVIZ_RETENTION_DAYS", "30")
	retDefault, _ := strconv.Atoi(retentionEnv)
	if retDefault <= 0 {
		retDefault = 30
	}
	retentionDays := flag.Int("retention-days", retDefault, "Time Machine full snapshot retention in days")
	logLevel := flag.String("log-level", envOrDefault("AUTONETVIZ_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	migrateOnly := flag.Bool("migrate-only", false, "Run SQLite Goose migrations and exit")
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := db.Open(ctx, *dbPath)
	if err != nil {
		logger.Error("failed opening sqlite store", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	if *migrateOnly {
		logger.Info("goose migrations applied", "db", *dbPath)
		return
	}

	inv, err := manual.LoadDir(*manualDir)
	if err != nil {
		logger.Error("failed loading manual YAML", "dir", *manualDir, "err", err)
		os.Exit(1)
	}
	logger.Info("loaded manual YAML",
		"dir", *manualDir,
		"files", len(inv.FilesLoaded),
		"devices", len(inv.Devices),
		"racks", len(inv.Racks),
		"issues", len(inv.Issues),
	)

	coord := server.NewCoordinator(store, inv, logger)
	if err := coord.UpdateManual(ctx, inv); err != nil {
		logger.Error("initial reconcile failed", "err", err)
		os.Exit(1)
	}
	coord.StartMaintenance(ctx, *retentionDays)

	if *manualDir != "" {
		reloadCh := manual.Watch(ctx, *manualDir, inv.Hash, 30*time.Second, logger)
		go func() {
			for newInv := range reloadCh {
				logger.Info("manual YAML change detected, reloading", "files", len(newInv.FilesLoaded), "hash", newInv.Hash[:8])
				if err := coord.UpdateManual(ctx, newInv); err != nil {
					logger.Error("manual YAML reload reconcile failed", "err", err)
				}
			}
		}()
	}

	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           server.NewRouter(coord),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("auto-netviz server listening", "addr", *listen)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down server")
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server exited with error", "err", err)
			_ = io.Discard
			os.Exit(1)
		}
	}
}
