package utils

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Context keys
type contextKey string

const (
	ContextKeyRequestID contextKey = "request_id"
	ContextKeyNodeID    contextKey = "node_id"
	ContextKeyComponent contextKey = "component"
	ContextKeyOperation contextKey = "operation"
	ContextKeyTraceID   contextKey = "trace_id"
	ContextKeySpanID    contextKey = "span_id"
	ContextKeyUserID    contextKey = "user_id"
	ContextKeySessionID contextKey = "session_id"
)

// Logger configuration constants
const (
	DefaultLogLevel       = "info"
	DefaultLogFileSize    = 100 // MB
	DefaultMaxBackups     = 10
	DefaultMaxAge         = 30 // days
	DefaultSampleRate     = 100
	HighLoadThreshold     = 5000 // messages per second
	SamplingCheckInterval = 1 * time.Second
)

// Sanitization patterns - compiled once at package init
var (

	// Sensitive field names
	sensitiveFieldNames = map[string]bool{
		"password":       true,
		"passwd":         true,
		"pwd":            true,
		"secret":         true,
		"token":          true,
		"key":            true,
		"apikey":         true,
		"api_key":        true,
		"private_key":    true,
		"encryption_key": true,
		"auth":           true,
		"authorization":  true,
		"bearer":         true,
		"jwt":            true,
		"credential":     true,
		"credentials":    true,
		"access_token":   true,
		"refresh_token":  true,
		"client_secret":  true,
		"aws_secret":     true,
		"ssh_key":        true,
		"certificate":    true,
		"private":        true,
	}
)

// LogLevel represents log severity
type LogLevel string

const (
	DebugLevel LogLevel = "debug"
	InfoLevel  LogLevel = "info"
	WarnLevel  LogLevel = "warn"
	ErrorLevel LogLevel = "error"
	FatalLevel LogLevel = "fatal"
)

// LogConfig holds logger configuration
type LogConfig struct {
	// Basic settings
	Level       string
	Development bool

	// Output settings
	OutputPath      string
	ErrorOutputPath string

	// Rotation settings
	EnableRotation bool
	MaxSize        int // megabytes
	MaxBackups     int
	MaxAge         int // days
	Compress       bool

	// Performance settings
	EnableSampling bool
	SampleRate     int
	BufferSize     int

	// Security settings
	EnableSanitization bool
	RedactionMode      RedactionMode

	// Context settings
	NodeID    string
	Component string

	// Custom fields
	DefaultFields map[string]interface{}
}

// RedactionMode defines how sensitive data is redacted
type RedactionMode int

const (
	RedactNone    RedactionMode = iota // No redaction (dangerous!)
	RedactPartial                      // Show partial data (e.g., email: u***@domain.com)
	RedactFull                         // Full redaction: [REDACTED]
	RedactHash                         // Replace with hash: [HASH:abc123]
)

// DefaultLogConfig returns production-ready defaults
func DefaultLogConfig() *LogConfig {
	return &LogConfig{
		Level:              getEnvOrDefault("LOG_LEVEL", DefaultLogLevel),
		Development:        getEnvOrDefault("ENVIRONMENT", "production") == "development",
		OutputPath:         getEnvOrDefault("LOG_FILE_PATH", ""),
		ErrorOutputPath:    "stderr",
		EnableRotation:     getEnvOrDefault("LOG_FILE_PATH", "") != "",
		MaxSize:            getEnvAsIntOrDefault("LOG_MAX_SIZE", DefaultLogFileSize),
		MaxBackups:         getEnvAsIntOrDefault("LOG_MAX_BACKUPS", DefaultMaxBackups),
		MaxAge:             getEnvAsIntOrDefault("LOG_MAX_AGE", DefaultMaxAge),
		Compress:           getEnvAsBoolOrDefault("LOG_COMPRESS", true),
		EnableSampling:     true,
		SampleRate:         DefaultSampleRate,
		EnableSanitization: true,
		RedactionMode:      RedactFull,
		NodeID:             getEnvOrDefault("NODE_ID", ""),
		Component:          getEnvOrDefault("SERVICE_NAME", "cybermesh"),
	}
}

// SecureLogConfig returns maximum security configuration
func SecureLogConfig() *LogConfig {
	config := DefaultLogConfig()
	config.EnableSanitization = true
	config.RedactionMode = RedactFull
	config.Development = false
	return config
}

// Logger provides structured, secure logging
type Logger struct {
	base        *zap.Logger
	config      *LogConfig
	atomicLevel zap.AtomicLevel

	// Performance tracking
	messageCount  uint64
	sampledCount  uint64
	lastReset     time.Time
	sampling      atomic.Bool
	sampleCounter uint64

	// Sanitization
	sanitizer *sanitizer

	// Lifecycle
	shutdownOnce sync.Once
	done         chan struct{}
	wg           sync.WaitGroup
}

// NewLogger creates a new logger instance
func NewLogger(config *LogConfig) (*Logger, error) {
	if config == nil {
		config = DefaultLogConfig()
	}

	if config.EnableSampling && config.SampleRate <= 0 {
		config.SampleRate = DefaultSampleRate
	}

	// Parse log level
	level, err := zapcore.ParseLevel(config.Level)
	if err != nil {
		level = zapcore.InfoLevel
	}

	atomicLevel := zap.NewAtomicLevelAt(level)

	// Create encoder config
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Create core
	core := buildCore(config, encoderConfig, atomicLevel)

	// Add sampling if enabled
	if config.EnableSampling {
		core = zapcore.NewSamplerWithOptions(
			core,
			time.Second,
			100, // first 100 messages per second
			10,  // thereafter 10 messages per second
		)
	}

	// Build logger
	zapLogger := zap.New(core,
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)

	// Add default fields
	if config.NodeID != "" {
		zapLogger = zapLogger.With(zap.String("node_id", config.NodeID))
	}
	if config.Component != "" {
		zapLogger = zapLogger.With(zap.String("component", config.Component))
	}

	// Add custom default fields
	if len(config.DefaultFields) > 0 {
		fields := make([]zap.Field, 0, len(config.DefaultFields))
		for k, v := range config.DefaultFields {
			fields = append(fields, zap.Any(k, v))
		}
		zapLogger = zapLogger.With(fields...)
	}

	logger := &Logger{
		base:        zapLogger,
		config:      config,
		atomicLevel: atomicLevel,
		lastReset:   time.Now(),
		done:        make(chan struct{}),
		sanitizer:   newSanitizer(config.RedactionMode),
	}

	// Start performance monitor
	logger.startPerformanceMonitor()

	return logger, nil
}

// WithContext creates a new logger with context fields
func (l *Logger) WithContext(ctx context.Context) *Logger {
	if ctx == nil {
		return l
	}

	fields := extractContextFields(ctx)
	if len(fields) == 0 {
		return l
	}

	// Create new logger with context fields
	return &Logger{
		base:        l.base.With(fields...),
		config:      l.config,
		atomicLevel: l.atomicLevel,
		sanitizer:   l.sanitizer,
		done:        l.done,
	}
}

// WithFields creates a logger with additional fields
func (l *Logger) WithFields(fields ...zap.Field) *Logger {
	sanitized := l.sanitizer.sanitizeFields(fields)
	return &Logger{
		base:        l.base.With(sanitized...),
		config:      l.config,
		atomicLevel: l.atomicLevel,
		sanitizer:   l.sanitizer,
		done:        l.done,
	}
}

// Log methods with context support

func (l *Logger) DebugContext(ctx context.Context, msg string, fields ...zap.Field) {
	atomic.AddUint64(&l.messageCount, 1)
	if l.shouldSample() {
		atomic.AddUint64(&l.sampledCount, 1)
		return
	}
	l.WithContext(ctx).base.Debug(msg, l.sanitizer.sanitizeFields(fields)...)
}

func (l *Logger) InfoContext(ctx context.Context, msg string, fields ...zap.Field) {
	atomic.AddUint64(&l.messageCount, 1)
	if l.shouldSample() {
		atomic.AddUint64(&l.sampledCount, 1)
		return
	}
	l.WithContext(ctx).base.Info(msg, l.sanitizer.sanitizeFields(fields)...)
}

func (l *Logger) WarnContext(ctx context.Context, msg string, fields ...zap.Field) {
	atomic.AddUint64(&l.messageCount, 1)
	if l.shouldSample() {
		atomic.AddUint64(&l.sampledCount, 1)
		return
	}
	l.WithContext(ctx).base.Warn(msg, l.sanitizer.sanitizeFields(fields)...)
}

func (l *Logger) ErrorContext(ctx context.Context, msg string, fields ...zap.Field) {
	atomic.AddUint64(&l.messageCount, 1)
	// Never sample errors
	l.WithContext(ctx).base.Error(msg, l.sanitizer.sanitizeFields(fields)...)
}

func (l *Logger) FatalContext(ctx context.Context, msg string, fields ...zap.Field) {
	atomic.AddUint64(&l.messageCount, 1)
	l.WithContext(ctx).base.Fatal(msg, l.sanitizer.sanitizeFields(fields)...)
}

// Log methods without context

func (l *Logger) Debug(msg string, fields ...zap.Field) {
	atomic.AddUint64(&l.messageCount, 1)
	if l.shouldSample() {
		atomic.AddUint64(&l.sampledCount, 1)
		return
	}
	l.base.Debug(msg, l.sanitizer.sanitizeFields(fields)...)
}

func (l *Logger) Info(msg string, fields ...zap.Field) {
	atomic.AddUint64(&l.messageCount, 1)
	if l.shouldSample() {
		atomic.AddUint64(&l.sampledCount, 1)
		return
	}
	l.base.Info(msg, l.sanitizer.sanitizeFields(fields)...)
}

func (l *Logger) Warn(msg string, fields ...zap.Field) {
	atomic.AddUint64(&l.messageCount, 1)
	if l.shouldSample() {
		atomic.AddUint64(&l.sampledCount, 1)
		return
	}
	l.base.Warn(msg, l.sanitizer.sanitizeFields(fields)...)
}

func (l *Logger) Error(msg string, fields ...zap.Field) {
	atomic.AddUint64(&l.messageCount, 1)
	l.base.Error(msg, l.sanitizer.sanitizeFields(fields)...)
}

func (l *Logger) Fatal(msg string, fields ...zap.Field) {
	atomic.AddUint64(&l.messageCount, 1)
	l.base.Fatal(msg, l.sanitizer.sanitizeFields(fields)...)
}

// Configuration methods

func (l *Logger) SetLevel(level string) error {
	newLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		return fmt.Errorf("invalid log level: %w", err)
	}
	l.atomicLevel.SetLevel(newLevel)
	l.Info("log level changed", zap.String("new_level", level))
	return nil
}

func (l *Logger) GetLevel() string {
	return l.atomicLevel.Level().String()
}

// Metrics

type LogMetrics struct {
	MessageCount uint64    `json:"message_count"`
	SampledCount uint64    `json:"sampled_count"`
	SamplingOn   bool      `json:"sampling_enabled"`
	LogLevel     string    `json:"log_level"`
	LastReset    time.Time `json:"last_reset"`
}

func (l *Logger) GetMetrics() LogMetrics {
	return LogMetrics{
		MessageCount: atomic.LoadUint64(&l.messageCount),
		SampledCount: atomic.LoadUint64(&l.sampledCount),
		SamplingOn:   l.sampling.Load(),
		LogLevel:     l.GetLevel(),
		LastReset:    l.lastReset,
	}
}

// Lifecycle

func (l *Logger) Shutdown() error {
	var err error
	l.shutdownOnce.Do(func() {
		close(l.done)
		l.wg.Wait()
		err = l.base.Sync()
	})
	return err
}

// Private methods

func buildCore(config *LogConfig, encoderConfig zapcore.EncoderConfig, level zap.AtomicLevel) zapcore.Core {
	var encoder zapcore.Encoder
	if config.Development {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	if config.EnableRotation && config.OutputPath != "" {
		writer := &lumberjack.Logger{
			Filename:   config.OutputPath,
			MaxSize:    config.MaxSize,
			MaxBackups: config.MaxBackups,
			MaxAge:     config.MaxAge,
			Compress:   config.Compress,
		}
		return zapcore.NewCore(encoder, zapcore.AddSync(writer), level)
	}

	return zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level)
}

func extractContextFields(ctx context.Context) []zap.Field {
	fields := make([]zap.Field, 0, 8)

	if val := ctx.Value(ContextKeyRequestID); val != nil {
		fields = append(fields, zap.String("request_id", fmt.Sprintf("%v", val)))
	}
	if val := ctx.Value(ContextKeyNodeID); val != nil {
		fields = append(fields, zap.String("node_id", fmt.Sprintf("%v", val)))
	}
	if val := ctx.Value(ContextKeyComponent); val != nil {
		fields = append(fields, zap.String("component", fmt.Sprintf("%v", val)))
	}
	if val := ctx.Value(ContextKeyOperation); val != nil {
		fields = append(fields, zap.String("operation", fmt.Sprintf("%v", val)))
	}
	if val := ctx.Value(ContextKeyTraceID); val != nil {
		fields = append(fields, zap.String("trace_id", fmt.Sprintf("%v", val)))
	}
	if val := ctx.Value(ContextKeySpanID); val != nil {
		fields = append(fields, zap.String("span_id", fmt.Sprintf("%v", val)))
	}
	if val := ctx.Value(ContextKeyUserID); val != nil {
		fields = append(fields, zap.String("user_id", fmt.Sprintf("%v", val)))
	}
	if val := ctx.Value(ContextKeySessionID); val != nil {
		fields = append(fields, zap.String("session_id", fmt.Sprintf("%v", val)))
	}

	return fields
}

func (l *Logger) startPerformanceMonitor() {
	if !l.config.EnableSampling {
		return
	}

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		ticker := time.NewTicker(SamplingCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-l.done:
				return
			case <-ticker.C:
				count := atomic.SwapUint64(&l.messageCount, 0)
				rate := float64(count) / SamplingCheckInterval.Seconds()

				if rate > HighLoadThreshold {
					if !l.sampling.Load() {
						l.sampling.Store(true)
						l.Warn("high log rate detected, enabling sampling",
							zap.Float64("rate", rate),
							zap.Float64("threshold", HighLoadThreshold))
					}
				} else {
					if l.sampling.Load() {
						l.sampling.Store(false)
						l.Info("log rate normalized, disabling sampling",
							zap.Float64("rate", rate))
					}
				}
			}
		}
	}()
}

func (l *Logger) shouldSample() bool {
	if !l.config.EnableSampling {
		return false
	}
	if !l.sampling.Load() {
		return false
	}
	sampleRate := l.config.SampleRate
	if sampleRate <= 0 {
		return false
	}
	counter := atomic.AddUint64(&l.sampleCounter, 1)
	return counter%uint64(sampleRate) != 0
}

// sanitizer handles sensitive data redaction
type sanitizer struct {
	mode RedactionMode
}

func newSanitizer(mode RedactionMode) *sanitizer {
	return &sanitizer{mode: mode}
}

func (s *sanitizer) sanitizeFields(fields []zap.Field) []zap.Field {
	if len(fields) == 0 {
		return fields
	}

	result := make([]zap.Field, 0, len(fields))
	for _, field := range fields {
		if s.isSensitiveField(field.Key) {
			result = append(result, s.redactField(field))
		} else if field.Type == zapcore.StringType {
			result = append(result, zap.String(field.Key, s.sanitizeString(field.String)))
		} else {
			result = append(result, field)
		}
	}
	return result
}

func (s *sanitizer) isSensitiveField(key string) bool {
	lower := strings.ToLower(key)
	return sensitiveFieldNames[lower] || strings.Contains(lower, "secret") ||
		strings.Contains(lower, "password") || strings.Contains(lower, "token")
}

func (s *sanitizer) redactField(field zap.Field) zap.Field {
	switch s.mode {
	case RedactNone:
		return field
	case RedactPartial:
		return zap.String(field.Key, "[PARTIAL]")
	case RedactHash:
		return zap.String(field.Key, "[HASH:"+hashString(field.String)+"]")
	default:
		return zap.String(field.Key, "[REDACTED]")
	}
}

func (s *sanitizer) sanitizeString(value string) string {
	// Simple string containment checks instead of complex regex
	// This prevents ReDoS attacks while still catching most secrets

	result := value

	// Check for common secret indicators
	lower := strings.ToLower(result)
	secretIndicators := []string{
		"bearer ",
		"password=",
		"pwd=",
		"secret=",
		"token=",
		"apikey=",
		"api_key=",
		"-----begin",
		"akia", // AWS keys
		"asia", // AWS keys
	}

	containsSecret := false
	for _, indicator := range secretIndicators {
		if strings.Contains(lower, indicator) {
			containsSecret = true
			break
		}
	}

	if containsSecret {
		return "[REDACTED]"
	}

	return result
}

func hashString(s string) string {
	// Simple hash for redaction mode
	var hash uint32
	for i := 0; i < len(s); i++ {
		hash = hash*31 + uint32(s[i])
	}
	return fmt.Sprintf("%08x", hash)
}

// Context helper functions

func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, ContextKeyRequestID, requestID)
}

func ContextWithNodeID(ctx context.Context, nodeID string) context.Context {
	return context.WithValue(ctx, ContextKeyNodeID, nodeID)
}

func ContextWithComponent(ctx context.Context, component string) context.Context {
	return context.WithValue(ctx, ContextKeyComponent, component)
}

func ContextWithOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, ContextKeyOperation, operation)
}

func ContextWithTracing(ctx context.Context, traceID, spanID string) context.Context {
	ctx = context.WithValue(ctx, ContextKeyTraceID, traceID)
	return context.WithValue(ctx, ContextKeySpanID, spanID)
}

func ContextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ContextKeyUserID, userID)
}

func ContextWithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, ContextKeySessionID, sessionID)
}

func ExtractRequestID(ctx context.Context) string {
	if id := ctx.Value(ContextKeyRequestID); id != nil {
		return fmt.Sprintf("%v", id)
	}
	return ""
}

// Zap field helpers

func ZapString(key, val string) zap.Field                 { return zap.String(key, val) }
func ZapInt(key string, val int) zap.Field                { return zap.Int(key, val) }
func ZapInt64(key string, val int64) zap.Field            { return zap.Int64(key, val) }
func ZapUint64(key string, val uint64) zap.Field          { return zap.Uint64(key, val) }
func ZapInt32(key string, val int32) zap.Field            { return zap.Int32(key, val) }
func ZapUint32(key string, val uint32) zap.Field          { return zap.Uint32(key, val) }
func ZapFloat64(key string, val float64) zap.Field        { return zap.Float64(key, val) }
func ZapBool(key string, val bool) zap.Field              { return zap.Bool(key, val) }
func ZapError(err error) zap.Field                        { return zap.Error(err) }
func ZapDuration(key string, val time.Duration) zap.Field { return zap.Duration(key, val) }
func ZapTime(key string, val time.Time) zap.Field         { return zap.Time(key, val) }
func ZapAny(key string, val interface{}) zap.Field        { return zap.Any(key, val) }
func ZapStringArray(key string, val []string) zap.Field   { return zap.Strings(key, val) }

// Global logger management

var (
	globalLogger     *Logger
	globalLoggerOnce sync.Once
	globalLoggerMu   sync.RWMutex
)

func GetLogger() *Logger {
	globalLoggerOnce.Do(func() {
		logger, err := NewLogger(DefaultLogConfig())
		if err != nil {
			zapLogger, _ := zap.NewProduction()
			globalLogger = &Logger{
				base:      zapLogger,
				config:    DefaultLogConfig(),
				done:      make(chan struct{}),
				sanitizer: newSanitizer(RedactFull),
			}
		} else {
			globalLogger = logger
		}
	})
	return globalLogger
}

func SetGlobalLogger(logger *Logger) {
	globalLoggerMu.Lock()
	defer globalLoggerMu.Unlock()
	if globalLogger != nil {
		globalLogger.Shutdown()
	}
	globalLogger = logger
}

func CreateTestLogger() *Logger {
	config := &LogConfig{
		Level:              "debug",
		Development:        true,
		EnableSampling:     false,
		EnableSanitization: false,
		RedactionMode:      RedactNone,
	}
	logger, _ := NewLogger(config)
	return logger
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvAsBoolOrDefault(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

func ZapInts(key string, val []int) zap.Field {
	return zap.Any(key, val)
}
