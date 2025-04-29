package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"

	"github.com/mammadmodi/go-outbox/cmd/outbox-relay/config"
	"github.com/mammadmodi/go-outbox/cmd/pkg/logging"
	"github.com/mammadmodi/go-outbox/outbox"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "./config/relay.toml"
	}

	// Load configuration
	appCfg, err := config.NewConfig(cfgPath)
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

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("shutdown signal received", slog.String("signal", sig.String()))
		relay.ShutDown()
		cancel()
	}()

	// Start relay
	if err = relay.Start(ctx); err != nil {
		logger.Error("relay failed to start", slog.Any("error", err))

		os.Exit(1)
	}

	logger.Info("shutdown complete")
}
