package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	RecordStatusPending = "pending"
	RecordStatusSent    = "sent"
	RecordStatusDead    = "dead"
)

// StorageRecord represents a message stored in the outbox table.
type StorageRecord struct {
	ID            uuid.UUID  `db:"id"`
	EventType     string     `db:"event_type"`
	AggregateType string     `db:"aggregate_type"`
	AggregateID   string     `db:"aggregate_id"`
	Data          []byte     `db:"data"`
	CreatedAt     time.Time  `db:"created_at"`
	SentAt        *time.Time `db:"sent_at"`
	Status        string     `db:"status"`
	Attempts      int        `db:"attempts"`
	Topic         string     `db:"topic"`
}

// SQLStorage provides DB operations for the outbox pattern.
type SQLStorage struct {
	db *sql.DB
}

// NewSQLStorage creates a new SQLStorage instance.
func NewSQLStorage(db *sql.DB) *SQLStorage {
	return &SQLStorage{
		db: db,
	}
}

// InitOutboxTable creates the outbox table if it doesn't exist yet.
func (s *SQLStorage) InitOutboxTable(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS outbox (
		id UUID PRIMARY KEY,
		event_type TEXT NOT NULL,
		aggregate_type TEXT NOT NULL,
		aggregate_id TEXT NOT NULL,
		data BYTEA NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		sent_at TIMESTAMPTZ,
		status TEXT NOT NULL,
		attempts INT NOT NULL DEFAULT 0,
		topic TEXT NOT NULL
	);
	
	CREATE INDEX IF NOT EXISTS idx_outbox_aggregate_type_id
	ON outbox (aggregate_type, aggregate_id);
	`

	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to initialize outbox table and indexes: %w", err)
	}

	return nil
}

// InsertMessage inserts a new message into the outbox table.
func (s *SQLStorage) InsertMessage(ctx context.Context, tx *sql.Tx, msg StorageRecord) error {
	query := `
        INSERT INTO outbox (id, event_type, aggregate_type, aggregate_id, data, topic, created_at, status, attempts)
        VALUES ($1, $2, $3, $4, $5, NOW(), $6, $7, 0)
    `

	if msg.ID == uuid.Nil {
		msg.ID = uuid.New()
	}

	_, err := tx.ExecContext(ctx, query,
		uuid.New(),
		msg.EventType,
		msg.AggregateType,
		msg.AggregateID,
		msg.Data,
		msg.Topic,
		RecordStatusPending,
	)

	return err
}

// FetchPendingMessages retrieves pending messages ordered by creation time.
func (s *SQLStorage) FetchPendingMessages(ctx context.Context, batchSize int) ([]*StorageRecord, error) {
	const query = `
        WITH next_events AS (
            SELECT DISTINCT ON (aggregate_type, aggregate_id)
                id, event_type, aggregate_type, aggregate_id, data, created_at, status, attempts, topic
            FROM outbox
            WHERE status = $1
            ORDER BY aggregate_type, aggregate_id, created_at ASC
        )
        SELECT * FROM next_events
        ORDER BY created_at ASC
        LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, query, RecordStatusPending, batchSize)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pending messages: %w", err)
	}
	defer rows.Close()

	var records []*StorageRecord
	for rows.Next() {
		var rec StorageRecord
		if err = rows.Scan(
			&rec.ID,
			&rec.EventType,
			&rec.AggregateType,
			&rec.AggregateID,
			&rec.Data,
			&rec.CreatedAt,
			&rec.Status,
			&rec.Attempts,
			&rec.Topic,
		); err != nil {
			return nil, fmt.Errorf("failed to scan outbox record: %w", err)
		}
		records = append(records, &rec)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return records, nil
}

// MarkMessageSent marks a message as successfully sent.
func (s *SQLStorage) MarkMessageSent(ctx context.Context, id string) error {
	const query = `
		UPDATE outbox
		SET status = $1, sent_at = NOW()
		WHERE id = $2
	`
	result, err := s.db.ExecContext(ctx, query, RecordStatusSent, id)
	if err != nil {
		return fmt.Errorf("failed to update message status to sent: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rowsAffected == 0 {
		return errors.New("no rows updated")
	}
	return nil
}

// IncrementAttempt increments the attempt count for a message.
func (s *SQLStorage) IncrementAttempt(ctx context.Context, id string) error {
	const query = `
		UPDATE outbox
		SET attempts = attempts + 1
		WHERE id = $1
	`
	if _, err := s.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("failed to increment attempt count: %w", err)
	}
	return nil
}

// MarkMessageDead marks a message as dead (failed permanently).
func (s *SQLStorage) MarkMessageDead(ctx context.Context, id string) error {
	const query = `
		UPDATE outbox
		SET status = $1, sent_at = NOW()
		WHERE id = $2
	`
	if _, err := s.db.ExecContext(ctx, query, RecordStatusDead, id); err != nil {
		return fmt.Errorf("failed to mark message as dead: %w", err)
	}
	return nil
}
