package hvjson

import (
	"fmt"
	"reflect"
	"unsafe"

	simd "github.com/dan-strohschein/syndrdb-simd"
)

type Decoder struct {
	data   []byte
	pos    int
	depth  int
	config *Config
}

func newDecoder(data []byte, config *Config) *Decoder {
	if config == nil {
		config = ConfigDefault()
	}
	return &Decoder{
		data:   data,
		pos:    0,
		depth:  0,
		config: config,
	}
}

// Reset reinitializes the decoder for a new data slice (used by stream decoder pool).
// Pass nil to clear references before returning to pool.
func (d *Decoder) Reset(data []byte) {
	d.data = data
	d.pos = 0
	d.depth = 0
}

func (d *Decoder) Decode(v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return &SyntaxError{Pos: d.pos, Code: ErrorInvalidValue, Message: "decode target must be a non-nil pointer"}
	}

	if !d.config.NoValidateJSONSkip {
		if d.config.UseStreamingUTF8 && len(d.data) > 1024*1024 {
			checker := simd.NewUTF8Checker()
			if err := checker.Validate(d.data); err != nil {
				return WrapSIMDError(d.data, err, ErrorInvalidUTF8)
			}
		} else {
			if err := simd.ValidateUTF8(d.data); err != nil {
				return WrapSIMDError(d.data, err, ErrorInvalidUTF8)
			}
		}
	}

	d.skipWhitespace()
	return d.decodeValue(rv.Elem())
}

func (d *Decoder) skipWhitespace() {
	d.pos = simd.SkipWhitespace(d.data, d.pos)
}

func (d *Decoder) decodeValue(v reflect.Value) error {
	d.skipWhitespace()

	if d.pos >= len(d.data) {
		return d.error(ErrorInvalidJSON, "unexpected end of input")
	}

	c := d.data[d.pos]

	switch c {
	case '{':
		return d.decodeObject(v)
	case '[':
		return d.decodeArray(v)
	case '"':
		return d.decodeString(v)
	case 't', 'f':
		return d.decodeBool(v)
	case 'n':
		return d.decodeNull(v)
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return d.decodeNumber(v)
	default:
		return d.error(ErrorInvalidJSON, fmt.Sprintf("unexpected character %q", c))
	}
}

func (d *Decoder) decodeObject(v reflect.Value) error {
	if d.depth >= d.config.MaxDepth {
		return d.error(ErrorStackOverflow, "exceeded maximum nesting depth")
	}
	d.depth++
	defer func() { d.depth-- }()

	if d.data[d.pos] != '{' {
		return d.error(ErrorInvalidJSON, "expected '{'")
	}
	d.pos++

	v = indirect(v)

	switch v.Kind() {
	case reflect.Map:
		return d.decodeMap(v)
	case reflect.Struct:
		return d.decodeStruct(v)
	case reflect.Interface:
		m := make(map[string]interface{})
		mapVal := reflect.ValueOf(m)
		if err := d.decodeMap(mapVal); err != nil {
			return err
		}
		v.Set(mapVal)
		return nil
	default:
		return d.error(ErrorTypeMismatch, fmt.Sprintf("cannot decode object into %s", v.Type()))
	}
}

func (d *Decoder) decodeMap(v reflect.Value) error {
	t := v.Type()
	if v.IsNil() {
		v.Set(reflect.MakeMap(t))
	}

	keyType := t.Key()
	elemType := t.Elem()

	d.skipWhitespace()

	if d.pos < len(d.data) && d.data[d.pos] == '}' {
		d.pos++
		return nil
	}

	for {
		d.skipWhitespace()

		if d.pos >= len(d.data) || d.data[d.pos] != '"' {
			return d.error(ErrorInvalidJSON, "expected string key")
		}

		keyVal := reflect.New(keyType).Elem()
		if err := d.decodeString(keyVal); err != nil {
			return err
		}

		d.skipWhitespace()

		if d.pos >= len(d.data) || d.data[d.pos] != ':' {
			return d.error(ErrorInvalidJSON, "expected ':'")
		}
		d.pos++

		d.skipWhitespace()

		elemVal := reflect.New(elemType).Elem()
		if err := d.decodeValue(elemVal); err != nil {
			return err
		}

		v.SetMapIndex(keyVal, elemVal)

		d.skipWhitespace()

		if d.pos >= len(d.data) {
			return d.error(ErrorInvalidJSON, "unexpected end of object")
		}

		if d.data[d.pos] == '}' {
			d.pos++
			return nil
		}

		if d.data[d.pos] != ',' {
			return d.error(ErrorInvalidJSON, "expected ',' or '}'")
		}
		d.pos++
	}
}

func (d *Decoder) decodeStruct(v reflect.Value) error {
	fields := getCachedFields(v.Type())

	d.skipWhitespace()

	if d.pos < len(d.data) && d.data[d.pos] == '}' {
		d.pos++
		return nil
	}

	for {
		d.skipWhitespace()

		if d.pos >= len(d.data) || d.data[d.pos] != '"' {
			return d.error(ErrorInvalidJSON, "expected string key")
		}

		fieldName, err := d.parseString()
		if err != nil {
			return err
		}

		d.skipWhitespace()

		if d.pos >= len(d.data) || d.data[d.pos] != ':' {
			return d.error(ErrorInvalidJSON, "expected ':'")
		}
		d.pos++

		field := fields.byExactName[fieldName]
		if field == nil && !d.config.DisableCache {
			field = fields.byFoldedName[foldName(fieldName)]
		}

		if field != nil {
			fieldVal := v.Field(field.index)
			if err := d.decodeValue(fieldVal); err != nil {
				return err
			}
		} else {
			// Unknown field - check if we should error
			if d.config.DisallowUnknownFields {
				return d.error(ErrorTypeMismatch, fmt.Sprintf("unknown field %q", fieldName))
			}
			if err := d.skipValue(); err != nil {
				return err
			}
		}

		d.skipWhitespace()

		if d.pos >= len(d.data) {
			return d.error(ErrorInvalidJSON, "unexpected end of object")
		}

		if d.data[d.pos] == '}' {
			d.pos++
			return nil
		}

		if d.data[d.pos] != ',' {
			return d.error(ErrorInvalidJSON, "expected ',' or '}'")
		}
		d.pos++
	}
}

func (d *Decoder) decodeArray(v reflect.Value) error {
	if d.depth >= d.config.MaxDepth {
		return d.error(ErrorStackOverflow, "exceeded maximum nesting depth")
	}
	d.depth++
	defer func() { d.depth-- }()

	if d.data[d.pos] != '[' {
		return d.error(ErrorInvalidJSON, "expected '['")
	}
	d.pos++

	v = indirect(v)

	switch v.Kind() {
	case reflect.Slice:
		return d.decodeSlice(v)
	case reflect.Array:
		return d.decodeArrayFixed(v)
	case reflect.Interface:
		s := make([]interface{}, 0)
		sliceVal := reflect.ValueOf(&s).Elem()
		if err := d.decodeSlice(sliceVal); err != nil {
			return err
		}
		v.Set(sliceVal)
		return nil
	default:
		return d.error(ErrorTypeMismatch, fmt.Sprintf("cannot decode array into %s", v.Type()))
	}
}

func (d *Decoder) decodeSlice(v reflect.Value) error {
	elemType := v.Type().Elem()

	d.skipWhitespace()

	if d.pos < len(d.data) && d.data[d.pos] == ']' {
		d.pos++
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		return nil
	}

	items := make([]reflect.Value, 0, 8)

	for {
		d.skipWhitespace()

		elem := reflect.New(elemType).Elem()
		if err := d.decodeValue(elem); err != nil {
			return err
		}
		items = append(items, elem)

		d.skipWhitespace()

		if d.pos >= len(d.data) {
			return d.error(ErrorInvalidJSON, "unexpected end of array")
		}

		if d.data[d.pos] == ']' {
			d.pos++
			break
		}

		if d.data[d.pos] != ',' {
			return d.error(ErrorInvalidJSON, "expected ',' or ']'")
		}
		d.pos++
	}

	slice := reflect.MakeSlice(v.Type(), len(items), len(items))
	for i, item := range items {
		slice.Index(i).Set(item)
	}
	v.Set(slice)

	return nil
}

func (d *Decoder) decodeArrayFixed(v reflect.Value) error {
	arrayLen := v.Len()
	elemType := v.Type().Elem()

	d.skipWhitespace()

	if d.pos < len(d.data) && d.data[d.pos] == ']' {
		d.pos++
		return nil
	}

	idx := 0
	for {
		d.skipWhitespace()

		if idx >= arrayLen {
			if err := d.skipValue(); err != nil {
				return err
			}
		} else {
			elem := reflect.New(elemType).Elem()
			if err := d.decodeValue(elem); err != nil {
				return err
			}
			v.Index(idx).Set(elem)
			idx++
		}

		d.skipWhitespace()

		if d.pos >= len(d.data) {
			return d.error(ErrorInvalidJSON, "unexpected end of array")
		}

		if d.data[d.pos] == ']' {
			d.pos++
			return nil
		}

		if d.data[d.pos] != ',' {
			return d.error(ErrorInvalidJSON, "expected ',' or ']'")
		}
		d.pos++
	}
}

func (d *Decoder) decodeString(v reflect.Value) error {
	s, err := d.parseString()
	if err != nil {
		return err
	}

	v = indirect(v)

	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Interface:
		v.Set(reflect.ValueOf(s))
	default:
		return d.error(ErrorTypeMismatch, fmt.Sprintf("cannot decode string into %s", v.Type()))
	}

	return nil
}

func (d *Decoder) parseString() (string, error) {
	if d.data[d.pos] != '"' {
		return "", d.error(ErrorInvalidString, "expected '\"'")
	}
	d.pos++

	start := d.pos

	end := simd.FindQuote(d.data, d.pos)
	if end < 0 {
		return "", d.error(ErrorInvalidString, "unterminated string")
	}

	rawStr := d.data[start:end]
	d.pos = end + 1

	escapeCount := simd.CountEscapes(rawStr)
	if escapeCount == 0 {
		return string(rawStr), nil
	}

	buf := getBuffer(len(rawStr))
	defer putBuffer(buf)

	n, err := simd.UnescapeString(rawStr, buf)
	if err != nil {
		return "", WrapSIMDError(d.data, err, ErrorInvalidString)
	}

	return string(buf[:n]), nil
}

func (d *Decoder) decodeBool(v reflect.Value) error {
	var b bool
	var size int

	if d.pos+4 <= len(d.data) && string(d.data[d.pos:d.pos+4]) == "true" {
		b = true
		size = 4
	} else if d.pos+5 <= len(d.data) && string(d.data[d.pos:d.pos+5]) == "false" {
		b = false
		size = 5
	} else {
		return d.error(ErrorInvalidValue, "invalid boolean value")
	}

	d.pos += size

	v = indirect(v)

	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(b)
	case reflect.Interface:
		v.Set(reflect.ValueOf(b))
	default:
		return d.error(ErrorTypeMismatch, fmt.Sprintf("cannot decode bool into %s", v.Type()))
	}

	return nil
}

func (d *Decoder) decodeNull(v reflect.Value) error {
	if d.pos+4 > len(d.data) || string(d.data[d.pos:d.pos+4]) != "null" {
		return d.error(ErrorInvalidValue, "invalid null value")
	}

	d.pos += 4

	v = indirect(v)

	switch v.Kind() {
	case reflect.Interface, reflect.Ptr, reflect.Map, reflect.Slice:
		v.Set(reflect.Zero(v.Type()))
	}

	return nil
}

func (d *Decoder) decodeNumber(v reflect.Value) error {
	start := d.pos

	// Scan forward to find the end of the number
	end := start
	for end < len(d.data) {
		c := d.data[end]
		if (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
			end++
		} else {
			break
		}
	}

	numStr := d.data[start:end]
	d.pos = end

	v = indirect(v)

	// Handle UseNumber - return the raw number string as Number type
	if v.Kind() == reflect.Interface && d.config.UseNumber {
		v.Set(reflect.ValueOf(Number(string(numStr))))
		return nil
	}

	if isInteger(numStr) {
		i, err := simd.ParseIntSIMD(numStr)
		if err != nil {
			return WrapSIMDError(d.data, err, ErrorInvalidNumber)
		}

		switch v.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			v.SetInt(i)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			v.SetUint(uint64(i))
		case reflect.Float32, reflect.Float64:
			v.SetFloat(float64(i))
		case reflect.Interface:
			// UseInt64 mode or default: return int64 for integers
			v.Set(reflect.ValueOf(i))
		default:
			return d.error(ErrorTypeMismatch, fmt.Sprintf("cannot decode number into %s", v.Type()))
		}
		return nil
	}

	f, err := simd.ParseFloatSIMD(numStr)
	if err != nil {
		return WrapSIMDError(d.data, err, ErrorInvalidNumber)
	}

	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		v.SetFloat(f)
	case reflect.Interface:
		// For floats into interface{}, check UseInt64 - if set and value is whole number, use int64
		if d.config.UseInt64 && f == float64(int64(f)) {
			v.Set(reflect.ValueOf(int64(f)))
		} else {
			v.Set(reflect.ValueOf(f))
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(f))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(f))
	default:
		return d.error(ErrorTypeMismatch, fmt.Sprintf("cannot decode number into %s", v.Type()))
	}

	return nil
}

func (d *Decoder) skipValue() error {
	d.skipWhitespace()

	if d.pos >= len(d.data) {
		return d.error(ErrorInvalidJSON, "unexpected end of input")
	}

	c := d.data[d.pos]

	switch c {
	case '{':
		return d.skipObject()
	case '[':
		return d.skipArray()
	case '"':
		return d.skipString()
	case 't', 'f', 'n':
		return d.skipLiteral()
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return d.skipNumber()
	default:
		return d.error(ErrorInvalidJSON, fmt.Sprintf("unexpected character %q", c))
	}
}

func (d *Decoder) skipObject() error {
	if d.data[d.pos] != '{' {
		return d.error(ErrorInvalidJSON, "expected '{'")
	}
	d.pos++

	d.skipWhitespace()
	if d.pos < len(d.data) && d.data[d.pos] == '}' {
		d.pos++
		return nil
	}

	for {
		d.skipWhitespace()
		if err := d.skipString(); err != nil {
			return err
		}
		d.skipWhitespace()
		if d.pos >= len(d.data) || d.data[d.pos] != ':' {
			return d.error(ErrorInvalidJSON, "expected ':'")
		}
		d.pos++
		if err := d.skipValue(); err != nil {
			return err
		}
		d.skipWhitespace()
		if d.pos >= len(d.data) {
			return d.error(ErrorInvalidJSON, "unexpected end of object")
		}
		if d.data[d.pos] == '}' {
			d.pos++
			return nil
		}
		if d.data[d.pos] != ',' {
			return d.error(ErrorInvalidJSON, "expected ',' or '}'")
		}
		d.pos++
	}
}

func (d *Decoder) skipArray() error {
	if d.data[d.pos] != '[' {
		return d.error(ErrorInvalidJSON, "expected '['")
	}
	d.pos++

	d.skipWhitespace()
	if d.pos < len(d.data) && d.data[d.pos] == ']' {
		d.pos++
		return nil
	}

	for {
		if err := d.skipValue(); err != nil {
			return err
		}
		d.skipWhitespace()
		if d.pos >= len(d.data) {
			return d.error(ErrorInvalidJSON, "unexpected end of array")
		}
		if d.data[d.pos] == ']' {
			d.pos++
			return nil
		}
		if d.data[d.pos] != ',' {
			return d.error(ErrorInvalidJSON, "expected ',' or ']'")
		}
		d.pos++
	}
}

func (d *Decoder) skipString() error {
	if d.data[d.pos] != '"' {
		return d.error(ErrorInvalidString, "expected '\"'")
	}
	d.pos++

	end := simd.FindQuote(d.data, d.pos)
	if end < 0 {
		return d.error(ErrorInvalidString, "unterminated string")
	}

	d.pos = end + 1
	return nil
}

func (d *Decoder) skipLiteral() error {
	start := d.pos

	for d.pos < len(d.data) {
		c := d.data[d.pos]
		if c >= 'a' && c <= 'z' {
			d.pos++
		} else {
			break
		}
	}

	literal := string(d.data[start:d.pos])
	if literal != "true" && literal != "false" && literal != "null" {
		return d.error(ErrorInvalidValue, fmt.Sprintf("invalid literal %q", literal))
	}

	return nil
}

func (d *Decoder) skipNumber() error {
	end := simd.FindStructuralChar(d.data, d.pos, []byte{',', '}', ']', ' ', '\t', '\n', '\r'})
	if end < 0 {
		end = len(d.data)
	}
	d.pos = end
	return nil
}

func (d *Decoder) error(code ErrorCode, message string) error {
	return NewSyntaxError(d.data, d.pos, code, message)
}

func indirect(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	return v
}

func isInteger(b []byte) bool {
	for _, c := range b {
		if c == '.' || c == 'e' || c == 'E' {
			return false
		}
	}
	return true
}

func bytesToString(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}
