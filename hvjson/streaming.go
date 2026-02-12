package hvjson

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"sync"

	simd "github.com/dan-strohschein/syndrdb-simd"
)

// ErrInvalidWriteState is returned when a write method is called in an invalid
// sequence (e.g. WriteObjectEnd with empty stack, or Encode during a write session).
var ErrInvalidWriteState = errors.New("hvjson: invalid write state")

// Write stack frame bits: bit 0 = kind (0 array, 1 object), bit 1 = first (0 not first, 1 first).
const (
	writeFrameArrayNotFirst = 0
	writeFrameObjectNotFirst = 1
	writeFrameArrayFirst    = 2
	writeFrameObjectFirst   = 3
)

const maxWriteDepth = 64

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

// DefaultIncrementalFlushThreshold is the default buffer size (64 KiB) before flushing when using IncrementalStreamEncoder.
// Tuned for speed; use a smaller value for lower memory.
const DefaultIncrementalFlushThreshold = 64 * 1024

// IncrementalStreamEncoder writes JSON values to an output stream with incremental flushing.
// It flushes to the underlying io.Writer when the buffer reaches flushThreshold bytes, keeping memory bounded
// while preserving SIMD-accelerated encoding. Use this for large streams (e.g. many or large documents)
// instead of StreamEncoder, which buffers each value fully before writing.
//
// The underlying Writer should not return partial writes (wrap with bufio.Writer if needed).
// Same SIMD and encoding behavior as Marshal; only the write path is incremental.
//
// IncrementalStreamEncoder also supports a streaming write API: WriteObjectStart, WriteArrayStart,
// WriteObjectField, WriteString, WriteInt64, etc., so callers can build one JSON value incrementally
// with the same SIMD encoding and incremental flush. Do not call Encode while a write session is active
// (write stack non-empty).
type IncrementalStreamEncoder struct {
	w              io.Writer
	config         *Config
	err            error
	buf            []byte
	flushThreshold int
	enc            *Encoder // dedicated encoder, not from pool

	// Streaming write API state (zero allocation: fixed-size stack).
	writeStack      [maxWriteDepth]uint8
	writeDepth      int
	writeIndentLevel int
	valueComplete   bool // true after a full value has been written (for NDJSON newline before next value)
}

// NewIncrementalEncoder returns a new incremental streaming encoder that writes to w.
// flushThreshold is the buffer size in bytes before flushing; if <= 0, DefaultIncrementalFlushThreshold is used.
func (c *Config) NewIncrementalEncoder(w io.Writer, flushThreshold int) *IncrementalStreamEncoder {
	if flushThreshold <= 0 {
		flushThreshold = DefaultIncrementalFlushThreshold
	}
	buf := make([]byte, 0, flushThreshold)
	return &IncrementalStreamEncoder{
		w:              w,
		config:         c,
		buf:            buf,
		flushThreshold: flushThreshold,
		enc:            &Encoder{},
	}
}

// NewIncrementalEncoder returns a new incremental streaming encoder with default config.
func NewIncrementalEncoder(w io.Writer, flushThreshold int) *IncrementalStreamEncoder {
	return ConfigDefault().NewIncrementalEncoder(w, flushThreshold)
}

// Encode encodes v as JSON and writes it to the stream with incremental flushes, then writes a newline.
// Encode must not be called while a write session is active (write stack non-empty); use WriteObjectEnd/WriteArrayEnd first.
func (ise *IncrementalStreamEncoder) Encode(v interface{}) error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth != 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	enc := ise.enc
	ise.buf = ise.buf[:0]
	enc.buf = ise.buf
	enc.config = ise.config
	enc.streamOut = ise.w
	enc.streamThreshold = ise.flushThreshold
	enc.depth = 0
	enc.indentLevel = 0
	enc.ptrSeenCount = 0
	enc.streamErr = nil
	if enc.ptrSeen != nil {
		for k := range enc.ptrSeen {
			delete(enc.ptrSeen, k)
		}
	}

	_, err := enc.Encode(v)
	if err != nil {
		ise.err = err
		enc.streamOut = nil
		enc.streamThreshold = 0
		return err
	}
	if enc.streamErr != nil {
		ise.err = enc.streamErr
		enc.streamOut = nil
		enc.streamThreshold = 0
		return enc.streamErr
	}
	enc.flushRemaining()
	ise.buf = enc.buf // reuse encoder's buffer (may have grown) for next Encode
	enc.streamOut = nil
	enc.streamThreshold = 0

	ise.buf = append(ise.buf[:0], '\n')
	if _, err := ise.w.Write(ise.buf); err != nil {
		ise.err = err
		return err
	}
	return nil
}

// SetIndent sets the indentation prefix and string for formatted output (like StreamEncoder.SetIndent).
func (ise *IncrementalStreamEncoder) SetIndent(prefix, indent string) {
	if ise.config == nil {
		ise.config = ConfigDefault()
	}
	ise.config.IndentPrefix = prefix
	ise.config.IndentString = indent
	ise.config.DoIndent = true
}

// SetEscapeHTML sets whether to escape <, >, and & (like StreamEncoder.SetEscapeHTML).
func (ise *IncrementalStreamEncoder) SetEscapeHTML(on bool) {
	if ise.config == nil {
		ise.config = ConfigDefault()
	}
	ise.config.EscapeHTML = on
}

// ensureWriteSession binds the internal encoder to the stream on first structural write.
// Call when writeDepth == 0 at the start of WriteObjectStart or WriteArrayStart.
// When valueComplete is set (previous value just finished), writes a newline for NDJSON and does not clear the buffer.
func (ise *IncrementalStreamEncoder) ensureWriteSession() {
	if ise.config == nil {
		ise.config = ConfigDefault()
	}
	enc := ise.enc
	if ise.valueComplete {
		ise.valueComplete = false
		enc.writeByte('\n')
		if enc.streamErr != nil {
			ise.err = enc.streamErr
			return
		}
		return // encoder already bound; buffer not cleared so we append next value after newline
	}
	ise.buf = ise.buf[:0]
	enc.buf = ise.buf
	enc.streamOut = ise.w
	enc.streamThreshold = ise.flushThreshold
	enc.config = ise.config
	enc.depth = 0
	enc.indentLevel = ise.writeIndentLevel
	enc.ptrSeenCount = 0
	enc.streamErr = nil
	if enc.ptrSeen != nil {
		for k := range enc.ptrSeen {
			delete(enc.ptrSeen, k)
		}
	}
}

// writeArrayComma writes newline+indent before the first array element, or comma+newline+indent before subsequent elements.
// Call before writing an array value; marks current frame as not first.
func (ise *IncrementalStreamEncoder) writeArrayComma() bool {
	if ise.err != nil {
		return false
	}
	if ise.writeDepth <= 0 {
		return false
	}
	frame := ise.writeStack[ise.writeDepth-1]
	if (frame & 1) != 0 {
		return false // object, not array
	}
	enc := ise.enc
	if (frame & 2) != 0 {
		// first array element: newline+indent only
		ise.writeStack[ise.writeDepth-1] = writeFrameArrayNotFirst
		enc.writeNewlineIndent()
	} else {
		enc.writeByte(',')
		enc.writeNewlineIndent()
	}
	if enc.streamErr != nil {
		ise.err = enc.streamErr
		return false
	}
	return true
}

// WriteObjectStart writes '{' and pushes an object frame. Call WriteObjectField then value writers, then WriteObjectEnd.
func (ise *IncrementalStreamEncoder) WriteObjectStart() error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth >= maxWriteDepth {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	if ise.writeDepth == 0 {
		ise.ensureWriteSession()
		if ise.err != nil {
			return ise.err
		}
	}
	ise.writeStack[ise.writeDepth] = writeFrameObjectFirst
	ise.writeDepth++
	ise.writeIndentLevel++
	ise.enc.indentLevel = ise.writeIndentLevel
	ise.enc.writeByte('{')
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	return nil
}

// WriteObjectEnd writes newline+indent (if not first), '}', and pops the object frame.
func (ise *IncrementalStreamEncoder) WriteObjectEnd() error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth <= 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	frame := ise.writeStack[ise.writeDepth-1]
	if (frame & 1) == 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	ise.writeDepth--
	ise.writeIndentLevel--
	ise.enc.indentLevel = ise.writeIndentLevel
	if (frame & 2) == 0 {
		ise.enc.writeNewlineIndent()
	}
	ise.enc.writeByte('}')
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	if ise.writeDepth == 0 {
		ise.buf = ise.enc.buf // sync so buffer growth is visible
		ise.valueComplete = true
	}
	return nil
}

// WriteArrayStart writes '[' and pushes an array frame. Call value writers, then WriteArrayEnd.
func (ise *IncrementalStreamEncoder) WriteArrayStart() error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth >= maxWriteDepth {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	if ise.writeDepth == 0 {
		ise.ensureWriteSession()
		if ise.err != nil {
			return ise.err
		}
	}
	ise.writeStack[ise.writeDepth] = writeFrameArrayFirst
	ise.writeDepth++
	ise.writeIndentLevel++
	ise.enc.indentLevel = ise.writeIndentLevel
	ise.enc.writeByte('[')
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	return nil
}

// WriteArrayEnd writes newline+indent (if not first), ']', and pops the array frame.
func (ise *IncrementalStreamEncoder) WriteArrayEnd() error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth <= 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	frame := ise.writeStack[ise.writeDepth-1]
	if (frame & 1) != 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	ise.writeDepth--
	ise.writeIndentLevel--
	ise.enc.indentLevel = ise.writeIndentLevel
	if (frame & 2) == 0 {
		ise.enc.writeNewlineIndent()
	}
	ise.enc.writeByte(']')
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	if ise.writeDepth == 0 {
		ise.buf = ise.enc.buf // sync so buffer growth is visible
		ise.valueComplete = true
	}
	return nil
}

// WriteObjectField writes the object key and colon. Call once per field before writing the value.
// If not the first field, writes ',' and newline+indent first.
func (ise *IncrementalStreamEncoder) WriteObjectField(key string) error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth <= 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	frame := ise.writeStack[ise.writeDepth-1]
	if (frame & 1) == 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	enc := ise.enc
	if (frame & 2) != 0 {
		// first field: newline+indent before key
		enc.writeNewlineIndent()
	} else {
		enc.writeByte(',')
		enc.writeNewlineIndent()
	}
	ise.writeStack[ise.writeDepth-1] = writeFrameObjectNotFirst
	if err := enc.encodeString(key); err != nil {
		ise.err = err
		return err
	}
	enc.writeColonSeparator()
	if enc.streamErr != nil {
		ise.err = enc.streamErr
		return enc.streamErr
	}
	return nil
}

// WriteString writes a JSON string value. For array elements, writes comma+indent when not first.
func (ise *IncrementalStreamEncoder) WriteString(s string) error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth <= 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	ise.writeArrayComma()
	if ise.err != nil {
		return ise.err
	}
	if err := ise.enc.encodeString(s); err != nil {
		ise.err = err
		return err
	}
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	return nil
}

// WriteInt64 writes a JSON number from int64. For array elements, writes comma+indent when not first.
func (ise *IncrementalStreamEncoder) WriteInt64(i int64) error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth <= 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	ise.writeArrayComma()
	if ise.err != nil {
		return ise.err
	}
	if err := ise.enc.encodeInt(i); err != nil {
		ise.err = err
		return err
	}
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	return nil
}

// WriteUint64 writes a JSON number from uint64. For array elements, writes comma+indent when not first.
func (ise *IncrementalStreamEncoder) WriteUint64(u uint64) error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth <= 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	ise.writeArrayComma()
	if ise.err != nil {
		return ise.err
	}
	if err := ise.enc.encodeUint(u); err != nil {
		ise.err = err
		return err
	}
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	return nil
}

// WriteFloat64 writes a JSON number from float64. For array elements, writes comma+indent when not first.
func (ise *IncrementalStreamEncoder) WriteFloat64(f float64) error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth <= 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	ise.writeArrayComma()
	if ise.err != nil {
		return ise.err
	}
	if err := ise.enc.encodeFloat(f, false); err != nil {
		ise.err = err
		return err
	}
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	return nil
}

// WriteFloat32 writes a JSON number from float32. For array elements, writes comma+indent when not first.
func (ise *IncrementalStreamEncoder) WriteFloat32(f float32) error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth <= 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	ise.writeArrayComma()
	if ise.err != nil {
		return ise.err
	}
	if err := ise.enc.encodeFloat(float64(f), true); err != nil {
		ise.err = err
		return err
	}
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	return nil
}

// WriteBool writes a JSON boolean. For array elements, writes comma+indent when not first.
func (ise *IncrementalStreamEncoder) WriteBool(b bool) error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth <= 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	ise.writeArrayComma()
	if ise.err != nil {
		return ise.err
	}
	ise.enc.encodeBool(b)
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	return nil
}

// WriteNull writes a JSON null. For array elements, writes comma+indent when not first.
func (ise *IncrementalStreamEncoder) WriteNull() error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth <= 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	ise.writeArrayComma()
	if ise.err != nil {
		return ise.err
	}
	ise.enc.writeBytes(nullBytes)
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	return nil
}

// WriteValue encodes v as JSON and appends it. For array elements, writes comma+indent when not first.
// Prefer WriteString, WriteInt64, etc. on hot paths to avoid reflection.
func (ise *IncrementalStreamEncoder) WriteValue(v interface{}) error {
	if ise.err != nil {
		return ise.err
	}
	if ise.writeDepth <= 0 {
		ise.err = ErrInvalidWriteState
		return ErrInvalidWriteState
	}
	ise.writeArrayComma()
	if ise.err != nil {
		return ise.err
	}
	_, err := ise.enc.Encode(v)
	if err != nil {
		ise.err = err
		return err
	}
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
	}
	return nil
}

// Flush writes any buffered data to the underlying io.Writer. Returns the first write error if any.
func (ise *IncrementalStreamEncoder) Flush() error {
	if ise.err != nil {
		return ise.err
	}
	ise.enc.flushRemaining()
	ise.buf = ise.enc.buf
	if ise.enc.streamErr != nil {
		ise.err = ise.enc.streamErr
		return ise.err
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
