package utils

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/fxamacker/cbor/v2"
)

// Serialization limits
const (
	DefaultSerializeMaxSize = 10 << 20  // 10MB
	MaxSafeSize             = 100 << 20 // 100MB absolute maximum
)

// Serialization errors
var (
	ErrSizeLimitExceeded = errors.New("serialize: size limit exceeded")
	ErrInvalidData       = errors.New("serialize: invalid data")
	ErrEncodingFailed    = errors.New("serialize: encoding failed")
	ErrDecodingFailed    = errors.New("serialize: decoding failed")
)

var (
	cborOnce   sync.Once
	cborEncOpt cbor.EncOptions
	cborDecOpt cbor.DecOptions
	cborEnc    cbor.EncMode
	cborDec    cbor.DecMode
	initErr    error
)

// SerializerConfig holds serialization configuration
type SerializerConfig struct {
	MaxSize           int64
	EnableCanonical   bool
	EnableStreaming   bool
	EnableCompression bool
}

// DefaultSerializerConfig returns secure defaults
func DefaultSerializerConfig() *SerializerConfig {
	return &SerializerConfig{
		MaxSize:         DefaultSerializeMaxSize,
		EnableCanonical: true,
		EnableStreaming: false,
	}
}

// Serializer provides safe serialization with size limits
type Serializer struct {
	config *SerializerConfig
}

// NewSerializer creates a new serializer
func NewSerializer(config *SerializerConfig) *Serializer {
	if config == nil {
		config = DefaultSerializerConfig()
	}

	if config.MaxSize <= 0 {
		config.MaxSize = DefaultSerializeMaxSize
	}
	if config.MaxSize > MaxSafeSize {
		config.MaxSize = MaxSafeSize
	}

	return &Serializer{config: config}
}

// initCBOR initializes CBOR modes once
func initCBOR() {
	cborOnce.Do(func() {
		cborEncOpt = cbor.EncOptions{
			Time:          cbor.TimeRFC3339Nano,
			Sort:          cbor.SortCoreDeterministic,
			IndefLength:   cbor.IndefLengthForbidden,
			ShortestFloat: cbor.ShortestFloat16,
			NaNConvert:    cbor.NaNConvert7e00,
			InfConvert:    cbor.InfConvertFloat16,
		}
		cborDecOpt = cbor.DecOptions{
			IndefLength:      cbor.IndefLengthForbidden,
			MaxArrayElements: 100000,
			MaxMapPairs:      100000,
			MaxNestedLevels:  32,
		}

		var err error
		cborEnc, err = cborEncOpt.EncMode()
		if err != nil {
			initErr = fmt.Errorf("failed to create CBOR encoder: %w", err)
			return
		}
		cborDec, err = cborDecOpt.DecMode()
		if err != nil {
			initErr = fmt.Errorf("failed to create CBOR decoder: %w", err)
			return
		}
	})
}

// JSON Serialization

// JSONMarshal encodes v to JSON with size limit
func (s *Serializer) JSONMarshal(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	limitWriter := &limitedWriter{w: &buf, limit: s.config.MaxSize}

	encoder := json.NewEncoder(limitWriter)
	if err := encoder.Encode(v); err != nil {
		if errors.Is(err, ErrSizeLimitExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrEncodingFailed, err)
	}

	return buf.Bytes(), nil
}

// JSONMarshalCanonical encodes v to canonical JSON (sorted keys)
func (s *Serializer) JSONMarshalCanonical(v interface{}) ([]byte, error) {
	// First marshal to get the structure
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncodingFailed, err)
	}

	// Check size
	if int64(len(data)) > s.config.MaxSize {
		return nil, ErrSizeLimitExceeded
	}

	// Re-parse and encode with sorted keys
	var canonical interface{}
	if err := json.Unmarshal(data, &canonical); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncodingFailed, err)
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "")
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(canonical); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncodingFailed, err)
	}

	return bytes.TrimSpace(buf.Bytes()), nil
}

// JSONUnmarshal decodes JSON data into v with size limit
func (s *Serializer) JSONUnmarshal(data []byte, v interface{}) error {
	if int64(len(data)) > s.config.MaxSize {
		return ErrSizeLimitExceeded
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields() // Security: reject unknown fields

	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrDecodingFailed, err)
	}

	// Check for trailing data
	if decoder.More() {
		return fmt.Errorf("%w: trailing data after JSON", ErrInvalidData)
	}

	return nil
}

// JSONUnmarshalStream decodes JSON from a reader with size limit
func (s *Serializer) JSONUnmarshalStream(r io.Reader, v interface{}) error {
	limitReader := io.LimitReader(r, s.config.MaxSize+1)

	decoder := json.NewDecoder(limitReader)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(v); err != nil {
		if err == io.EOF {
			return fmt.Errorf("%w: unexpected EOF", ErrInvalidData)
		}
		return fmt.Errorf("%w: %v", ErrDecodingFailed, err)
	}

	// Check if we hit the size limit
	var buf [1]byte
	if n, _ := limitReader.Read(buf[:]); n > 0 {
		return ErrSizeLimitExceeded
	}

	return nil
}

// CBOR Serialization

// CBORMarshal encodes v to canonical CBOR with size limit
func (s *Serializer) CBORMarshal(v interface{}) ([]byte, error) {
	initCBOR()
	if initErr != nil {
		return nil, initErr
	}

	var buf bytes.Buffer
	limitWriter := &limitedWriter{w: &buf, limit: s.config.MaxSize}

	encoder := cborEnc.NewEncoder(limitWriter)
	if err := encoder.Encode(v); err != nil {
		if errors.Is(err, ErrSizeLimitExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrEncodingFailed, err)
	}

	return buf.Bytes(), nil
}

// CBORUnmarshal decodes CBOR data into v with size limit
func (s *Serializer) CBORUnmarshal(data []byte, v interface{}) error {
	initCBOR()
	if initErr != nil {
		return initErr
	}

	if int64(len(data)) > s.config.MaxSize {
		return ErrSizeLimitExceeded
	}

	decoder := cborDec.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrDecodingFailed, err)
	}

	return nil
}

// CBORUnmarshalStream decodes CBOR from a reader with size limit
func (s *Serializer) CBORUnmarshalStream(r io.Reader, v interface{}) error {
	initCBOR()
	if initErr != nil {
		return initErr
	}

	limitReader := io.LimitReader(r, s.config.MaxSize+1)
	decoder := cborDec.NewDecoder(limitReader)

	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrDecodingFailed, err)
	}

	// Check if we hit the size limit
	var buf [1]byte
	if n, _ := limitReader.Read(buf[:]); n > 0 {
		return ErrSizeLimitExceeded
	}

	return nil
}

// limitedWriter enforces a size limit on writes
type limitedWriter struct {
	w       io.Writer
	written int64
	limit   int64
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.written+int64(len(p)) > lw.limit {
		return 0, ErrSizeLimitExceeded
	}

	n, err := lw.w.Write(p)
	lw.written += int64(n)
	return n, err
}

// Global convenience functions (legacy compatibility)

var defaultSerializer = NewSerializer(DefaultSerializerConfig())

// JSONMarshal encodes v to JSON
func JSONMarshal(v interface{}) ([]byte, error) {
	return defaultSerializer.JSONMarshal(v)
}

// JSONUnmarshal decodes JSON data into v
func JSONUnmarshal(data []byte, v interface{}) error {
	return defaultSerializer.JSONUnmarshal(data, v)
}

// JSONMarshalCanonical encodes v to canonical JSON
func JSONMarshalCanonical(v interface{}) ([]byte, error) {
	return defaultSerializer.JSONMarshalCanonical(v)
}

// CBORMarshal encodes v to canonical CBOR
func CBORMarshal(v interface{}) ([]byte, error) {
	return defaultSerializer.CBORMarshal(v)
}

// CBORUnmarshal decodes CBOR data into v
func CBORUnmarshal(data []byte, v interface{}) error {
	return defaultSerializer.CBORUnmarshal(data, v)
}

// SafeJSONMarshal safely marshals with error recovery
func SafeJSONMarshal(v interface{}) ([]byte, error) {
	defer func() {
		if r := recover(); r != nil {
			// Log panic but don't crash
		}
	}()
	return JSONMarshal(v)
}

// SafeJSONUnmarshal safely unmarshals with error recovery
func SafeJSONUnmarshal(data []byte, v interface{}) error {
	defer func() {
		if r := recover(); r != nil {
			// Log panic but don't crash
		}
	}()
	return JSONUnmarshal(data, v)
}
