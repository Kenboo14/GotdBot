package utils

import (
	"log/slog"
	"os"
	"strings"
)

// InitLogger initializes the global log/slog structured logger.
// If LOG_FORMAT is "text", it uses TextHandler (development).
// Otherwise, it defaults to JSONHandler (production microservice standard).
func InitLogger(component string) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if strings.ToLower(os.Getenv("LOG_FORMAT")) == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	if component != "" {
		handler = handler.WithAttrs([]slog.Attr{slog.String("component", component)})
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func Info(msg string, args ...any) {
	slog.Info(msg, args...)
}

func Warn(msg string, args ...any) {
	slog.Warn(msg, args...)
}

func Error(msg string, args ...any) {
	slog.Error(msg, args...)
}

func Debug(msg string, args ...any) {
	slog.Debug(msg, args...)
}

// LogError provides backward-compatible structured error logging
func LogError(msg string, err error, fields map[string]any) {
	if err == nil {
		return
	}
	attrs := make([]any, 0, len(fields)*2+2)
	attrs = append(attrs, "error", err.Error())
	for k, v := range fields {
		attrs = append(attrs, k, v)
	}
	slog.Error(msg, attrs...)
}
