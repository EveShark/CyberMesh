package utils

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Audit severity levels
type AuditSeverity string

const (
	AuditDebug    AuditSeverity = "DEBUG"
	AuditInfo     AuditSeverity = "INFO"
	AuditWarn     AuditSeverity = "WARN"
	AuditError    AuditSeverity = "ERROR"
	AuditCritical AuditSeverity = "CRITICAL"
	AuditSecurity AuditSeverity = "SECURITY"
)

// Audit errors
var (
	ErrAuditLogClosed    = errors.New("audit: log is closed")
	ErrAuditVerifyFailed = errors.New("audit: verification failed")
	ErrAuditSequenceGap  = errors.New("audit: sequence number gap detected")
)

// AuditConfig configures the audit logger
type AuditConfig struct {
	// Output
	FilePath       string
	EnableRotation bool
	MaxSize        int // MB
	MaxBackups     int
	MaxAge         int // days
	Compress       bool

	// Security
	EnableSigning    bool
	SigningKey       []byte
	EnableEncryption bool
	EncryptionKey    []byte

	// Remote shipping
	EnableRemote    bool
	RemoteEndpoint  string
	RemoteBatchSize int

	// Performance
	BufferSize    int
	FlushInterval time.Duration

	// Static fields
	NodeID       string
	Component    string
	StaticFields map[string]interface{}
}

// DefaultAuditConfig returns secure defaults
func DefaultAuditConfig() *AuditConfig {
	return &AuditConfig{
		EnableRotation:  true,
		MaxSize:         100,
		MaxBackups:      30,
		MaxAge:          90,
		Compress:        true,
		EnableSigning:   true,
		BufferSize:      64 * 1024,
		FlushInterval:   5 * time.Second,
		RemoteBatchSize: 100,
	}
}

// AuditRecord represents a single audit log entry
type AuditRecord struct {
	Timestamp string                 `json:"ts"`
	Sequence  uint64                 `json:"seq"`
	Event     string                 `json:"event"`
	Severity  AuditSeverity          `json:"severity"`
	NodeID    string                 `json:"node_id,omitempty"`
	Component string                 `json:"component,omitempty"`
	UserID    string                 `json:"user_id,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Signature string                 `json:"sig,omitempty"`
	PrevHash  string                 `json:"prev_hash,omitempty"`
}

// AuditLogger provides tamper-evident audit logging
type AuditLogger struct {
	config  *AuditConfig
	writer  io.Writer
	closer  io.Closer
	encoder *json.Encoder

	// Integrity
	sequence   uint64
	lastHash   string
	signingKey []byte

	// Buffering
	buffer   *bufio.Writer
	bufferMu sync.Mutex

	// Remote shipping
	remoteBatch []AuditRecord
	remoteMu    sync.Mutex

	// Lifecycle
	closed     atomic.Bool
	flushTimer *time.Timer
	wg         sync.WaitGroup
	stopCh     chan struct{}
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(config *AuditConfig) (*AuditLogger, error) {
	if config == nil {
		config = DefaultAuditConfig()
	}

	if config.FilePath == "" {
		return nil, errors.New("file path is required")
	}

	// Create writer
	var writer io.Writer
	var closer io.Closer

	if config.EnableRotation {
		rotator := &lumberjack.Logger{
			Filename:   config.FilePath,
			MaxSize:    config.MaxSize,
			MaxBackups: config.MaxBackups,
			MaxAge:     config.MaxAge,
			Compress:   config.Compress,
		}
		writer = rotator
		closer = rotator
	} else {
		f, err := os.OpenFile(config.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed to open audit log: %w", err)
		}
		writer = f
		closer = f
	}

	// Create buffered writer
	buffer := bufio.NewWriterSize(writer, config.BufferSize)

	al := &AuditLogger{
		config:      config,
		writer:      writer,
		closer:      closer,
		buffer:      buffer,
		encoder:     json.NewEncoder(buffer),
		signingKey:  config.SigningKey,
		remoteBatch: make([]AuditRecord, 0, config.RemoteBatchSize),
		stopCh:      make(chan struct{}),
	}

	// Start periodic flush
	if config.FlushInterval > 0 {
		al.startPeriodicFlush()
	}

	// Start remote shipper
	if config.EnableRemote {
		al.startRemoteShipper()
	}

	return al, nil
}

// Log writes an audit record
func (al *AuditLogger) Log(event string, severity AuditSeverity, fields map[string]interface{}) error {
	if al.closed.Load() {
		return ErrAuditLogClosed
	}

	record := al.buildRecord(event, severity, fields)

	// Sign the record
	if al.config.EnableSigning && len(al.signingKey) > 0 {
		record.Signature = al.signRecord(record)
	}

	// Write to log
	if err := al.writeRecord(record); err != nil {
		return err
	}

	// Queue for remote shipping
	if al.config.EnableRemote {
		al.queueRemote(record)
	}

	return nil
}

// LogContext writes an audit record with context
func (al *AuditLogger) LogContext(ctx context.Context, event string, severity AuditSeverity, fields map[string]interface{}) error {
	if fields == nil {
		fields = make(map[string]interface{})
	}

	// Extract context values
	if requestID := ExtractRequestID(ctx); requestID != "" {
		fields["request_id"] = requestID
	}
	if val := ctx.Value(ContextKeyUserID); val != nil {
		fields["user_id"] = fmt.Sprintf("%v", val)
	}
	if val := ctx.Value(ContextKeySessionID); val != nil {
		fields["session_id"] = fmt.Sprintf("%v", val)
	}

	return al.Log(event, severity, fields)
}

// Convenience methods for different severity levels

func (al *AuditLogger) Debug(event string, fields map[string]interface{}) error {
	return al.Log(event, AuditDebug, fields)
}

func (al *AuditLogger) Info(event string, fields map[string]interface{}) error {
	return al.Log(event, AuditInfo, fields)
}

func (al *AuditLogger) Warn(event string, fields map[string]interface{}) error {
	return al.Log(event, AuditWarn, fields)
}

func (al *AuditLogger) Error(event string, fields map[string]interface{}) error {
	return al.Log(event, AuditError, fields)
}

func (al *AuditLogger) Critical(event string, fields map[string]interface{}) error {
	return al.Log(event, AuditCritical, fields)
}

func (al *AuditLogger) Security(event string, fields map[string]interface{}) error {
	return al.Log(event, AuditSecurity, fields)
}

// WithStatic returns a copy with additional static fields
func (al *AuditLogger) WithStatic(fields map[string]interface{}) *AuditLogger {
	newConfig := *al.config
	newConfig.StaticFields = make(map[string]interface{})

	for k, v := range al.config.StaticFields {
		newConfig.StaticFields[k] = v
	}
	for k, v := range fields {
		newConfig.StaticFields[k] = v
	}

	return &AuditLogger{
		config:     &newConfig,
		writer:     al.writer,
		closer:     al.closer,
		buffer:     al.buffer,
		encoder:    al.encoder,
		signingKey: al.signingKey,
		lastHash:   al.lastHash,
		sequence:   al.sequence,
		stopCh:     al.stopCh,
	}
}

// Flush flushes buffered records
func (al *AuditLogger) Flush() error {
	al.bufferMu.Lock()
	defer al.bufferMu.Unlock()

	if al.buffer != nil {
		return al.buffer.Flush()
	}

	return nil
}

// Close flushes and closes the audit logger
func (al *AuditLogger) Close() error {
	if !al.closed.CompareAndSwap(false, true) {
		return ErrAuditLogClosed
	}

	close(al.stopCh)
	al.wg.Wait()

	if err := al.Flush(); err != nil {
		return err
	}

	if al.closer != nil {
		return al.closer.Close()
	}

	return nil
}

// VerifyLog verifies the integrity of the audit log
func VerifyLog(path string, signingKey []byte) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var prevHash string
	var lastSeq uint64
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		var record AuditRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("line %d: invalid JSON: %w", lineNum, err)
		}

		// Check sequence
		if record.Sequence != lastSeq+1 {
			return fmt.Errorf("line %d: %w: expected %d, got %d",
				lineNum, ErrAuditSequenceGap, lastSeq+1, record.Sequence)
		}

		// Check previous hash
		if prevHash != "" && record.PrevHash != prevHash {
			return fmt.Errorf("line %d: hash chain broken", lineNum)
		}

		// Verify signature if present
		if record.Signature != "" && len(signingKey) > 0 {
			expectedSig := computeRecordSignature(record, signingKey)
			if record.Signature != expectedSig {
				return fmt.Errorf("line %d: %w", lineNum, ErrAuditVerifyFailed)
			}
		}

		prevHash = computeRecordHash(record)
		lastSeq = record.Sequence
	}

	return scanner.Err()
}

// Private methods

func (al *AuditLogger) buildRecord(event string, severity AuditSeverity, fields map[string]interface{}) AuditRecord {
	seq := atomic.AddUint64(&al.sequence, 1)

	record := AuditRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Sequence:  seq,
		Event:     event,
		Severity:  severity,
		NodeID:    al.config.NodeID,
		Component: al.config.Component,
		Fields:    make(map[string]interface{}),
		PrevHash:  al.lastHash,
	}

	// Add static fields
	for k, v := range al.config.StaticFields {
		record.Fields[k] = v
	}

	// Add dynamic fields
	for k, v := range fields {
		record.Fields[k] = v
	}

	return record
}

func (al *AuditLogger) signRecord(record AuditRecord) string {
	return computeRecordSignature(record, al.signingKey)
}

func (al *AuditLogger) writeRecord(record AuditRecord) error {
	al.bufferMu.Lock()
	defer al.bufferMu.Unlock()

	if err := al.encoder.Encode(record); err != nil {
		return fmt.Errorf("failed to encode record: %w", err)
	}

	// Update hash chain
	al.lastHash = computeRecordHash(record)

	return nil
}

func (al *AuditLogger) queueRemote(record AuditRecord) {
	al.remoteMu.Lock()
	defer al.remoteMu.Unlock()

	al.remoteBatch = append(al.remoteBatch, record)

	if len(al.remoteBatch) >= al.config.RemoteBatchSize {
		go al.shipRemoteBatch()
	}
}

func (al *AuditLogger) startPeriodicFlush() {
	al.wg.Add(1)
	go func() {
		defer al.wg.Done()
		ticker := time.NewTicker(al.config.FlushInterval)
		defer ticker.Stop()

		for {
			select {
			case <-al.stopCh:
				return
			case <-ticker.C:
				al.Flush()
			}
		}
	}()
}

func (al *AuditLogger) startRemoteShipper() {
	al.wg.Add(1)
	go func() {
		defer al.wg.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-al.stopCh:
				return
			case <-ticker.C:
				al.shipRemoteBatch()
			}
		}
	}()
}

func (al *AuditLogger) shipRemoteBatch() {
	al.remoteMu.Lock()
	batch := al.remoteBatch
	al.remoteBatch = make([]AuditRecord, 0, al.config.RemoteBatchSize)
	al.remoteMu.Unlock()

	if len(batch) == 0 {
		return
	}

	// TODO: Implement actual remote shipping (HTTP POST, etc.)
	// For now, this is a placeholder
	_ = batch
}

// Helper functions

func computeRecordHash(record AuditRecord) string {
	// Create canonical representation
	data := fmt.Sprintf("%s|%d|%s|%s", record.Timestamp, record.Sequence, record.Event, record.Severity)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func computeRecordSignature(record AuditRecord, key []byte) string {
	// Create canonical representation for signing
	data := fmt.Sprintf("%s|%d|%s|%s|%s",
		record.Timestamp, record.Sequence, record.Event, record.Severity, record.PrevHash)

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}
