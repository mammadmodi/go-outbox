package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
}

// NewApp creates a new App instance.
func NewApp(storage *outbox.SQLStorage, db *sql.DB, port string) *App {
	return &App{
		outboxStorage: storage,
		db:            db,
		port:          port,
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
	router.HandleFunc("PUT /users/", a.updateUserHandler) // expects ID at the end

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
	defer tx.Rollback()

	var userID int64
	err = tx.QueryRowContext(ctx,
		"INSERT INTO users (name, email) VALUES ($1, $2) RETURNING id",
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

// updateUserHandler updates a user and stores an update event in the outbox.
func (a *App) updateUserHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 || parts[2] == "" {
		http.Error(w, "invalid user ID in path", http.StatusBadRequest)
		return
	}
	userID := parts[2]

	type UpdateUserRequest struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		http.Error(w, "failed to start transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		"UPDATE users SET name = $1, email = $2, updated_at = now() WHERE id = $3",
		req.Name, req.Email, userID,
	)
	if err != nil {
		http.Error(w, "failed to update user", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "failed to fetch update result", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	event := outbox.StorageRecord{
		EventType:     "UserUpdated",
		AggregateType: "User",
		AggregateID:   userID,
		Data:          []byte(fmt.Sprintf(`{"id":%s,"name":"%s","email":"%s"}`, userID, req.Name, req.Email)),
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": userID})
}

func (a *App) initUsersTable(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	);
	`
	if _, err := a.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to initialize users table: %w", err)
	}

	return nil
}
