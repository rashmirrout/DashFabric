package datamanagement

import (
	"context"

	obs "github.com/dashfabric/fm/pkg/observability"
)

// defaultLogger is the package-level logger, initialized with a no-op fallback
var defaultLogger obs.Logger = &noOpLogger{}

// SetLogger sets the package-level logger for DM module
func SetLogger(logger obs.Logger) {
	if logger != nil {
		defaultLogger = logger
	}
}

// noOpLogger is a fallback logger that does nothing
type noOpLogger struct{}

func (n *noOpLogger) Info(msg string, keyvals ...interface{})              {}
func (n *noOpLogger) Error(msg string, keyvals ...interface{})            {}
func (n *noOpLogger) Debug(msg string, keyvals ...interface{})            {}
func (n *noOpLogger) WithContext(ctx context.Context) obs.Logger { return n }
