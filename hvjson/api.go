package hvjson

import "strconv"

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

func MarshalWithConfig(v interface{}, config *Config) ([]byte, error) {
	encoder := newEncoder(config)
	defer encoder.Release()
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
