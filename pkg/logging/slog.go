// Package logging provides a simple wrapper around the slog package for structured logging.
package logging

import (
	"fmt"
	"log/slog"
	"os"
)

type Config struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

const (
	FormatJSON = "json"
	FormatText = "text"
)

// NewSlogger creates a new slog.Logger instance based on the provided configuration.
func NewSlogger(levelStr, format string) (*slog.Logger, error) {
	var level slog.Level
	switch levelStr {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return nil, fmt.Errorf("invalid log level: %s", levelStr)
	}

	handlerOptions := &slog.HandlerOptions{
		Level: level,
	}

	var h slog.Handler
	switch format {
	case FormatJSON:
		h = slog.NewJSONHandler(os.Stdout, handlerOptions)
	case FormatText:
		h = slog.NewTextHandler(os.Stdout, handlerOptions)
	default:
		h = slog.NewTextHandler(os.Stdout, handlerOptions)
	}
	logger := slog.New(h)

	return logger, nil
}
