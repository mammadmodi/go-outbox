package outbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLeaseElector_IsLeader(t *testing.T) {
	// Create a mock database connection.
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	noopLogger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	testLockID := int64(42)
	elector := NewLeaseElector(db, testLockID, noopLogger)

	ctx := context.Background()

	tests := []struct {
		name           string
		mockSetup      func()
		expectedError  bool
		expectedLeader bool
	}{
		{
			name: "#1 Successfully acquires the lock",
			mockSetup: func() {
				mock.ExpectQuery("SELECT pg_try_advisory_lock\\(\\$1\\)").
					WithArgs(testLockID).
					WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
			},
			expectedError:  false,
			expectedLeader: true,
		},
		{
			name: "#2 Fails to acquire the lock",
			mockSetup: func() {
				mock.ExpectQuery("SELECT pg_try_advisory_lock\\(\\$1\\)").
					WithArgs(testLockID).
					WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(false))
			},
			expectedError:  false,
			expectedLeader: false,
		},
		{
			name: "#3 Database error",
			mockSetup: func() {
				mock.ExpectQuery("SELECT pg_try_advisory_lock\\(\\$1\\)").
					WithArgs(testLockID).
					WillReturnError(errors.New("database error"))
			},
			expectedError:  true,
			expectedLeader: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup()

			isLeader, err := elector.IsLeader(ctx)
			if (err != nil) != tt.expectedError {
				t.Errorf("unexpected error: got %v, want error=%v", err, tt.expectedError)
			}
			if isLeader != tt.expectedLeader {
				t.Errorf("unexpected leader status: got %v, want %v", isLeader, tt.expectedLeader)
			}
		})
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %v", err)
	}
}
