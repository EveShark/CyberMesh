package utils

import (
	"context"
)

// AuditLoggerAdapter adapts utils.AuditLogger to consensus AuditLogger interface
// Consensus expects LogContext(ctx, event, severity string, fields)
// But utils.AuditLogger has LogContext(ctx, event, severity AuditSeverity, fields)
type AuditLoggerAdapter struct {
	logger *AuditLogger
}

// NewAuditLoggerAdapter creates an adapter for consensus engine
func NewAuditLoggerAdapter(logger *AuditLogger) *AuditLoggerAdapter {
	if logger == nil {
		return nil
	}
	return &AuditLoggerAdapter{logger: logger}
}

func (a *AuditLoggerAdapter) Info(event string, fields map[string]interface{}) error {
	if a == nil || a.logger == nil {
		return nil
	}
	return a.logger.Info(event, fields)
}

func (a *AuditLoggerAdapter) Warn(event string, fields map[string]interface{}) error {
	if a == nil || a.logger == nil {
		return nil
	}
	return a.logger.Warn(event, fields)
}

func (a *AuditLoggerAdapter) Error(event string, fields map[string]interface{}) error {
	if a == nil || a.logger == nil {
		return nil
	}
	return a.logger.Error(event, fields)
}

func (a *AuditLoggerAdapter) Security(event string, fields map[string]interface{}) error {
	if a == nil || a.logger == nil {
		return nil
	}
	return a.logger.Security(event, fields)
}

// LogContext adapts string severity to AuditSeverity type
func (a *AuditLoggerAdapter) LogContext(ctx context.Context, event string, severity string, fields map[string]interface{}) error {
	if a == nil || a.logger == nil {
		return nil
	}
	return a.logger.LogContext(ctx, event, AuditSeverity(severity), fields)
}
