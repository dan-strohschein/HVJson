package hvjson

import (
	"io"
)

// StreamEncoder writes JSON values to an output stream.
type StreamEncoder struct {
	w      io.Writer
	config *Config
	err    error
	buf    []byte
}

// NewEncoder returns a new encoder that writes to w.
func (c *Config) NewEncoder(w io.Writer) *StreamEncoder {
	return &StreamEncoder{
		w:      w,
		config: c,
		buf:    make([]byte, 0, 256),
	}
}

// NewEncoder returns a new encoder that writes to w using default config.
func NewEncoder(w io.Writer) *StreamEncoder {
	return ConfigDefault().NewEncoder(w)
}

// Encode writes the JSON encoding of v to the stream,
// followed by a newline character.
func (se *StreamEncoder) Encode(v interface{}) error {
	if se.err != nil {
		return se.err
	}

	// Reuse encoder from pool
	enc := newEncoder(se.config)
	defer enc.Release()

	data, err := enc.Encode(v)
	if err != nil {
		se.err = err
		return err
	}

	// Write to stream
	if _, err := se.w.Write(data); err != nil {
		se.err = err
		return err
	}

	// Write newline
	if _, err := se.w.Write([]byte{'\n'}); err != nil {
		se.err = err
		return err
	}

	return nil
}

// StreamDecoder reads and decodes JSON values from an input stream.
type StreamDecoder struct {
	r         io.Reader
	config    *Config
	buf       []byte
	pos       int
	filled    int
	err       error
	useNumber bool
	useInt64  bool
}

// NewDecoder returns a new decoder that reads from r.
func (c *Config) NewDecoder(r io.Reader) *StreamDecoder {
	return &StreamDecoder{
		r:      r,
		config: c,
		buf:    make([]byte, 4096),
		pos:    0,
		filled: 0,
	}
}

// NewDecoder returns a new decoder that reads from r using default config.
func NewDecoder(r io.Reader) *StreamDecoder {
	return ConfigDefault().NewDecoder(r)
}

// Decode reads the next JSON-encoded value from its input and stores it
// in the value pointed to by v.
func (sd *StreamDecoder) Decode(v interface{}) error {
	if sd.err != nil {
		return sd.err
	}

	// Skip whitespace to find next value
	if err := sd.skipWhitespace(); err != nil {
		return err
	}

	// Mark the start position
	start := sd.pos

	// Find the end of this JSON value
	end, err := sd.skipValue()
	if err != nil {
		return err
	}

	// Extract the JSON value
	jsonData := sd.buf[start:end]

	// Unmarshal it
	decoder := newDecoder(jsonData, sd.config)
	if err := decoder.Decode(v); err != nil {
		sd.err = err
		return err
	}

	// Move position forward
	sd.pos = end

	return nil
}

// UseNumber causes the Decoder to unmarshal a number into an interface{} as a
// Number instead of as a float64.
func (sd *StreamDecoder) UseNumber() {
	sd.useNumber = true
	if sd.config != nil {
		sd.config.UseNumber = true
	}
}

// UseInt64 causes the Decoder to unmarshal a number into an interface{} as an
// int64 instead of as a float64.
func (sd *StreamDecoder) UseInt64() {
	sd.useInt64 = true
	if sd.config != nil {
		sd.config.UseInt64 = true
	}
}

// skipWhitespace skips whitespace characters in the stream
func (sd *StreamDecoder) skipWhitespace() error {
	for {
		// Need more data?
		if sd.pos >= sd.filled {
			if err := sd.fill(); err != nil {
				return err
			}
		}

		c := sd.buf[sd.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			sd.pos++
			continue
		}
		break
	}
	return nil
}

// skipValue skips one complete JSON value and returns its end position
func (sd *StreamDecoder) skipValue() (int, error) {
	if sd.pos >= sd.filled {
		if err := sd.fill(); err != nil {
			return 0, err
		}
	}

	start := sd.pos
	c := sd.buf[sd.pos]

	switch c {
	case '"':
		return sd.skipString()
	case '{':
		return sd.skipObject()
	case '[':
		return sd.skipArray()
	case 't', 'f':
		return sd.skipLiteral("true", "false")
	case 'n':
		return sd.skipLiteral("null")
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return sd.skipNumber()
	default:
		return 0, &SyntaxError{
			Pos:     start,
			Code:    ErrorInvalidJSON,
			Message: "invalid character",
		}
	}
}

// skipString skips a JSON string
func (sd *StreamDecoder) skipString() (int, error) {
	sd.pos++ // skip opening quote

	for {
		if sd.pos >= sd.filled {
			if err := sd.fill(); err != nil {
				return 0, err
			}
		}

		c := sd.buf[sd.pos]
		if c == '"' {
			sd.pos++
			return sd.pos, nil
		}
		if c == '\\' {
			sd.pos++
			if sd.pos >= sd.filled {
				if err := sd.fill(); err != nil {
					return 0, err
				}
			}
			sd.pos++ // skip escaped char
		} else {
			sd.pos++
		}
	}
}

// skipObject skips a JSON object
func (sd *StreamDecoder) skipObject() (int, error) {
	sd.pos++ // skip '{'

	if err := sd.skipWhitespace(); err != nil {
		return 0, err
	}

	// Empty object?
	if sd.buf[sd.pos] == '}' {
		sd.pos++
		return sd.pos, nil
	}

	for {
		// Skip key (must be string)
		if _, err := sd.skipString(); err != nil {
			return 0, err
		}

		if err := sd.skipWhitespace(); err != nil {
			return 0, err
		}

		// Skip ':'
		if sd.buf[sd.pos] != ':' {
			return 0, &SyntaxError{Pos: sd.pos, Code: ErrorInvalidJSON, Message: "expected ':'"}
		}
		sd.pos++

		if err := sd.skipWhitespace(); err != nil {
			return 0, err
		}

		// Skip value
		if _, err := sd.skipValue(); err != nil {
			return 0, err
		}

		if err := sd.skipWhitespace(); err != nil {
			return 0, err
		}

		c := sd.buf[sd.pos]
		if c == '}' {
			sd.pos++
			return sd.pos, nil
		}
		if c != ',' {
			return 0, &SyntaxError{Pos: sd.pos, Code: ErrorInvalidJSON, Message: "expected ',' or '}'"}
		}
		sd.pos++ // skip ','

		if err := sd.skipWhitespace(); err != nil {
			return 0, err
		}
	}
}

// skipArray skips a JSON array
func (sd *StreamDecoder) skipArray() (int, error) {
	sd.pos++ // skip '['

	if err := sd.skipWhitespace(); err != nil {
		return 0, err
	}

	// Empty array?
	if sd.buf[sd.pos] == ']' {
		sd.pos++
		return sd.pos, nil
	}

	for {
		// Skip value
		if _, err := sd.skipValue(); err != nil {
			return 0, err
		}

		if err := sd.skipWhitespace(); err != nil {
			return 0, err
		}

		c := sd.buf[sd.pos]
		if c == ']' {
			sd.pos++
			return sd.pos, nil
		}
		if c != ',' {
			return 0, &SyntaxError{Pos: sd.pos, Code: ErrorInvalidJSON, Message: "expected ',' or ']'"}
		}
		sd.pos++ // skip ','

		if err := sd.skipWhitespace(); err != nil {
			return 0, err
		}
	}
}

// skipLiteral skips a JSON literal (true, false, null)
func (sd *StreamDecoder) skipLiteral(literals ...string) (int, error) {
	for _, lit := range literals {
		if sd.pos+len(lit) > sd.filled {
			if err := sd.fill(); err != nil {
				return 0, err
			}
		}

		if sd.pos+len(lit) <= sd.filled {
			if string(sd.buf[sd.pos:sd.pos+len(lit)]) == lit {
				sd.pos += len(lit)
				return sd.pos, nil
			}
		}
	}

	return 0, &SyntaxError{Pos: sd.pos, Code: ErrorInvalidJSON, Message: "invalid literal"}
}

// skipNumber skips a JSON number
func (sd *StreamDecoder) skipNumber() (int, error) {
	start := sd.pos

	// Optional minus
	if sd.buf[sd.pos] == '-' {
		sd.pos++
		if sd.pos >= sd.filled {
			if err := sd.fill(); err != nil {
				return 0, err
			}
		}
	}

	// Integer part
	if sd.buf[sd.pos] == '0' {
		sd.pos++
	} else if sd.buf[sd.pos] >= '1' && sd.buf[sd.pos] <= '9' {
		sd.pos++
		for sd.pos < sd.filled && sd.buf[sd.pos] >= '0' && sd.buf[sd.pos] <= '9' {
			sd.pos++
		}
		if sd.pos >= sd.filled {
			if err := sd.fill(); err != nil {
				return 0, err
			}
		}
	} else {
		return 0, &SyntaxError{Pos: start, Code: ErrorInvalidNumber, Message: "invalid number"}
	}

	// Fractional part
	if sd.pos < sd.filled && sd.buf[sd.pos] == '.' {
		sd.pos++
		if sd.pos >= sd.filled {
			if err := sd.fill(); err != nil {
				return 0, err
			}
		}
		if sd.buf[sd.pos] < '0' || sd.buf[sd.pos] > '9' {
			return 0, &SyntaxError{Pos: sd.pos, Code: ErrorInvalidNumber, Message: "invalid number"}
		}
		for sd.pos < sd.filled && sd.buf[sd.pos] >= '0' && sd.buf[sd.pos] <= '9' {
			sd.pos++
		}
		if sd.pos >= sd.filled {
			if err := sd.fill(); err != nil {
				return 0, err
			}
		}
	}

	// Exponent part
	if sd.pos < sd.filled && (sd.buf[sd.pos] == 'e' || sd.buf[sd.pos] == 'E') {
		sd.pos++
		if sd.pos >= sd.filled {
			if err := sd.fill(); err != nil {
				return 0, err
			}
		}
		if sd.buf[sd.pos] == '+' || sd.buf[sd.pos] == '-' {
			sd.pos++
			if sd.pos >= sd.filled {
				if err := sd.fill(); err != nil {
					return 0, err
				}
			}
		}
		if sd.buf[sd.pos] < '0' || sd.buf[sd.pos] > '9' {
			return 0, &SyntaxError{Pos: sd.pos, Code: ErrorInvalidNumber, Message: "invalid number"}
		}
		for sd.pos < sd.filled && sd.buf[sd.pos] >= '0' && sd.buf[sd.pos] <= '9' {
			sd.pos++
		}
	}

	return sd.pos, nil
}

// fill reads more data from the reader into the buffer
func (sd *StreamDecoder) fill() error {
	if sd.err != nil {
		return sd.err
	}

	// If we have unprocessed data, move it to the beginning
	if sd.pos < sd.filled {
		copy(sd.buf, sd.buf[sd.pos:sd.filled])
		sd.filled -= sd.pos
		sd.pos = 0
	} else {
		sd.pos = 0
		sd.filled = 0
	}

	// Grow buffer if needed
	if sd.filled >= len(sd.buf)-1 {
		newBuf := make([]byte, len(sd.buf)*2)
		copy(newBuf, sd.buf[:sd.filled])
		sd.buf = newBuf
	}

	// Read more data
	n, err := sd.r.Read(sd.buf[sd.filled:])
	if n > 0 {
		sd.filled += n
	}

	if err != nil {
		if err == io.EOF && sd.filled > sd.pos {
			// We have data to process, don't return EOF yet
			return nil
		}
		sd.err = err
		return err
	}

	return nil
}

// More reads another JSON value into the buffer without decoding.
// It reports whether there is another value available.
func (sd *StreamDecoder) More() bool {
	if sd.err != nil {
		return false
	}

	if err := sd.skipWhitespace(); err != nil {
		return false
	}

	return sd.pos < sd.filled
}

// Buffered returns a reader of the data remaining in the Decoder's buffer.
func (sd *StreamDecoder) Buffered() io.Reader {
	return &bufferedReader{sd: sd}
}

type bufferedReader struct {
	sd *StreamDecoder
}

func (br *bufferedReader) Read(p []byte) (int, error) {
	if br.sd.pos >= br.sd.filled {
		return 0, io.EOF
	}
	n := copy(p, br.sd.buf[br.sd.pos:br.sd.filled])
	br.sd.pos += n
	return n, nil
}

// DisallowUnknownFields causes the Decoder to return an error when the destination
// is a struct and the input contains object keys which do not match any
// non-ignored, exported fields in the destination.
func (sd *StreamDecoder) DisallowUnknownFields() {
	// Create a copy of config if needed to avoid modifying shared config
	if sd.config == ConfigDefault() || sd.config == ConfigFastest() {
		newConfig := *sd.config
		sd.config = &newConfig
	}
	sd.config.DisallowUnknownFields = true
}

// SetEscapeHTML specifies whether problematic HTML characters should be escaped
// inside JSON quoted strings. When true (the default), <, >, and & are escaped
// as \\u003c, \\u003e, and \\u0026.
func (se *StreamEncoder) SetEscapeHTML(on bool) {
	// Create a copy of config if needed to avoid modifying shared config
	if se.config == ConfigDefault() || se.config == ConfigFastest() {
		newConfig := *se.config
		se.config = &newConfig
	}
	se.config.EscapeHTML = on
}

// SetIndent instructs the encoder to format each subsequent encoded value as if
// indented by the package-level function Indent(dst, src, prefix, indent).
func (se *StreamEncoder) SetIndent(prefix, indent string) {
	// Create a copy of config if needed to avoid modifying shared config
	if se.config == ConfigDefault() || se.config == ConfigFastest() {
		newConfig := *se.config
		se.config = &newConfig
	}
	se.config.IndentPrefix = prefix
	se.config.IndentString = indent
	se.config.DoIndent = true
}
