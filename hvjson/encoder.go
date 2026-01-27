package hvjson

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"sync"
	"unsafe"

	simd "github.com/dan-strohschein/syndrdb-simd"
)

// Pre-computed small integers for fast encoding (0-9999)
var smallIntTable [10000][]byte

// Pre-computed indent tables for common patterns (0-16 levels)
const maxPrecomputedIndentLevel = 16

// Size of inline pointer seen array for cycle detection (avoids map allocation for simple structs)
const ptrSeenInlineSize = 8

var (
	indent2Space [maxPrecomputedIndentLevel + 1][]byte // 2-space indent
	indent4Space [maxPrecomputedIndentLevel + 1][]byte // 4-space indent
	indentTab    [maxPrecomputedIndentLevel + 1][]byte // tab indent
)

func init() {
	for i := 0; i < 10000; i++ {
		smallIntTable[i] = []byte(strconv.Itoa(i))
	}

	// Pre-compute common indent patterns
	for level := 0; level <= maxPrecomputedIndentLevel; level++ {
		// 2-space indent
		indent2 := make([]byte, level*2)
		for j := 0; j < level*2; j++ {
			indent2[j] = ' '
		}
		indent2Space[level] = indent2

		// 4-space indent
		indent4 := make([]byte, level*4)
		for j := 0; j < level*4; j++ {
			indent4[j] = ' '
		}
		indent4Space[level] = indent4

		// Tab indent
		indentT := make([]byte, level)
		for j := 0; j < level; j++ {
			indentT[j] = '\t'
		}
		indentTab[level] = indentT
	}
}

type Encoder struct {
	buf            []byte
	scratch        [256]byte // Pre-allocated scratch space to avoid small allocations
	config         *Config
	depth          int
	indentLevel    int // Current indentation level for MarshalIndent
	// Inline pointer tracking for cycle detection (avoids map allocation for simple structs)
	ptrSeenInline  [ptrSeenInlineSize]uintptr
	ptrSeenCount   int
	ptrSeen        map[uintptr]bool // Fallback for complex/deeply nested structures
}

var encoderPool = sync.Pool{
	New: func() interface{} {
		return &Encoder{}
	},
}

func newEncoder(config *Config) *Encoder {
	if config == nil {
		config = ConfigDefault()
	}
	e := encoderPool.Get().(*Encoder)
	// Use scratch space for initial buffer to avoid allocation
	e.buf = e.scratch[:0]
	e.config = config
	e.depth = 0
	e.indentLevel = 0
	e.ptrSeenCount = 0 // Reset inline pointer tracking
	if e.ptrSeen != nil {
		for k := range e.ptrSeen {
			delete(e.ptrSeen, k)
		}
	}
	return e
}

// newEncoderWithBuffer creates an encoder that uses the provided buffer
// instead of the internal scratch space. Used by MarshalTo for zero-copy encoding.
func newEncoderWithBuffer(buf []byte, config *Config) *Encoder {
	if config == nil {
		config = ConfigDefault()
	}
	e := encoderPool.Get().(*Encoder)
	e.buf = buf // Use provided buffer directly
	e.config = config
	e.depth = 0
	e.indentLevel = 0
	e.ptrSeenCount = 0
	if e.ptrSeen != nil {
		for k := range e.ptrSeen {
			delete(e.ptrSeen, k)
		}
	}
	return e
}

func (e *Encoder) Release() {
	// Don't return buffer to pool - it's used by the result
	e.buf = nil
	encoderPool.Put(e)
}

// writeNewlineIndent writes a newline and the current indentation.
// Uses pre-computed tables for common indent patterns for zero allocation.
func (e *Encoder) writeNewlineIndent() {
	if !e.config.DoIndent {
		return
	}

	e.buf = append(e.buf, '\n')

	// Write prefix if set
	if len(e.config.IndentPrefix) > 0 {
		e.buf = append(e.buf, e.config.IndentPrefix...)
	}

	// Use pre-computed table if possible
	level := e.indentLevel
	indentStr := e.config.IndentString

	switch indentStr {
	case "  ": // 2-space (most common)
		if level <= maxPrecomputedIndentLevel {
			e.buf = append(e.buf, indent2Space[level]...)
			return
		}
	case "    ": // 4-space
		if level <= maxPrecomputedIndentLevel {
			e.buf = append(e.buf, indent4Space[level]...)
			return
		}
	case "\t": // tab
		if level <= maxPrecomputedIndentLevel {
			e.buf = append(e.buf, indentTab[level]...)
			return
		}
	}

	// Fallback: compute indent on the fly
	for i := 0; i < level; i++ {
		e.buf = append(e.buf, indentStr...)
	}
}

// writeColonSeparator writes the colon separator between key and value.
// Adds a space after the colon when indenting for readability.
func (e *Encoder) writeColonSeparator() {
	if e.config.DoIndent {
		e.buf = append(e.buf, ':', ' ')
	} else {
		e.buf = append(e.buf, ':')
	}
}

func (e *Encoder) Encode(v interface{}) ([]byte, error) {
	// Fast path for common types without reflection
	switch val := v.(type) {
	case string:
		return e.encodeStringFast(val)
	case int:
		return e.encodeIntFast(int64(val))
	case int64:
		return e.encodeIntFast(val)
	case int32:
		return e.encodeIntFast(int64(val))
	case int16:
		return e.encodeIntFast(int64(val))
	case int8:
		return e.encodeIntFast(int64(val))
	case uint:
		return e.encodeUintFast(uint64(val))
	case uint64:
		return e.encodeUintFast(val)
	case uint32:
		return e.encodeUintFast(uint64(val))
	case uint16:
		return e.encodeUintFast(uint64(val))
	case uint8:
		return e.encodeUintFast(uint64(val))
	case float64:
		return e.encodeFloatFast(val, false)
	case float32:
		return e.encodeFloatFast(float64(val), true)
	case bool:
		return e.encodeBoolFast(val)
	case nil:
		e.buf = append(e.buf, "null"...)
		return e.buf, nil
	}

	rv := reflect.ValueOf(v)
	if err := e.encodeValue(rv); err != nil {
		return nil, err
	}

	// If buffer is using scratch space, make a copy for return
	// Check if buf points to scratch array using uintptr comparison
	if len(e.buf) > 0 {
		bufPtr := uintptr(unsafe.Pointer(&e.buf[0]))
		scratchStart := uintptr(unsafe.Pointer(&e.scratch[0]))
		scratchEnd := scratchStart + uintptr(len(e.scratch))

		if bufPtr >= scratchStart && bufPtr < scratchEnd {
			result := make([]byte, len(e.buf))
			copy(result, e.buf)
			return result, nil
		}
	}
	return e.buf, nil
}

func (e *Encoder) encodeValue(v reflect.Value) error {
	if e.depth >= e.config.MaxDepth {
		return &SyntaxError{Code: ErrorStackOverflow, Message: "exceeded maximum nesting depth"}
	}

	if !v.IsValid() {
		e.buf = append(e.buf, "null"...)
		return nil
	}

	if v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		if v.Kind() == reflect.Ptr {
			ptr := v.Pointer()
			// Check for cycles - first try inline array, then fallback to map
			if e.ptrSeenCount < ptrSeenInlineSize {
				// Check inline array for cycle
				for i := 0; i < e.ptrSeenCount; i++ {
					if e.ptrSeenInline[i] == ptr {
						return &SyntaxError{Code: ErrorInvalidValue, Message: "encountered a cycle via pointer"}
					}
				}
				e.ptrSeenInline[e.ptrSeenCount] = ptr
				e.ptrSeenCount++
				defer func() { e.ptrSeenCount-- }()
			} else {
				// Fallback to map for deeply nested structures
				if e.ptrSeen == nil {
					e.ptrSeen = make(map[uintptr]bool, 8)
				}
				if e.ptrSeen[ptr] {
					return &SyntaxError{Code: ErrorInvalidValue, Message: "encountered a cycle via pointer"}
				}
				e.ptrSeen[ptr] = true
				defer delete(e.ptrSeen, ptr)
			}
		}
		return e.encodeValue(v.Elem())
	}

	switch v.Kind() {
	case reflect.Bool:
		return e.encodeBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return e.encodeInt(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return e.encodeUint(v.Uint())
	case reflect.Float32, reflect.Float64:
		return e.encodeFloat(v.Float(), v.Kind() == reflect.Float32)
	case reflect.String:
		return e.encodeString(v.String())
	case reflect.Struct:
		return e.encodeStruct(v)
	case reflect.Map:
		return e.encodeMap(v)
	case reflect.Slice, reflect.Array:
		return e.encodeSlice(v)
	default:
		return &SyntaxError{Code: ErrorInvalidValue, Message: fmt.Sprintf("unsupported type: %s", v.Type())}
	}
}

func (e *Encoder) encodeValueUnsafe(ptr unsafe.Pointer, typ reflect.Type) error {
	if e.depth >= e.config.MaxDepth {
		return &SyntaxError{Code: ErrorStackOverflow, Message: "exceeded maximum nesting depth"}
	}

	// Handle pointer types
	if typ.Kind() == reflect.Ptr {
		ptrVal := *(*unsafe.Pointer)(ptr)
		if ptrVal == nil {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		// Check for cycles - first try inline array, then fallback to map
		ptrAddr := uintptr(ptrVal)
		if e.ptrSeenCount < ptrSeenInlineSize {
			// Check inline array for cycle
			for i := 0; i < e.ptrSeenCount; i++ {
				if e.ptrSeenInline[i] == ptrAddr {
					return &SyntaxError{Code: ErrorInvalidValue, Message: "encountered a cycle via pointer"}
				}
			}
			e.ptrSeenInline[e.ptrSeenCount] = ptrAddr
			e.ptrSeenCount++
			defer func() { e.ptrSeenCount-- }()
		} else {
			// Fallback to map for deeply nested structures
			if e.ptrSeen == nil {
				e.ptrSeen = make(map[uintptr]bool, 8)
			}
			if e.ptrSeen[ptrAddr] {
				return &SyntaxError{Code: ErrorInvalidValue, Message: "encountered a cycle via pointer"}
			}
			e.ptrSeen[ptrAddr] = true
			defer delete(e.ptrSeen, ptrAddr)
		}
		return e.encodeValueUnsafe(ptrVal, typ.Elem())
	}

	switch typ.Kind() {
	case reflect.Bool:
		return e.encodeBool(*(*bool)(ptr))
	case reflect.Int:
		return e.encodeInt(int64(*(*int)(ptr)))
	case reflect.Int8:
		return e.encodeInt(int64(*(*int8)(ptr)))
	case reflect.Int16:
		return e.encodeInt(int64(*(*int16)(ptr)))
	case reflect.Int32:
		return e.encodeInt(int64(*(*int32)(ptr)))
	case reflect.Int64:
		return e.encodeInt(*(*int64)(ptr))
	case reflect.Uint:
		return e.encodeUint(uint64(*(*uint)(ptr)))
	case reflect.Uint8:
		return e.encodeUint(uint64(*(*uint8)(ptr)))
	case reflect.Uint16:
		return e.encodeUint(uint64(*(*uint16)(ptr)))
	case reflect.Uint32:
		return e.encodeUint(uint64(*(*uint32)(ptr)))
	case reflect.Uint64:
		return e.encodeUint(*(*uint64)(ptr))
	case reflect.Float32:
		return e.encodeFloat(float64(*(*float32)(ptr)), true)
	case reflect.Float64:
		return e.encodeFloat(*(*float64)(ptr), false)
	case reflect.String:
		return e.encodeString(*(*string)(ptr))
	case reflect.Struct:
		fields := getCachedFields(typ)
		return e.encodeStructUnsafe(ptr, fields)
	case reflect.Slice:
		// For slice, we need to use reflection since unsafe doesn't give us length/cap easily
		v := reflect.NewAt(typ, ptr).Elem()
		return e.encodeSlice(v)
	case reflect.Array:
		v := reflect.NewAt(typ, ptr).Elem()
		return e.encodeSlice(v)
	case reflect.Map:
		v := reflect.NewAt(typ, ptr).Elem()
		return e.encodeMap(v)
	default:
		return &SyntaxError{Code: ErrorInvalidValue, Message: fmt.Sprintf("unsupported type: %s", typ)}
	}
}

//go:inline
func (e *Encoder) encodeBool(b bool) error {
	if b {
		e.buf = append(e.buf, "true"...)
	} else {
		e.buf = append(e.buf, "false"...)
	}
	return nil
}

// formatInt64Fast is optimized integer formatting (faster than strconv for common cases)
//
//go:inline
func formatInt64Fast(i int64, buf []byte) int {
	if i == 0 {
		buf[0] = '0'
		return 1
	}

	neg := i < 0
	if neg {
		i = -i
	}

	// Format digits in reverse
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}

	if neg {
		pos--
		buf[pos] = '-'
	}

	// Shift to beginning if needed
	if pos > 0 {
		n := len(buf) - pos
		copy(buf[0:], buf[pos:len(buf)])
		return n
	}
	return len(buf) - pos
}

//go:inline
func (e *Encoder) encodeInt(i int64) error {
	// Fast path for small positive integers (lookup table 0-9999)
	if i >= 0 && i < 10000 {
		e.buf = append(e.buf, smallIntTable[i]...)
		return nil
	}

	// Use SIMD-optimized integer formatting
	n := simd.FormatInt64(i, e.scratch[:20])
	e.buf = append(e.buf, e.scratch[:n]...)
	return nil
}

// formatUint64Fast is optimized unsigned integer formatting
//
//go:inline
func formatUint64Fast(u uint64, buf []byte) int {
	if u == 0 {
		buf[0] = '0'
		return 1
	}

	// Format digits in reverse
	pos := len(buf)
	for u > 0 {
		pos--
		buf[pos] = byte('0' + u%10)
		u /= 10
	}

	// Shift to beginning if needed
	if pos > 0 {
		n := len(buf) - pos
		copy(buf[0:], buf[pos:len(buf)])
		return n
	}
	return len(buf) - pos
}

//go:inline
func (e *Encoder) encodeUint(u uint64) error {
	// Fast path for small integers (lookup table 0-9999)
	if u < 10000 {
		e.buf = append(e.buf, smallIntTable[u]...)
		return nil
	}

	// Use SIMD-optimized integer formatting
	n := simd.FormatUint64(u, e.scratch[:20])
	e.buf = append(e.buf, e.scratch[:n]...)
	return nil
}

func (e *Encoder) encodeFloat(f float64, is32bit bool) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return &SyntaxError{Code: ErrorInvalidValue, Message: "invalid float value (NaN or Inf)"}
	}
	
	// Use SIMD-optimized float formatting (requires 24-byte buffer)
	// FormatFloat64 returns bytes written to scratch buffer
	n := simd.FormatFloat64(f, e.scratch[:24])
	e.buf = append(e.buf, e.scratch[:n]...)
	return nil
}

func (e *Encoder) encodeString(s string) error {
	e.buf = append(e.buf, '"')
	if s == "" {
		e.buf = append(e.buf, '"')
		return nil
	}

	// Convert string to bytes (zero-copy)
	bytes := unsafe.Slice(unsafe.StringData(s), len(s))

	// Check if we need HTML escaping
	needsHTMLEscape := e.config.EscapeHTML

	// Fast path - no escaping needed
	// For short strings (<8 bytes), inline check is faster than SIMD call
	// ARM64 NEON processes 16 bytes atomically, so even 8-byte strings benefit from SIMD
	needsEscape := false
	if len(s) < 8 {
		for i := 0; i < len(bytes); i++ {
			c := bytes[i]
			if c == '"' || c == '\\' || c == '/' || c < 0x20 {
				needsEscape = true
				break
			}
			if needsHTMLEscape && (c == '<' || c == '>' || c == '&') {
				needsEscape = true
				break
			}
		}
	} else {
		needsEscape = simd.HasSpecialChars(bytes)
		// Also check for HTML chars if EscapeHTML is enabled
		if !needsEscape && needsHTMLEscape {
			for i := 0; i < len(bytes); i++ {
				c := bytes[i]
				if c == '<' || c == '>' || c == '&' {
					needsEscape = true
					break
				}
			}
		}
	}

	if !needsEscape {
		e.buf = append(e.buf, s...)
		e.buf = append(e.buf, '"')
		return nil
	}

	// Slow path - need escaping
	if needsHTMLEscape {
		// HTML escape path: escape <, >, & as Unicode escapes
		return e.encodeStringWithHTMLEscape(bytes)
	}

	// SIMD accelerated escaping (no HTML escaping)
	// Use EscapedSize for exact allocation (avoids over-allocation)
	escapedLen := simd.EscapedSize(bytes)
	oldLen := len(e.buf)

	// Ensure buffer has capacity
	if cap(e.buf)-oldLen < escapedLen {
		newBuf := make([]byte, oldLen, oldLen+escapedLen)
		copy(newBuf, e.buf)
		e.buf = newBuf
	}
	e.buf = e.buf[:oldLen+escapedLen]

	// SIMD escape
	simd.EscapeJSONString(bytes, e.buf[oldLen:])

	e.buf = append(e.buf, '"')
	return nil
}

// encodeStringWithHTMLEscape escapes JSON special chars plus HTML chars (<, >, &)
func (e *Encoder) encodeStringWithHTMLEscape(bytes []byte) error {
	for _, c := range bytes {
		switch c {
		case '"':
			e.buf = append(e.buf, '\\', '"')
		case '\\':
			e.buf = append(e.buf, '\\', '\\')
		case '/':
			e.buf = append(e.buf, '\\', '/')
		case '\b':
			e.buf = append(e.buf, '\\', 'b')
		case '\f':
			e.buf = append(e.buf, '\\', 'f')
		case '\n':
			e.buf = append(e.buf, '\\', 'n')
		case '\r':
			e.buf = append(e.buf, '\\', 'r')
		case '\t':
			e.buf = append(e.buf, '\\', 't')
		case '<':
			e.buf = append(e.buf, '\\', 'u', '0', '0', '3', 'c')
		case '>':
			e.buf = append(e.buf, '\\', 'u', '0', '0', '3', 'e')
		case '&':
			e.buf = append(e.buf, '\\', 'u', '0', '0', '2', '6')
		default:
			if c < 0x20 {
				// Other control characters: \uXXXX
				e.buf = append(e.buf, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			} else {
				e.buf = append(e.buf, c)
			}
		}
	}
	e.buf = append(e.buf, '"')
	return nil
}

// hexDigits for encoding control characters
var hexDigits = []byte("0123456789abcdef")

func (e *Encoder) encodeStruct(v reflect.Value) error {
	e.depth++

	fields := getCachedFields(v.Type())

	// Use unsafe path if addressable
	if v.CanAddr() {
		err := e.encodeStructUnsafe(unsafe.Pointer(v.UnsafeAddr()), fields)
		e.depth--
		return err
	}

	// Ensure buffer has estimated capacity
	if cap(e.buf)-len(e.buf) < fields.estimatedSize {
		newBuf := make([]byte, len(e.buf), len(e.buf)+fields.estimatedSize)
		copy(newBuf, e.buf)
		e.buf = newBuf
	}

	e.buf = append(e.buf, '{')
	first := true

	for _, field := range fields.list {
		if field.omitEmpty {
			fv := v.Field(field.index)
			if isEmptyValue(fv) {
				continue
			}
		}

		if first {
			e.indentLevel++
			e.writeNewlineIndent()
			first = false
		} else {
			e.buf = append(e.buf, ',')
			e.writeNewlineIndent()
		}

		// Use pre-encoded field name
		e.buf = append(e.buf, field.encodedName...)
		e.writeColonSeparator()

		fv := v.Field(field.index)
		if err := e.encodeValue(fv); err != nil {
			e.depth--
			return err
		}
	}

	if !first {
		e.indentLevel--
		e.writeNewlineIndent()
	}
	e.buf = append(e.buf, '}')
	e.depth--
	return nil
}

func (e *Encoder) encodeStructUnsafe(structPtr unsafe.Pointer, fields *fieldsCache) error {
	// Ensure buffer has estimated capacity
	if cap(e.buf)-len(e.buf) < fields.estimatedSize {
		newBuf := make([]byte, len(e.buf), len(e.buf)+fields.estimatedSize)
		copy(newBuf, e.buf)
		e.buf = newBuf
	}

	e.buf = append(e.buf, '{')
	first := true

	for _, field := range fields.list {
		fieldPtr := unsafe.Pointer(uintptr(structPtr) + field.offset)

		if field.omitEmpty {
			if e.isEmptyValueUnsafe(fieldPtr, field.typ) {
				continue
			}
		}

		if first {
			e.indentLevel++
			e.writeNewlineIndent()
			first = false
		} else {
			e.buf = append(e.buf, ',')
			e.writeNewlineIndent()
		}

		// Use pre-encoded field name
		e.buf = append(e.buf, field.encodedName...)
		e.writeColonSeparator()

		// Use compiled encoder for maximum performance
		if err := field.encoder(e, fieldPtr); err != nil {
			return err
		}
	}

	if !first {
		e.indentLevel--
		e.writeNewlineIndent()
	}
	e.buf = append(e.buf, '}')
	return nil
}

func (e *Encoder) encodeMap(v reflect.Value) error {
	if v.IsNil() {
		e.buf = append(e.buf, "null"...)
		return nil
	}

	e.depth++

	e.buf = append(e.buf, '{')
	keys := v.MapKeys()
	first := true

	for _, key := range keys {
		if first {
			e.indentLevel++
			e.writeNewlineIndent()
			first = false
		} else {
			e.buf = append(e.buf, ',')
			e.writeNewlineIndent()
		}

		keyStr := ""
		switch key.Kind() {
		case reflect.String:
			keyStr = key.String()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			keyStr = strconv.FormatInt(key.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			keyStr = strconv.FormatUint(key.Uint(), 10)
		default:
			e.depth--
			return &SyntaxError{Code: ErrorInvalidValue, Message: fmt.Sprintf("unsupported map key type: %s", key.Type())}
		}

		if err := e.encodeString(keyStr); err != nil {
			e.depth--
			return err
		}
		e.writeColonSeparator()

		if err := e.encodeValue(v.MapIndex(key)); err != nil {
			e.depth--
			return err
		}
	}

	if !first {
		e.indentLevel--
		e.writeNewlineIndent()
	}
	e.buf = append(e.buf, '}')
	e.depth--
	return nil
}

func (e *Encoder) encodeSlice(v reflect.Value) error {
	if v.Kind() == reflect.Slice && v.IsNil() {
		e.buf = append(e.buf, "null"...)
		return nil
	}

	e.depth++

	e.buf = append(e.buf, '[')
	n := v.Len()

	if n > 0 {
		e.indentLevel++
		e.writeNewlineIndent()
	}

	for i := 0; i < n; i++ {
		if i > 0 {
			e.buf = append(e.buf, ',')
			e.writeNewlineIndent()
		}
		if err := e.encodeValue(v.Index(i)); err != nil {
			e.depth--
			return err
		}
	}

	if n > 0 {
		e.indentLevel--
		e.writeNewlineIndent()
	}
	e.buf = append(e.buf, ']')
	e.depth--
	return nil
}

// needsEscapingString checks if string contains characters that need JSON escaping
// Uses SIMD-accelerated scanning for: ", \, /, and control chars < 0x20
//
//go:inline
func needsEscapingString(s string) bool {
	bytes := unsafe.Slice(unsafe.StringData(s), len(s))
	return simd.HasSpecialChars(bytes)
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	}
	return false
}

func (e *Encoder) isEmptyValueUnsafe(ptr unsafe.Pointer, typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.String:
		return len(*(*string)(ptr)) == 0
	case reflect.Bool:
		return !*(*bool)(ptr)
	case reflect.Int:
		return *(*int)(ptr) == 0
	case reflect.Int8:
		return *(*int8)(ptr) == 0
	case reflect.Int16:
		return *(*int16)(ptr) == 0
	case reflect.Int32:
		return *(*int32)(ptr) == 0
	case reflect.Int64:
		return *(*int64)(ptr) == 0
	case reflect.Uint:
		return *(*uint)(ptr) == 0
	case reflect.Uint8:
		return *(*uint8)(ptr) == 0
	case reflect.Uint16:
		return *(*uint16)(ptr) == 0
	case reflect.Uint32:
		return *(*uint32)(ptr) == 0
	case reflect.Uint64:
		return *(*uint64)(ptr) == 0
	case reflect.Float32:
		return *(*float32)(ptr) == 0
	case reflect.Float64:
		return *(*float64)(ptr) == 0
	case reflect.Slice, reflect.Map, reflect.Array:
		// For complex types, fall back to reflection
		v := reflect.NewAt(typ, ptr).Elem()
		return v.Len() == 0
	case reflect.Ptr:
		return *(*unsafe.Pointer)(ptr) == nil
	}
	return false
}

func stringToBytes(s string) []byte {
	return *(*[]byte)(unsafe.Pointer(&struct {
		string
		Cap int
	}{s, len(s)}))
}

// Fast-path methods that avoid reflection overhead
func (e *Encoder) encodeStringFast(s string) ([]byte, error) {
	if err := e.encodeString(s); err != nil {
		return nil, err
	}
	return e.buf, nil
}

func (e *Encoder) encodeIntFast(i int64) ([]byte, error) {
	// Fast path for small positive integers (lookup table 0-9999)
	if i >= 0 && i < 10000 {
		e.buf = append(e.buf, smallIntTable[i]...)
		return e.buf, nil
	}
	// Use SIMD-optimized integer formatting
	n := simd.FormatInt64(i, e.scratch[:20])
	e.buf = append(e.buf, e.scratch[:n]...)
	return e.buf, nil
}

func (e *Encoder) encodeUintFast(u uint64) ([]byte, error) {
	// Fast path for small integers (lookup table 0-9999)
	if u < 10000 {
		e.buf = append(e.buf, smallIntTable[u]...)
		return e.buf, nil
	}
	// Use SIMD-optimized integer formatting
	n := simd.FormatUint64(u, e.scratch[:20])
	e.buf = append(e.buf, e.scratch[:n]...)
	return e.buf, nil
}

func (e *Encoder) encodeFloatFast(f float64, is32bit bool) ([]byte, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, &SyntaxError{Code: ErrorInvalidValue, Message: "invalid float value (NaN or Inf)"}
	}
	// Use SIMD-optimized float formatting (requires 24-byte buffer)
	n := simd.FormatFloat64(f, e.scratch[:24])
	e.buf = append(e.buf, e.scratch[:n]...)
	return e.buf, nil
}

func (e *Encoder) encodeBoolFast(b bool) ([]byte, error) {
	if b {
		e.buf = append(e.buf, "true"...)
	} else {
		e.buf = append(e.buf, "false"...)
	}
	return e.buf, nil
}
