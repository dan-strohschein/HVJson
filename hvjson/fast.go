package hvjson

import (
	"reflect"
	"sync"
	"unsafe"
)

// encoderFunc is a pre-compiled encoding function for a specific type
type encoderFunc func(e *Encoder, ptr unsafe.Pointer) error

var (
	encoderFuncCache     sync.Map // map[reflect.Type]encoderFunc
	encoderCompileOnceMu sync.Mutex
	encoderCompileOnce   = make(map[reflect.Type]*sync.Once)
)

// getEncoderFunc returns a pre-compiled encoder for the given type
// Uses sync.Once to prevent duplicate compilation under high concurrency
func getEncoderFunc(t reflect.Type) encoderFunc {
	// Fast path: check if already compiled
	if fn, ok := encoderFuncCache.Load(t); ok {
		return fn.(encoderFunc)
	}

	// Slow path: ensure only one goroutine compiles encoder for this type
	encoderCompileOnceMu.Lock()
	once, exists := encoderCompileOnce[t]
	if !exists {
		once = &sync.Once{}
		encoderCompileOnce[t] = once
	}
	encoderCompileOnceMu.Unlock()

	// Only one goroutine will execute this block per type
	var compiled encoderFunc
	once.Do(func() {
		compiled = compileEncoderFunc(t)
		encoderFuncCache.Store(t, compiled)
	})

	// If another goroutine compiled it, load from cache
	if compiled == nil {
		fn, _ := encoderFuncCache.Load(t)
		compiled = fn.(encoderFunc)
	}

	return compiled
}

// compileEncoderFunc generates an optimized encoder for a type
func compileEncoderFunc(t reflect.Type) encoderFunc {
	switch t.Kind() {
	case reflect.Struct:
		return compileStructEncoder(t)
	case reflect.Ptr:
		elem := t.Elem()
		elemFunc := getEncoderFunc(elem)
		return func(e *Encoder, ptr unsafe.Pointer) error {
			p := *(*unsafe.Pointer)(ptr)
			if p == nil {
				e.buf = append(e.buf, "null"...)
				return nil
			}
			return elemFunc(e, p)
		}
	case reflect.String:
		return func(e *Encoder, ptr unsafe.Pointer) error {
			s := *(*string)(ptr)
			return e.encodeString(s)
		}
	case reflect.Int:
		return func(e *Encoder, ptr unsafe.Pointer) error {
			i := *(*int)(ptr)
			return e.encodeInt(int64(i))
		}
	case reflect.Int64:
		return func(e *Encoder, ptr unsafe.Pointer) error {
			i := *(*int64)(ptr)
			return e.encodeInt(i)
		}
	case reflect.Int32:
		return func(e *Encoder, ptr unsafe.Pointer) error {
			i := *(*int32)(ptr)
			return e.encodeInt(int64(i))
		}
	case reflect.Bool:
		return func(e *Encoder, ptr unsafe.Pointer) error {
			b := *(*bool)(ptr)
			return e.encodeBool(b)
		}
	case reflect.Slice:
		return compileSliceEncoder(t)
	default:
		// Fallback to reflection
		return func(e *Encoder, ptr unsafe.Pointer) error {
			v := reflect.NewAt(t, ptr).Elem()
			return e.encodeValue(v)
		}
	}
}

// compileStructEncoder creates an optimized struct encoder
func compileStructEncoder(t reflect.Type) encoderFunc {
	fields := getCachedFields(t)

	// Create field encoders
	type fieldEncoder struct {
		encodedName []byte // Pre-encoded: "fieldName" (with quotes)
		offset      uintptr
		omitEmpty   bool
		isEmpty     func(ptr unsafe.Pointer) bool // Type-specific empty check
		encoder     encoderFunc
	}

	fieldEncoders := make([]fieldEncoder, 0, len(fields.list))
	for _, f := range fields.list {
		field := t.Field(f.index)
		fieldEncoders = append(fieldEncoders, fieldEncoder{
			encodedName: f.encodedName,
			offset:      field.Offset,
			omitEmpty:   f.omitEmpty,
			isEmpty:     getIsEmptyFunc(field.Type),
			encoder:     getEncoderFunc(field.Type),
		})
	}

	return func(e *Encoder, ptr unsafe.Pointer) error {
		e.depth++
		if e.depth >= e.config.MaxDepth {
			e.depth--
			return &SyntaxError{Code: ErrorStackOverflow, Message: "exceeded maximum nesting depth"}
		}

		e.buf = append(e.buf, '{')
		first := true

		for i := range fieldEncoders {
			fe := &fieldEncoders[i]
			fieldPtr := unsafe.Pointer(uintptr(ptr) + fe.offset)

			// Check omitEmpty with type-specific empty check
			if fe.omitEmpty && fe.isEmpty != nil && fe.isEmpty(fieldPtr) {
				continue
			}

			if first {
				e.indentLevel++
				e.writeNewlineIndent()
				first = false
			} else {
				e.buf = append(e.buf, ',')
				e.writeNewlineIndent()
			}

			// Encode key using pre-encoded name (no function call!)
			e.buf = append(e.buf, fe.encodedName...)
			e.writeColonSeparator()

			// Encode value
			if err := fe.encoder(e, fieldPtr); err != nil {
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
}

// getIsEmptyFunc returns a function that checks if a value is empty for omitEmpty
func getIsEmptyFunc(t reflect.Type) func(ptr unsafe.Pointer) bool {
	switch t.Kind() {
	case reflect.Bool:
		return func(ptr unsafe.Pointer) bool {
			return !*(*bool)(ptr)
		}
	case reflect.Int:
		return func(ptr unsafe.Pointer) bool {
			return *(*int)(ptr) == 0
		}
	case reflect.Int8:
		return func(ptr unsafe.Pointer) bool {
			return *(*int8)(ptr) == 0
		}
	case reflect.Int16:
		return func(ptr unsafe.Pointer) bool {
			return *(*int16)(ptr) == 0
		}
	case reflect.Int32:
		return func(ptr unsafe.Pointer) bool {
			return *(*int32)(ptr) == 0
		}
	case reflect.Int64:
		return func(ptr unsafe.Pointer) bool {
			return *(*int64)(ptr) == 0
		}
	case reflect.Uint, reflect.Uintptr:
		return func(ptr unsafe.Pointer) bool {
			return *(*uint)(ptr) == 0
		}
	case reflect.Uint8:
		return func(ptr unsafe.Pointer) bool {
			return *(*uint8)(ptr) == 0
		}
	case reflect.Uint16:
		return func(ptr unsafe.Pointer) bool {
			return *(*uint16)(ptr) == 0
		}
	case reflect.Uint32:
		return func(ptr unsafe.Pointer) bool {
			return *(*uint32)(ptr) == 0
		}
	case reflect.Uint64:
		return func(ptr unsafe.Pointer) bool {
			return *(*uint64)(ptr) == 0
		}
	case reflect.Float32:
		return func(ptr unsafe.Pointer) bool {
			return *(*float32)(ptr) == 0
		}
	case reflect.Float64:
		return func(ptr unsafe.Pointer) bool {
			return *(*float64)(ptr) == 0
		}
	case reflect.String:
		return func(ptr unsafe.Pointer) bool {
			return *(*string)(ptr) == ""
		}
	case reflect.Slice, reflect.Map:
		return func(ptr unsafe.Pointer) bool {
			// Check for nil or empty
			slice := (*sliceHeader)(ptr)
			return slice.Data == nil || slice.Len == 0
		}
	case reflect.Ptr, reflect.Interface:
		return func(ptr unsafe.Pointer) bool {
			return *(*unsafe.Pointer)(ptr) == nil
		}
	case reflect.Array:
		// Arrays are never "empty" in encoding/json sense
		return nil
	case reflect.Struct:
		// Structs are never "empty" in encoding/json sense (unless they implement specific interface)
		return nil
	default:
		return nil
	}
}

// compileSliceEncoder creates an optimized slice encoder
func compileSliceEncoder(t reflect.Type) encoderFunc {
	elemType := t.Elem()
	elemSize := elemType.Size()

	// Specialized encoder for []string (very common)
	if elemType.Kind() == reflect.String {
		return func(e *Encoder, ptr unsafe.Pointer) error {
			slice := (*sliceHeader)(ptr)
			if slice.Data == nil {
				e.buf = append(e.buf, "null"...)
				return nil
			}

			e.depth++
			if e.depth >= e.config.MaxDepth {
				e.depth--
				return &SyntaxError{Code: ErrorStackOverflow, Message: "exceeded maximum nesting depth"}
			}

			e.buf = append(e.buf, '[')
			if slice.Len > 0 {
				e.indentLevel++
				e.writeNewlineIndent()
			}

			for i := 0; i < slice.Len; i++ {
				if i > 0 {
					e.buf = append(e.buf, ',')
					e.writeNewlineIndent()
				}
				// Direct string access without function pointer call
				s := *(*string)(unsafe.Pointer(uintptr(slice.Data) + uintptr(i)*elemSize))
				if err := e.encodeString(s); err != nil {
					e.depth--
					return err
				}
			}

			if slice.Len > 0 {
				e.indentLevel--
				e.writeNewlineIndent()
			}
			e.buf = append(e.buf, ']')
			e.depth--
			return nil
		}
	}

	// Generic slice encoder
	elemEncoder := getEncoderFunc(elemType)

	return func(e *Encoder, ptr unsafe.Pointer) error {
		slice := (*sliceHeader)(ptr)
		if slice.Data == nil {
			e.buf = append(e.buf, "null"...)
			return nil
		}

		e.depth++
		if e.depth >= e.config.MaxDepth {
			e.depth--
			return &SyntaxError{Code: ErrorStackOverflow, Message: "exceeded maximum nesting depth"}
		}

		e.buf = append(e.buf, '[')
		if slice.Len > 0 {
			e.indentLevel++
			e.writeNewlineIndent()
		}

		for i := 0; i < slice.Len; i++ {
			if i > 0 {
				e.buf = append(e.buf, ',')
				e.writeNewlineIndent()
			}
			elemPtr := unsafe.Pointer(uintptr(slice.Data) + uintptr(i)*elemSize)
			if err := elemEncoder(e, elemPtr); err != nil {
				e.depth--
				return err
			}
		}

		if slice.Len > 0 {
			e.indentLevel--
			e.writeNewlineIndent()
		}
		e.buf = append(e.buf, ']')
		e.depth--
		return nil
	}
}

type sliceHeader struct {
	Data unsafe.Pointer
	Len  int
	Cap  int
}

// FastMarshal uses pre-compiled encoders for maximum performance
func FastMarshal(v interface{}) ([]byte, error) {
	return FastMarshalWithConfig(v, ConfigDefault())
}

// FastMarshalIndent is like FastMarshal but with indentation for readable output.
// Uses pre-compiled encoders for maximum performance even with indentation.
func FastMarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	config := &Config{
		MaxDepth:     10000,
		IndentPrefix: prefix,
		IndentString: indent,
		DoIndent:     true,
	}
	return FastMarshalWithConfig(v, config)
}

// FastMarshalWithConfig uses pre-compiled encoders with custom config
func FastMarshalWithConfig(v interface{}, config *Config) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}

	val := reflect.ValueOf(v)
	t := val.Type()

	// Get pre-compiled encoder
	encoderFn := getEncoderFunc(t)

	// Get encoder from pool
	encoder := newEncoder(config)
	defer encoder.Release()

	// Use unsafe pointer for direct access
	var ptr unsafe.Pointer
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return []byte("null"), nil
		}
		ptr = unsafe.Pointer(val.Pointer())
		// Update type and encoder for pointed-to value
		t = t.Elem()
		encoderFn = getEncoderFunc(t)
	} else if val.CanAddr() {
		ptr = unsafe.Pointer(val.UnsafeAddr())
	} else {
		// Value is not addressable, allocate and copy
		ptrVal := reflect.New(t)
		ptrVal.Elem().Set(val)
		ptr = unsafe.Pointer(ptrVal.Pointer())
	}

	if err := encoderFn(encoder, ptr); err != nil {
		return nil, err
	}

	return encoder.buf, nil
}

// fastMarshalToWithEncoder uses pre-compiled encoders with an existing encoder.
// Used by MarshalTo for zero-copy encoding.
func fastMarshalToWithEncoder(encoder *Encoder, v interface{}) ([]byte, error) {
	if v == nil {
		encoder.buf = append(encoder.buf, "null"...)
		return encoder.buf, nil
	}

	val := reflect.ValueOf(v)
	t := val.Type()

	// Get pre-compiled encoder
	encoderFn := getEncoderFunc(t)

	// Use unsafe pointer for direct access
	var ptr unsafe.Pointer
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			encoder.buf = append(encoder.buf, "null"...)
			return encoder.buf, nil
		}
		ptr = unsafe.Pointer(val.Pointer())
		// Update type and encoder for pointed-to value
		t = t.Elem()
		encoderFn = getEncoderFunc(t)
	} else if val.CanAddr() {
		ptr = unsafe.Pointer(val.UnsafeAddr())
	} else {
		// Value is not addressable, allocate and copy
		ptrVal := reflect.New(t)
		ptrVal.Elem().Set(val)
		ptr = unsafe.Pointer(ptrVal.Pointer())
	}

	if err := encoderFn(encoder, ptr); err != nil {
		return nil, err
	}

	return encoder.buf, nil
}
