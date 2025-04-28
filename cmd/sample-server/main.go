package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"

	"github.com/mammadmodi/go-outbox/cmd/sample-server/app"
	"github.com/mammadmodi/go-outbox/outbox"
	"github.com/mammadmodi/go-outbox/pkg/logging"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "./config/sample-server.toml"
	}

	// Load configuration
	appCfg, err := app.NewConfig(cfgPath)
	if err != nil {
		slog.Error("failed to load config", slog.Any("error", err))
		os.Exit(1)
	}

	logger, err := logging.NewSlogger(appCfg.LogLevel, appCfg.LogFormat)
	if err != nil {
		slog.Error("failed to create logger", slog.Any("error", err))
		os.Exit(1)
	}

	// Connect to PostgreSQL
	db, err := sql.Open("postgres", appCfg.DatabaseDSN)
	if err != nil {
		logger.Error("failed to connect to database", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		if err = db.Close(); err != nil {
			logger.Error("failed to close database connection", slog.Any("error", err))
		}
	}()

	if err = db.PingContext(ctx); err != nil {
		logger.Error("failed to ping database", slog.Any("error", err))
		os.Exit(1)
	}

	// Connect to NATS
	nc, err := nats.Connect(appCfg.NatsURL)
	if err != nil {
		logger.Error("failed to connect to NATS", slog.Any("error", err))
		os.Exit(1)
	}
	defer nc.Close()

	// Initialize components
	storage := outbox.NewSQLStorage(db)
	if err = storage.InitOutboxTable(ctx); err != nil {
		logger.Error("failed to initialize outbox table", slog.Any("error", err))
		os.Exit(1)
	}

	publisher := outbox.NewNatsPublisher(nc, logger)
	elector := outbox.NewLeaseElector(db, appCfg.AdvisoryLock, logger)
	relay := outbox.NewRelay(storage, publisher, elector, appCfg.Relay, logger)

	sampleServer := app.NewApp(storage, db, appCfg.ServerPort)
	if err = sampleServer.Init(ctx); err != nil {
		logger.Error("failed to initialize sample server", slog.Any("error", err))
		os.Exit(1)
	}

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup
	wg.Add(2)

	// Start relay
	go func() {
		defer wg.Done()
		logger.Info("starting outbox relay...")
		if err = relay.Start(ctx); err != nil {
			logger.Error("relay stopped with error", slog.Any("error", err))
		}
	}()

	// Start HTTP server
	go func() {
		defer wg.Done()
		logger.Info("starting sample HTTP server...")
		if err = sampleServer.Start(); err != nil && err != context.Canceled && err != http.ErrServerClosed {
			logger.Error("server stopped with error", slog.Any("error", err))
		}
	}()

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Info("shutdown signal received", slog.String("signal", sig.String()))

	// Trigger shutdown
	cancel()
	relay.ShutDown()
	_ = sampleServer.Stop(context.Background())

	wg.Wait()

	logger.Info("graceful shutdown complete")
}
