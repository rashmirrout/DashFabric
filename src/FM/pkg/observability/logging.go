package observability

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger provides structured logging interface
type Logger interface {
	Info(msg string, keyvals ...interface{})
	Error(msg string, keyvals ...interface{})
	Debug(msg string, keyvals ...interface{})
	WithContext(ctx context.Context) Logger
}

// StructuredLogger implements Logger with zap backend
type StructuredLogger struct {
	logger  *zap.Logger
	sugar   *zap.SugaredLogger
	traceID string
}

// NewStructuredLogger creates a new structured logger with specified level
func NewStructuredLogger(level string) (*StructuredLogger, error) {
	var config zap.Config

	switch level {
	case "debug":
		config = zap.NewDevelopmentConfig()
		config.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case "info":
		config = zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	case "warn":
		config = zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zapcore.WarnLevel)
	case "error":
		config = zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zapcore.ErrorLevel)
	default:
		config = zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}

	// Use JSON format for structured output
	config.Encoding = "json"
	config.OutputPaths = []string{"stdout"}
	config.ErrorOutputPaths = []string{"stderr"}

	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build zap logger: %w", err)
	}

	return &StructuredLogger{
		logger:  logger,
		sugar:   logger.Sugar(),
		traceID: "",
	}, nil
}

// Info logs at INFO level with key-value pairs
func (sl *StructuredLogger) Info(msg string, keyvals ...interface{}) {
	fields := sl.toFields(keyvals...)
	sl.logger.Info(msg, fields...)
}

// Error logs at ERROR level with key-value pairs
func (sl *StructuredLogger) Error(msg string, keyvals ...interface{}) {
	fields := sl.toFields(keyvals...)
	sl.logger.Error(msg, fields...)
}

// Debug logs at DEBUG level with key-value pairs
func (sl *StructuredLogger) Debug(msg string, keyvals ...interface{}) {
	fields := sl.toFields(keyvals...)
	sl.logger.Debug(msg, fields...)
}

// WithContext extracts trace ID from context and returns logger with it
func (sl *StructuredLogger) WithContext(ctx context.Context) Logger {
	traceID := TraceIDFromContext(ctx)
	return &StructuredLogger{
		logger:  sl.logger,
		sugar:   sl.sugar,
		traceID: traceID,
	}
}

// toFields converts key-value pairs to zap fields, prepending trace ID if present
func (sl *StructuredLogger) toFields(keyvals ...interface{}) []zap.Field {
	fields := []zap.Field{}

	// Add trace ID if present
	if sl.traceID != "" {
		fields = append(fields, zap.String("trace_id", sl.traceID))
	}

	// Convert alternating key-value pairs to zap fields
	for i := 0; i < len(keyvals)-1; i += 2 {
		key := fmt.Sprintf("%v", keyvals[i])
		val := keyvals[i+1]
		fields = append(fields, zap.Any(key, val))
	}

	return fields
}

// Sync flushes any buffered log entries
func (sl *StructuredLogger) Sync() error {
	return sl.logger.Sync()
}
