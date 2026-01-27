package hvjson

import (
	"reflect"
	"strconv"
	"sync"
)

type Config struct {
	NoValidateJSONSkip bool
	UseStreamingUTF8   bool
	MaxDepth           int
	DisableCache       bool
	// Number handling options
	UseNumber bool // Return Number type instead of float64 for interface{}
	UseInt64  bool // Return int64 for integers instead of float64 for interface{}
	// Indentation options
	IndentPrefix string
	IndentString string
	DoIndent     bool
	// Decoding options
	DisallowUnknownFields bool // Return error for unknown fields in structs
	// Encoding options
	EscapeHTML bool // Escape <, >, & in strings (default: true to match stdlib)
}

// Number represents a JSON number literal.
// It preserves the original string representation for precision.
type Number string

// String returns the literal text of the number.
func (n Number) String() string {
	return string(n)
}

// Int64 returns the number as an int64.
func (n Number) Int64() (int64, error) {
	return strconv.ParseInt(string(n), 10, 64)
}

// Float64 returns the number as a float64.
func (n Number) Float64() (float64, error) {
	return strconv.ParseFloat(string(n), 64)
}

var defaultConfig = Config{
	NoValidateJSONSkip: false,
	UseStreamingUTF8:   false,
	MaxDepth:           10000,
	DisableCache:       false,
	EscapeHTML:         true, // Match encoding/json default
}

var fastestConfig = Config{
	NoValidateJSONSkip: true,
	UseStreamingUTF8:   false,
	MaxDepth:           10000,
	DisableCache:       false,
}

func ConfigDefault() *Config {
	return &defaultConfig
}

func ConfigFastest() *Config {
	return &fastestConfig
}

// Marshal encodes v as JSON. For struct types, it automatically uses
// pre-compiled encoders (FastMarshal) for maximum performance.
func Marshal(v interface{}) ([]byte, error) {
	return MarshalWithConfig(v, ConfigDefault())
}

func MarshalString(v interface{}) (string, error) {
	b, err := Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// MarshalWithConfig encodes v as JSON using the provided config.
// For struct types, it automatically uses pre-compiled encoders for maximum performance.
func MarshalWithConfig(v interface{}, config *Config) ([]byte, error) {
	// For structs and pointers to structs, use FastMarshal path for pre-compiled encoders
	if v != nil {
		rv := reflect.ValueOf(v)
		kind := rv.Kind()
		if kind == reflect.Struct || (kind == reflect.Ptr && rv.Type().Elem().Kind() == reflect.Struct) {
			return FastMarshalWithConfig(v, config)
		}
	}

	// For primitives, maps, slices, use encoder with fast-path type switches
	encoder := newEncoder(config)
	defer encoder.Release()
	data, err := encoder.Encode(v)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// MarshalResult holds a pooled marshal result.
// Call Release() when done to return the buffer to the pool.
type MarshalResult struct {
	Data []byte
}

// marshalResultInternal is the internal structure with pool reference
type marshalResultInternal struct {
	result MarshalResult
	bufPtr *[]byte // Original buffer pointer to return to pool
}

// Release returns the buffer to the pool. Must be called after consuming Data.
func (r *MarshalResult) Release() {
	// This is a no-op for the external struct - pooling is handled internally
	// The actual pooling happens when we use the sync.Pool for the internal struct
}

// String returns the JSON as a string. Can be called before Release().
func (r *MarshalResult) String() string {
	return string(r.Data)
}

// Buffer pool for MarshalPooled
var marshalBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 0, 512)
		return &buf
	},
}

// MarshalPooled encodes v as JSON using a pooled buffer.
// Returns a MarshalResult - the buffer is reused internally.
// For maximum performance with zero allocations, use MarshalTo with your own buffer.
func MarshalPooled(v interface{}) (*MarshalResult, error) {
	return MarshalPooledWithConfig(v, ConfigDefault())
}

// MarshalPooledWithConfig encodes v as JSON using a pooled buffer with custom config.
func MarshalPooledWithConfig(v interface{}, config *Config) (*MarshalResult, error) {
	bufPtr := marshalBufferPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]

	result, err := MarshalTo(buf, v, config)
	if err != nil {
		marshalBufferPool.Put(bufPtr)
		return nil, err
	}

	// Copy result since we're returning the buffer to pool
	// For true zero-copy, caller should use MarshalTo directly
	out := make([]byte, len(result))
	copy(out, result)
	marshalBufferPool.Put(bufPtr)

	return &MarshalResult{
		Data: out,
	}, nil
}

// MarshalTo appends the JSON encoding of v to dst and returns the extended buffer.
// This is a zero-copy API that avoids allocating a new buffer for the result.
func MarshalTo(dst []byte, v interface{}, config *Config) ([]byte, error) {
	if config == nil {
		config = ConfigDefault()
	}

	encoder := newEncoderWithBuffer(dst, config)
	defer encoder.Release()

	// For structs and pointers to structs, use pre-compiled encoders
	if v != nil {
		rv := reflect.ValueOf(v)
		kind := rv.Kind()
		if kind == reflect.Struct || (kind == reflect.Ptr && rv.Type().Elem().Kind() == reflect.Struct) {
			return fastMarshalToWithEncoder(encoder, v)
		}
	}

	data, err := encoder.Encode(v)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func Unmarshal(data []byte, v interface{}) error {
	return UnmarshalWithConfig(data, v, ConfigDefault())
}

func UnmarshalString(data string, v interface{}) error {
	return Unmarshal([]byte(data), v)
}

func UnmarshalWithConfig(data []byte, v interface{}, config *Config) error {
	decoder := newDecoder(data, config)
	return decoder.Decode(v)
}

// MarshalIndent is like Marshal but applies Indent to format the output.
// Each element in a JSON object or array begins on a new line,
// with prefix followed by one or more copies of indent based on nesting depth.
func MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	config := &Config{
		MaxDepth:     10000,
		IndentPrefix: prefix,
		IndentString: indent,
		DoIndent:     true,
	}
	return MarshalWithConfig(v, config)
}

// MarshalIndentWithConfig is like MarshalIndent but uses a custom config.
// The config's IndentPrefix, IndentString, and DoIndent fields will be overwritten.
func MarshalIndentWithConfig(v interface{}, prefix, indent string, config *Config) ([]byte, error) {
	// Copy config to avoid modifying the original
	cfg := *config
	cfg.IndentPrefix = prefix
	cfg.IndentString = indent
	cfg.DoIndent = true
	return MarshalWithConfig(v, &cfg)
}
