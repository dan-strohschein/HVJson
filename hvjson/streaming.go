package hvjson

import (
	"bufio"
	"bytes"
	"io"
	"sync"

	simd "github.com/dan-strohschein/syndrdb-simd"
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
// followed by a newline character (single write for fewer syscalls and no newline alloc).
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

	// Single write: build JSON + newline in se.buf to avoid second Write and []byte{'\n'} alloc
	se.buf = append(se.buf[:0], data...)
	se.buf = append(se.buf, '\n')
	if _, err := se.w.Write(se.buf); err != nil {
		se.err = err
		return err
	}
	return nil
}

// StreamDecoder reads and decodes JSON values from an input stream.
type StreamDecoder struct {
	r               io.Reader
	config          *Config
	buf             []byte
	pos             int
	filled          int
	err             error
	useNumber       bool
	useInt64        bool
	valueStartOffset int // start of current value in buf; reset to 0 when fill() compacts
}

// NewDecoder returns a new decoder that reads from r.
// If r is not already a *bufio.Reader, it is wrapped to reduce read syscalls.
func (c *Config) NewDecoder(r io.Reader) *StreamDecoder {
	if _, ok := r.(*bufio.Reader); !ok {
		r = bufio.NewReader(r)
	}
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

// streamDecoderPool reuses Decoder instances for stream decoding to reduce allocations.
var streamDecoderPool = sync.Pool{
	New: func() interface{} { return &Decoder{} },
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

	// Mark the start of this value (fill() may compact and set valueStartOffset to 0)
	sd.valueStartOffset = sd.pos

	// Find the end of this JSON value
	end, err := sd.skipValue()
	if err != nil {
		return err
	}

	// Extract the JSON value. If fill() compacted during skipValue, valueStartOffset was set to 0
	// and the value is at the start of the buffer; otherwise use the offset we recorded.
	start := sd.valueStartOffset
	if start > end {
		start = 0 // compact happened mid-skip; value is at buf[0:end]
	}
	jsonData := sd.buf[start:end]

	// Unmarshal using pooled Decoder to avoid allocation per value
	decoder := streamDecoderPool.Get().(*Decoder)
	decoder.Reset(jsonData)
	decoder.config = sd.config
	err = decoder.Decode(v)
	decoder.Reset(nil) // clear slice reference before returning to pool
	streamDecoderPool.Put(decoder)
	if err != nil {
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

// simdWhitespaceMinLen is the minimum remaining buffer length to use SIMD skip.
const simdWhitespaceMinLen = 32

// skipWhitespace skips whitespace characters in the stream.
// Uses SIMD when enough data is available for better throughput on whitespace-heavy input.
func (sd *StreamDecoder) skipWhitespace() error {
	for {
		if sd.pos >= sd.filled {
			if err := sd.fill(); err != nil {
				return err
			}
		}
		remaining := sd.filled - sd.pos
		if remaining >= simdWhitespaceMinLen {
			newPos := simd.SkipWhitespace(sd.buf[:sd.filled], sd.pos)
			sd.pos = newPos
			if sd.pos < sd.filled {
				return nil
			}
			continue
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

// Predefined literal bytes to avoid string allocation in skipLiteral.
var (
	literalTrue  = []byte("true")
	literalFalse = []byte("false")
	literalNull  = []byte("null")
)

// skipLiteral skips a JSON literal (true, false, null) using byte comparison to avoid allocations.
func (sd *StreamDecoder) skipLiteral(literals ...string) (int, error) {
	for _, lit := range literals {
		litLen := len(lit)
		if sd.pos+litLen > sd.filled {
			if err := sd.fill(); err != nil {
				return 0, err
			}
		}

		if sd.pos+litLen <= sd.filled {
			slice := sd.buf[sd.pos : sd.pos+litLen]
			var match bool
			switch lit {
			case "true":
				match = bytes.Equal(slice, literalTrue)
			case "false":
				match = bytes.Equal(slice, literalFalse)
			case "null":
				match = bytes.Equal(slice, literalNull)
			default:
				match = string(slice) == lit // fallback for any other literal
			}
			if match {
				sd.pos += litLen
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

// streamDecoderBufPoolMinCap is the minimum buffer capacity to use the buffer pool in fill().
const streamDecoderBufPoolMinCap = 64 * 1024

// fill reads more data from the reader into the buffer.
// When we have unprocessed data (mid-value), we grow and copy from valueStartOffset
// so the full value stays contiguous; otherwise we compact or reset.
// Large buffers (>= 64KB) are obtained from the buffer pool to reduce GC pressure.
func (sd *StreamDecoder) fill() error {
	if sd.err != nil {
		return sd.err
	}

	if sd.pos < sd.filled {
		// We're in the middle of a value; keep the full value contiguous.
		valueLen := sd.filled - sd.valueStartOffset
		newCap := valueLen * 2
		if newCap < len(sd.buf)*2 {
			newCap = len(sd.buf) * 2
		}
		var newBuf []byte
		if newCap >= streamDecoderBufPoolMinCap {
			newBuf = getBuffer(newCap)
			newBuf = newBuf[:valueLen]
			if cap(sd.buf) >= streamDecoderBufPoolMinCap {
				putBuffer(sd.buf)
			}
		} else {
			newBuf = make([]byte, newCap)
		}
		copy(newBuf, sd.buf[sd.valueStartOffset:sd.filled])
		sd.buf = newBuf
		sd.pos = sd.pos - sd.valueStartOffset
		sd.filled = valueLen
		sd.valueStartOffset = 0
	} else {
		sd.pos = 0
		sd.filled = 0
		sd.valueStartOffset = 0
	}

	// Grow buffer if we still don't have room to read
	if sd.filled >= len(sd.buf)-1 {
		newCap := len(sd.buf) * 2
		var newBuf []byte
		if newCap >= streamDecoderBufPoolMinCap {
			newBuf = getBuffer(newCap)
			newBuf = newBuf[:sd.filled]
			copy(newBuf, sd.buf[:sd.filled])
			if cap(sd.buf) >= streamDecoderBufPoolMinCap {
				putBuffer(sd.buf)
			}
		} else {
			newBuf = make([]byte, newCap)
			copy(newBuf, sd.buf[:sd.filled])
		}
		sd.buf = newBuf
	}

	// Read more data
	n, err := sd.r.Read(sd.buf[sd.filled:])
	if n > 0 {
		sd.filled += n
	}

	if err != nil {
		if err == io.EOF && sd.filled > sd.pos {
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
