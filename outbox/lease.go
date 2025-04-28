package outbox

import (
	"context"
	"database/sql"
	"log/slog"
)

// LeaseElector implements a simple LeaderElector based on a Postgres advisory lock.
type LeaseElector struct {
	db     *sql.DB
	lockID int64
	logger *slog.Logger
}

// NewLeaseElector creates a new LeaseElector with given Postgres connection and lock ID.
func NewLeaseElector(db *sql.DB, lockID int64, logger *slog.Logger) *LeaseElector {
	return &LeaseElector{
		db:     db,
		lockID: lockID,
		logger: logger,
	}
}

// IsLeader tries to acquire a session-based advisory lock.
func (l *LeaseElector) IsLeader(ctx context.Context) (bool, error) {
	var success bool
	if err := l.db.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", l.lockID).Scan(&success); err != nil {
		l.logger.Error("Lease: failed to acquire advisory lock", slog.Any("error", err))

		return false, err
	}

	if success {
		l.logger.Info("Lease: leadership acquired")
	} else {
		l.logger.Info("Lease: leadership NOT acquired")
	}

	return success, nil
}
