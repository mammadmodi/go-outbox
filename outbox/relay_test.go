package outbox_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mammadmodi/go-outbox/outbox"
)

// MockStorage mocks Storage interface
type MockStorage struct {
	mock.Mock
}

func (m *MockStorage) FetchPendingMessages(ctx context.Context, limit int) ([]*outbox.StorageRecord, error) {
	args := m.Called(ctx, limit)

	return args.Get(0).([]*outbox.StorageRecord), args.Error(1)
}

func (m *MockStorage) MarkMessageSent(ctx context.Context, messageID string) error {
	return m.Called(ctx, messageID).Error(0)
}

func (m *MockStorage) IncrementAttempt(ctx context.Context, messageID string) error {
	return m.Called(ctx, messageID).Error(0)
}

func (m *MockStorage) MarkMessageDead(ctx context.Context, messageID string) error {
	return m.Called(ctx, messageID).Error(0)
}

// MockPublisher mocks Publisher interface
type MockPublisher struct {
	mock.Mock
}

func (m *MockPublisher) Publish(msg *outbox.StorageRecord) error {
	return m.Called(msg).Error(0)
}

// MockLeaderElector mocks LeaderElector interface
type MockLeaderElector struct {
	mock.Mock
}

func (m *MockLeaderElector) IsLeader(ctx context.Context) (bool, error) {
	args := m.Called(ctx)

	return args.Bool(0), args.Error(1)
}

func TestRelay_Start_TableDriven(t *testing.T) {
	type fields struct {
		isLeader             bool
		isLeaderError        error
		fetchMessages        []*outbox.StorageRecord
		fetchMessagesError   error
		publishError         error
		markSentError        error
		incrementAttemptCall bool
	}
	tests := []struct {
		name   string
		fields fields
	}{
		{
			name: "#1 Successfully publish and mark sent",
			fields: fields{
				isLeader: true,
				fetchMessages: []*outbox.StorageRecord{
					{
						ID:            uuid.New(),
						EventType:     "UserCreated",
						AggregateType: "User",
						AggregateID:   "123",
						Data:          []byte(`{"name":"John"}`),
						Attempts:      0,
						Topic:         "user.created",
					},
				},
			},
		},
		{
			name: "#2 Not leader skips processing",
			fields: fields{
				isLeader: false,
			},
		},
		{
			name: "#3 Error while fetching messages",
			fields: fields{
				isLeader:           true,
				fetchMessagesError: errors.New("db error"),
			},
		},
		{
			name: "#4 Publish fails increments attempt",
			fields: fields{
				isLeader: true,
				fetchMessages: []*outbox.StorageRecord{
					{
						ID:            uuid.New(),
						EventType:     "UserCreated",
						AggregateType: "User",
						AggregateID:   "456",
						Data:          []byte(`{"name":"Doe"}`),
						Attempts:      0,
						Topic:         "user.created",
					},
				},
				publishError:         errors.New("publish failed"),
				incrementAttemptCall: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			storage := new(MockStorage)
			publisher := new(MockPublisher)
			leader := new(MockLeaderElector)

			cfg := outbox.RelayConfig{
				PollInterval: 10 * time.Millisecond,
				BatchSize:    10,
				MaxAttempts:  3,
			}

			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

			relay := outbox.NewRelay(storage, publisher, leader, cfg, logger)

			leader.On("IsLeader", mock.Anything).Return(tt.fields.isLeader, tt.fields.isLeaderError)

			if tt.fields.isLeader {
				// Only if leader, FetchPendingMessages is called
				storage.On("FetchPendingMessages", mock.Anything, cfg.BatchSize).
					Return(tt.fields.fetchMessages, tt.fields.fetchMessagesError)

				if tt.fields.fetchMessagesError == nil && len(tt.fields.fetchMessages) > 0 {
					msg := tt.fields.fetchMessages[0]

					// If publishing fails
					if tt.fields.publishError != nil {
						publisher.On("Publish", msg).Return(tt.fields.publishError)
						if tt.fields.incrementAttemptCall {
							storage.On("IncrementAttempt", mock.Anything, msg.ID.String()).Return(nil)
						}
					} else {
						publisher.On("Publish", msg).Return(nil)
						storage.On("MarkMessageSent", mock.Anything, msg.ID.String()).Return(tt.fields.markSentError)
					}
				}
			}

			var wg sync.WaitGroup
			wg.Add(1)

			// Shutdown after a short while
			go func() {
				defer wg.Done()
				time.Sleep(30 * time.Millisecond)
				relay.ShutDown()
			}()

			err := relay.Start(ctx)
			require.NoError(t, err)

			wg.Wait()

			leader.AssertExpectations(t)
			storage.AssertExpectations(t)
			publisher.AssertExpectations(t)
		})
	}
}
