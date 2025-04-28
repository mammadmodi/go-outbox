package outbox

import (
	"log/slog"

	"github.com/nats-io/nats.go"
)

var ErrMissingTopic = nats.ErrBadSubject

// NatsPublisher implements the Publisher interface with the NATS messaging system.
type NatsPublisher struct {
	conn   *nats.Conn
	logger *slog.Logger
}

// NewNatsPublisher creates a new NatsPublisher.
func NewNatsPublisher(conn *nats.Conn, logger *slog.Logger) *NatsPublisher {
	return &NatsPublisher{
		conn:   conn,
		logger: logger,
	}
}

// Publish sends an Outbox message to NATS, using EventType as a header.
func (p *NatsPublisher) Publish(msg *StorageRecord) error {
	if msg.Topic == "" {
		p.logger.
			With(slog.String("message_id", msg.ID.String())).
			Error("Publisher: missing topic in message")
		return ErrMissingTopic
	}

	headers := nats.Header{}
	headers.Set("event-type", msg.EventType)
	headers.Set("aggregate-type", msg.AggregateType)
	headers.Set("aggregate-id", msg.AggregateID)

	if err := p.conn.PublishMsg(&nats.Msg{
		Subject: msg.Topic,
		Data:    msg.Data,
		Header:  headers,
	}); err != nil {
		p.logger.
			With(slog.String("message_id", msg.ID.String()), slog.String("subject", msg.Topic), slog.Any("error", err)).
			Error("Publisher: failed to publish to NATS")
		return err
	}

	p.logger.
		With(slog.String("message_id", msg.ID.String()), slog.String("subject", msg.Topic)).
		Info("Publisher: successfully published message")

	return nil
}
