package outbox

import (
	"context"
	"log/slog"
	"time"
)

type (
	// Relay is responsible for polling the outbox table and publishing messages.
	Relay struct {
		logger    *slog.Logger
		storage   Storage
		publisher Publisher
		done      chan struct{}
		leader    LeaderElector
		cfg       RelayConfig
	}

	// RelayConfig holds the configuration for the outbox relay (polling loop).
	RelayConfig struct {
		// PollInterval is the interval between polling the outbox table.
		PollInterval time.Duration `mapstructure:"poll_interval"`
		// BatchSize is the number of messages to fetch in each poll.
		BatchSize int `mapstructure:"batch_size"`
		// MaxAttempts is the maximum number of attempts to publish a message before marking it as dead.
		MaxAttempts int `mapstructure:"max_attempts"`
	}

	// Storage abstracts DB access.
	Storage interface {
		FetchPendingMessages(ctx context.Context, limit int) ([]*StorageRecord, error)
		MarkMessageSent(ctx context.Context, messageID string) error
		IncrementAttempt(ctx context.Context, messageID string) error
		MarkMessageDead(ctx context.Context, messageID string) error
	}

	// Publisher abstracts NATS (or any broker) publishing.
	Publisher interface {
		Publish(msg *StorageRecord) error
	}

	// LeaderElector abstracts leader election mechanism.
	LeaderElector interface {
		IsLeader(ctx context.Context) (bool, error)
	}
)

// NewRelay creates a new Relay.
func NewRelay(storage Storage, publisher Publisher, leader LeaderElector, cfg RelayConfig, logger *slog.Logger) *Relay {
	return &Relay{
		storage:   storage,
		publisher: publisher,
		leader:    leader,
		cfg:       cfg,
		logger:    logger,
		done:      make(chan struct{}),
	}
}

// Start starts the relay polling loop.
func (r *Relay) Start(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("Relay: context canceled, stopping")
			return ctx.Err()
		case <-r.done:
			r.logger.Info("Relay: done signal received, stopping")
			return nil
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Relay) tick(ctx context.Context) {
	isLeader, err := r.leader.IsLeader(ctx)
	if err != nil {
		r.logger.Error("Relay: failed to check leadership", slog.Any("error", err))

		return
	}

	if !isLeader {
		r.logger.Debug("Relay: not leader, skipping tick")

		return
	}

	r.processMessages(ctx)
}

func (r *Relay) processMessages(ctx context.Context) {
	messages, err := r.storage.FetchPendingMessages(ctx, r.cfg.BatchSize)
	if err != nil {
		r.logger.Error("Relay: failed to fetch messages", slog.Any("error", err))

		return
	}

	if len(messages) == 0 {
		r.logger.Debug("Relay: no pending messages found")

		return
	}

	for _, msg := range messages {
		// Check if message exceeded max attempts
		if msg.Attempts >= r.cfg.MaxAttempts {
			r.logger.
				With(slog.String("message_id", msg.ID.String()), slog.Int("attempts", msg.Attempts)).
				Warn("Relay: message exceeded max attempts, marking as dead")

			if err = r.storage.MarkMessageDead(ctx, msg.ID.String()); err != nil {
				r.logger.
					With(slog.String("message_id", msg.ID.String()), slog.Any("error", err)).
					Error("Relay: failed to mark message as dead")
			}

			continue
		}

		if err = r.publisher.Publish(msg); err != nil {
			r.logger.
				With(slog.String("message_id", msg.ID.String()), slog.Any("error", err)).
				Error("Relay: failed to publish message")

			// Increment attempt count on failure
			if incErr := r.storage.IncrementAttempt(ctx, msg.ID.String()); incErr != nil {
				r.logger.
					With(slog.String("message_id", msg.ID.String()), slog.Any("error", incErr)).
					Error("Relay: failed to increment attempt count")
			}

			continue
		}

		// Mark message as sent
		if err = r.storage.MarkMessageSent(ctx, msg.ID.String()); err != nil {
			r.logger.
				With(slog.String("message_id", msg.ID.String()), slog.Any("error", err)).
				Error("Relay: failed to mark message as sent")

			continue
		}

		r.logger.With(slog.String("message_id", msg.ID.String())).
			Info("Relay: successfully published and marked message")
	}
}

// ShutDown gracefully stops the relay.
func (r *Relay) ShutDown() {
	close(r.done)
}
