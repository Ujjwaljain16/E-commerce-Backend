// Package logger provides structured JSON logging for microservices.
// It supports different log levels and automatic context extraction.
package logger

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"
)

// LogLevel represents the severity of a log entry
type LogLevel string

// Log level constants for structured logging.
const (
	DEBUG LogLevel = "DEBUG"
	INFO  LogLevel = "INFO"
	WARN  LogLevel = "WARN"
	ERROR LogLevel = "ERROR"
)

// Logger is a structured logger that outputs JSON format
type Logger struct {
	service string
	logger  *log.Logger
}

// LogEntry represents a single log entry in JSON format
type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     LogLevel               `json:"level"`
	Service   string                 `json:"service"`
	TraceID   string                 `json:"trace_id,omitempty"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// New creates a new Logger for the specified service
func New(service string) *Logger {
	return &Logger{
		service: service,
		logger:  log.New(os.Stdout, "", 0),
	}
}

// Info logs an informational message
func (l *Logger) Info(ctx context.Context, message string, data map[string]interface{}) {
	l.log(ctx, INFO, message, data)
}

// Error logs an error message
func (l *Logger) Error(ctx context.Context, message string, data map[string]interface{}) {
	l.log(ctx, ERROR, message, data)
}

// Debug logs a debug message
func (l *Logger) Debug(ctx context.Context, message string, data map[string]interface{}) {
	l.log(ctx, DEBUG, message, data)
}

// Warn logs a warning message
func (l *Logger) Warn(ctx context.Context, message string, data map[string]interface{}) {
	l.log(ctx, WARN, message, data)
}

// log is the internal method that formats and outputs log entries
func (l *Logger) log(ctx context.Context, level LogLevel, message string, data map[string]interface{}) {
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Service:   l.service,
		TraceID:   getTraceID(ctx),
		Message:   message,
		Data:      data,
	}

	jsonLog, _ := json.Marshal(entry)
	l.logger.Println(string(jsonLog))
}

// getTraceID extracts trace ID from context for distributed tracing
func getTraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if traceID := ctx.Value("trace_id"); traceID != nil {
		if id, ok := traceID.(string); ok {
			return id
		}
	}
	return ""
}
