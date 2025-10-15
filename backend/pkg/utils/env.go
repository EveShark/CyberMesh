package utils

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Configuration errors
var (
	ErrConfigValueInvalid    = errors.New("config: invalid value")
	ErrConfigValueRequired   = errors.New("config: required value missing")
	ErrConfigValueOutOfRange = errors.New("config: value out of range")
	ErrConfigSecretExposed   = errors.New("config: secret may be exposed")
)

// ConfigValidator validates configuration values
type ConfigValidator interface {
	Validate(key, value string) error
}

// ConfigSource defines where configuration is loaded from
type ConfigSource interface {
	Get(key string) (string, bool)
	Set(key, value string) error
	Delete(key string) error
	List() map[string]string
}

// ConfigChangeHandler is called when config values change
type ConfigChangeHandler func(key, oldValue, newValue string)

// ConfigManager provides secure configuration management
type ConfigManager struct {
	source    ConfigSource
	validator ConfigValidator
	logger    *Logger

	// Security
	sensitiveKeys map[string]bool
	redactMode    RedactionMode

	// Change notifications
	handlers   map[string][]ConfigChangeHandler
	handlersMu sync.RWMutex

	// Metrics
	accessCount map[string]uint64
	accessMu    sync.RWMutex
}

// ConfigManagerConfig holds configuration for the config manager
type ConfigManagerConfig struct {
	Source        ConfigSource
	Validator     ConfigValidator
	Logger        *Logger
	SensitiveKeys []string
	RedactMode    RedactionMode
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(config *ConfigManagerConfig) (*ConfigManager, error) {
	if config == nil {
		return nil, errors.New("config is required")
	}

	if config.Source == nil {
		config.Source = &envSource{}
	}

	cm := &ConfigManager{
		source:        config.Source,
		validator:     config.Validator,
		logger:        config.Logger,
		sensitiveKeys: make(map[string]bool),
		redactMode:    config.RedactMode,
		handlers:      make(map[string][]ConfigChangeHandler),
		accessCount:   make(map[string]uint64),
	}

	// Mark sensitive keys
	for _, key := range config.SensitiveKeys {
		cm.sensitiveKeys[strings.ToLower(key)] = true
	}

	return cm, nil
}

// GetString returns a string configuration value
func (cm *ConfigManager) GetString(key, defaultValue string) string {
	value, exists := cm.source.Get(key)
	cm.recordAccess(key)

	if !exists || strings.TrimSpace(value) == "" {
		cm.logDefault(key, defaultValue)
		return defaultValue
	}

	if cm.validator != nil {
		if err := cm.validator.Validate(key, value); err != nil {
			cm.logInvalid(key, err)
			return defaultValue
		}
	}

	return strings.TrimSpace(value)
}

// GetStringRequired returns a required string value or error
func (cm *ConfigManager) GetStringRequired(key string) (string, error) {
	value, exists := cm.source.Get(key)
	cm.recordAccess(key)

	if !exists || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: %s", ErrConfigValueRequired, key)
	}

	value = strings.TrimSpace(value)

	if cm.validator != nil {
		if err := cm.validator.Validate(key, value); err != nil {
			return "", fmt.Errorf("%w: %s: %v", ErrConfigValueInvalid, key, err)
		}
	}

	return value, nil
}

// GetSecret returns a secret value with security validation
func (cm *ConfigManager) GetSecret(key string) (string, error) {
	value, exists := cm.source.Get(key)
	cm.recordAccess(key)

	if !exists || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: %s", ErrConfigValueRequired, key)
	}

	value = strings.TrimSpace(value)

	// Validate secret isn't obviously exposed
	if err := cm.validateSecret(key, value); err != nil {
		if cm.logger != nil {
			cm.logger.Warn("secret validation warning",
				ZapString("key", key),
				ZapError(err))
		}
	}

	cm.markSensitive(key)

	return value, nil
}

// GetInt returns an integer configuration value
func (cm *ConfigManager) GetInt(key string, defaultValue int) int {
	value, exists := cm.source.Get(key)
	cm.recordAccess(key)

	if !exists || strings.TrimSpace(value) == "" {
		cm.logDefault(key, defaultValue)
		return defaultValue
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		cm.logInvalid(key, err)
		return defaultValue
	}

	return parsed
}

// GetIntRange returns an integer within specified bounds
func (cm *ConfigManager) GetIntRange(key string, defaultValue, min, max int) int {
	value := cm.GetInt(key, defaultValue)

	if value < min || value > max {
		if cm.logger != nil {
			cm.logger.Warn("config value out of range, using default",
				ZapString("key", key),
				ZapInt("value", value),
				ZapInt("min", min),
				ZapInt("max", max),
				ZapInt("default", defaultValue))
		}
		return defaultValue
	}

	return value
}

// GetUint64 returns an unsigned 64-bit integer
func (cm *ConfigManager) GetUint64(key string, defaultValue uint64) uint64 {
	value, exists := cm.source.Get(key)
	cm.recordAccess(key)

	if !exists || strings.TrimSpace(value) == "" {
		cm.logDefault(key, defaultValue)
		return defaultValue
	}

	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		cm.logInvalid(key, err)
		return defaultValue
	}

	return parsed
}

// GetFloat64 returns a float64 configuration value
func (cm *ConfigManager) GetFloat64(key string, defaultValue float64) float64 {
	value, exists := cm.source.Get(key)
	cm.recordAccess(key)

	if !exists || strings.TrimSpace(value) == "" {
		cm.logDefault(key, defaultValue)
		return defaultValue
	}

	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		cm.logInvalid(key, err)
		return defaultValue
	}

	return parsed
}

// GetBool returns a boolean configuration value
func (cm *ConfigManager) GetBool(key string, defaultValue bool) bool {
	value, exists := cm.source.Get(key)
	cm.recordAccess(key)

	if !exists || strings.TrimSpace(value) == "" {
		cm.logDefault(key, defaultValue)
		return defaultValue
	}

	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "1", "true", "t", "yes", "y", "on", "enabled":
		return true
	case "0", "false", "f", "no", "n", "off", "disabled":
		return false
	default:
		cm.logInvalid(key, fmt.Errorf("invalid boolean: %s", value))
		return defaultValue
	}
}

// GetDuration returns a duration configuration value
func (cm *ConfigManager) GetDuration(key string, defaultValue time.Duration) time.Duration {
	value, exists := cm.source.Get(key)
	cm.recordAccess(key)

	if !exists || strings.TrimSpace(value) == "" {
		cm.logDefault(key, defaultValue)
		return defaultValue
	}

	v := strings.TrimSpace(value)

	// Try standard duration format
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}

	// Try integer seconds
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Duration(n) * time.Second
	}

	cm.logInvalid(key, fmt.Errorf("invalid duration: %s", value))
	return defaultValue
}

// GetStringSlice returns a comma-separated list as a slice
func (cm *ConfigManager) GetStringSlice(key string, defaultValue []string) []string {
	value, exists := cm.source.Get(key)
	cm.recordAccess(key)

	if !exists || strings.TrimSpace(value) == "" {
		cm.logDefault(key, defaultValue)
		return defaultValue
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	if len(result) == 0 {
		return defaultValue
	}

	return result
}

// Set updates a configuration value
func (cm *ConfigManager) Set(key, value string) error {
	oldValue, _ := cm.source.Get(key)

	if cm.validator != nil {
		if err := cm.validator.Validate(key, value); err != nil {
			return fmt.Errorf("%w: %v", ErrConfigValueInvalid, err)
		}
	}

	if err := cm.source.Set(key, value); err != nil {
		return err
	}

	cm.notifyHandlers(key, oldValue, value)

	if cm.logger != nil && !cm.isSensitive(key) {
		cm.logger.Info("config value updated",
			ZapString("key", key),
			ZapString("old", oldValue),
			ZapString("new", value))
	}

	return nil
}

// OnChange registers a handler for configuration changes
func (cm *ConfigManager) OnChange(key string, handler ConfigChangeHandler) {
	cm.handlersMu.Lock()
	defer cm.handlersMu.Unlock()

	cm.handlers[key] = append(cm.handlers[key], handler)
}

// GetMetrics returns access metrics
func (cm *ConfigManager) GetMetrics() map[string]uint64 {
	cm.accessMu.RLock()
	defer cm.accessMu.RUnlock()

	result := make(map[string]uint64, len(cm.accessCount))
	for k, v := range cm.accessCount {
		result[k] = v
	}

	return result
}

// Private methods

func (cm *ConfigManager) recordAccess(key string) {
	cm.accessMu.Lock()
	cm.accessCount[key]++
	cm.accessMu.Unlock()
}

func (cm *ConfigManager) logDefault(key string, defaultValue interface{}) {
	if cm.logger != nil {
		cm.logger.Debug("using default config value",
			ZapString("key", key),
			ZapAny("default", defaultValue))
	}
}

func (cm *ConfigManager) logInvalid(key string, err error) {
	if cm.logger != nil {
		cm.logger.Warn("invalid config value, using default",
			ZapString("key", key),
			ZapError(err))
	}
}

func (cm *ConfigManager) isSensitive(key string) bool {
	return cm.sensitiveKeys[strings.ToLower(key)]
}

func (cm *ConfigManager) markSensitive(key string) {
	cm.sensitiveKeys[strings.ToLower(key)] = true
}

func (cm *ConfigManager) validateSecret(key, value string) error {
	// Check minimum length
	if len(value) < 16 {
		return fmt.Errorf("secret too short (min 16 chars)")
	}

	// Warn if it looks like a placeholder
	lower := strings.ToLower(value)
	placeholders := []string{"changeme", "password", "secret", "xxx", "todo", "fixme"}
	for _, p := range placeholders {
		if strings.Contains(lower, p) {
			return fmt.Errorf("%w: contains placeholder text", ErrConfigSecretExposed)
		}
	}

	return nil
}

func (cm *ConfigManager) notifyHandlers(key, oldValue, newValue string) {
	cm.handlersMu.RLock()
	handlers := cm.handlers[key]
	cm.handlersMu.RUnlock()

	for _, handler := range handlers {
		go func(h ConfigChangeHandler) {
			defer func() {
				if r := recover(); r != nil {
					if cm.logger != nil {
						cm.logger.Error("config change handler panic",
							ZapString("key", key),
							ZapAny("panic", r))
					}
				}
			}()
			h(key, oldValue, newValue)
		}(handler)
	}
}

var sensitiveEnvKeyFragments = []string{
	"secret",
	"password",
	"token",
	"apikey",
	"api_key",
	"private",
	"credential",
	"certificate",
	"cert",
	"key",
	"dsn",
}

func isSensitiveEnvKey(key string) bool {
	lower := strings.ToLower(key)
	for _, frag := range sensitiveEnvKeyFragments {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

const redactedEnvValue = "[REDACTED]"

// envSource implements ConfigSource using environment variables
type envSource struct{}

func (e *envSource) Get(key string) (string, bool) {
	value := os.Getenv(key)
	return value, value != ""
}

func (e *envSource) Set(key, value string) error {
	return os.Setenv(key, value)
}

func (e *envSource) Delete(key string) error {
	return os.Unsetenv(key)
}

func (e *envSource) List() map[string]string {
	result := make(map[string]string)
	for _, env := range os.Environ() {
		if idx := strings.IndexByte(env, '='); idx > 0 {
			key := env[:idx]
			value := env[idx+1:]
			if isSensitiveEnvKey(key) {
				result[key] = redactedEnvValue
			} else {
				result[key] = value
			}
		}
	}
	return result
}

// Legacy compatibility functions

func GetEnvString(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func GetEnvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func GetEnvBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return def
	}
}

func GetEnvDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Duration(n) * time.Second
	}
	return def
}

// SecureCompare performs constant-time string comparison
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
