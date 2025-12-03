package logger

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLogger_Info(t *testing.T) {
	logger := New("test-service")
	ctx := context.Background()

	// This will output to stdout, which we're just testing doesn't panic
	logger.Info(ctx, "test message", map[string]interface{}{
		"key": "value",
	})
}

func TestLogger_Error(t *testing.T) {
	logger := New("test-service")
	ctx := context.Background()

	logger.Error(ctx, "error message", map[string]interface{}{
		"error": "test error",
	})
}

func TestLogger_WithTraceID(t *testing.T) {
	logger := New("test-service")
	ctx := context.WithValue(context.Background(), "trace_id", "trace-123")

	logger.Info(ctx, "message with trace", nil)
}

func TestLogEntry_JSONFormat(t *testing.T) {
	entry := LogEntry{
		Timestamp: "2025-12-03T10:00:00Z",
		Level:     INFO,
		Service:   "test-service",
		TraceID:   "trace-123",
		Message:   "test message",
		Data: map[string]interface{}{
			"key": "value",
		},
	}

	jsonData, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Failed to marshal log entry: %v", err)
	}

	jsonString := string(jsonData)
	if !strings.Contains(jsonString, "test-service") {
		t.Error("JSON should contain service name")
	}
	if !strings.Contains(jsonString, "INFO") {
		t.Error("JSON should contain log level")
	}
}

func TestLogger_AllLevels(t *testing.T) {
	logger := New("test-service")
	ctx := context.Background()

	tests := []struct {
		name  string
		logFn func()
	}{
		{
			name:  "Debug",
			logFn: func() { logger.Debug(ctx, "debug msg", nil) },
		},
		{
			name:  "Info",
			logFn: func() { logger.Info(ctx, "info msg", nil) },
		},
		{
			name:  "Warn",
			logFn: func() { logger.Warn(ctx, "warn msg", nil) },
		},
		{
			name:  "Error",
			logFn: func() { logger.Error(ctx, "error msg", nil) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			tt.logFn()
		})
	}
}
