package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mammadmodi/go-outbox/outbox"
)

// App represents the main server application.
type App struct {
	db            *sql.DB
	outboxStorage *outbox.SQLStorage
	server        *http.Server
	port          string
	logger        *slog.Logger
}

// NewApp creates a new App instance.
func NewApp(storage *outbox.SQLStorage, db *sql.DB, port string, logger *slog.Logger) *App {
	return &App{
		outboxStorage: storage,
		db:            db,
		port:          port,
		logger:        logger,
	}
}

// Init initializes the HTTP server and prepares the database.
func (a *App) Init(ctx context.Context) error {
	if err := a.outboxStorage.InitOutboxTable(ctx); err != nil {
		return err
	}

	if err := a.initUsersTable(ctx); err != nil {
		return err
	}

	router := http.NewServeMux()
	router.HandleFunc("POST /users", a.createUserHandler)
	router.HandleFunc("PATCH /users/", a.verifyUserHandler) // will route like /users/{id}/verify

	a.server = &http.Server{
		Addr:    ":" + a.port,
		Handler: router,
	}

	return nil
}

// Start starts the HTTP server.
func (a *App) Start() error {
	return a.server.ListenAndServe()
}

// Stop gracefully shuts down the HTTP server.
func (a *App) Stop(ctx context.Context) error {
	return a.server.Shutdown(ctx)
}

// createUserHandler creates a new user and stores an event in the outbox.
func (a *App) createUserHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	type CreateUserRequest struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		http.Error(w, "failed to start transaction", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err = tx.Rollback(); err != nil {
			a.logger.Error("failed to rollback transaction", slog.Any("error", err))
		}
	}()

	var userID int64
	err = tx.QueryRowContext(ctx,
		"INSERT INTO users (name, email, verified) VALUES ($1, $2, false) RETURNING id",
		req.Name, req.Email,
	).Scan(&userID)
	if err != nil {
		http.Error(w, "failed to insert user", http.StatusInternalServerError)
		return
	}

	event := outbox.StorageRecord{
		EventType:     "UserCreated",
		AggregateType: "User",
		AggregateID:   fmt.Sprintf("%d", userID),
		Data:          []byte(fmt.Sprintf(`{"id":%d,"name":"%s","email":"%s"}`, userID, req.Name, req.Email)),
		Topic:         "users",
	}

	if err = a.outboxStorage.InsertMessage(ctx, tx, event); err != nil {
		http.Error(w, "failed to insert outbox message", http.StatusInternalServerError)
		return
	}

	if err = tx.Commit(); err != nil {
		http.Error(w, "failed to commit transaction", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": userID})
}

// verifyUserHandler verifies a user and stores an event in the outbox.
func (a *App) verifyUserHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Expect URL like /users/{id}/verify
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 4 || parts[3] != "verify" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	userID := parts[2]

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		http.Error(w, "failed to start transaction", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err = tx.Rollback(); err != nil {
			a.logger.Error("failed to rollback transaction", slog.Any("error", err))
		}
	}()

	// Check if user exists and is not already verified
	var verified bool
	err = tx.QueryRowContext(ctx, "SELECT verified FROM users WHERE id = $1", userID).Scan(&verified)
	if err == sql.ErrNoRows {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to query user", http.StatusInternalServerError)
		return
	}
	if verified {
		http.Error(w, "user already verified", http.StatusConflict)
		return
	}

	// Mark as verified
	_, err = tx.ExecContext(ctx, `
		UPDATE users
		SET verified = true, updated_at = now()
		WHERE id = $1
	`, userID)
	if err != nil {
		http.Error(w, "failed to update user", http.StatusInternalServerError)
		return
	}

	// Insert verification event
	event := outbox.StorageRecord{
		EventType:     "UserVerified",
		AggregateType: "User",
		AggregateID:   userID,
		Data:          []byte(fmt.Sprintf(`{"id":%s,"verified":true}`, userID)),
		Topic:         "users",
	}

	if err = a.outboxStorage.InsertMessage(ctx, tx, event); err != nil {
		http.Error(w, "failed to insert outbox message", http.StatusInternalServerError)
		return
	}

	if err = tx.Commit(); err != nil {
		http.Error(w, "failed to commit transaction", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "verified"})
}

func (a *App) initUsersTable(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT NOT NULL,
		verified BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	);
	`

	if _, err := a.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to initialize users table: %w", err)
	}

	return nil
}
