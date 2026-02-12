package hvjson

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestStreamingWriteObjectMatchesEncode verifies that building an object via Write* API
// produces equivalent JSON to Encode of an equivalent Go value (decode both and compare).
func TestStreamingWriteObjectMatchesEncode(t *testing.T) {
	var writeBuf bytes.Buffer
	enc := NewIncrementalEncoder(&writeBuf, 4096)
	if err := enc.WriteObjectStart(); err != nil {
		t.Fatalf("WriteObjectStart: %v", err)
	}
	if err := enc.WriteObjectField("name"); err != nil {
		t.Fatalf("WriteObjectField: %v", err)
	}
	if err := enc.WriteString("Alice"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := enc.WriteObjectField("age"); err != nil {
		t.Fatalf("WriteObjectField: %v", err)
	}
	if err := enc.WriteInt64(25); err != nil {
		t.Fatalf("WriteInt64: %v", err)
	}
	if err := enc.WriteObjectField("active"); err != nil {
		t.Fatalf("WriteObjectField: %v", err)
	}
	if err := enc.WriteBool(true); err != nil {
		t.Fatalf("WriteBool: %v", err)
	}
	if err := enc.WriteObjectEnd(); err != nil {
		t.Fatalf("WriteObjectEnd: %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	writeOut := writeBuf.Bytes()

	// Equivalent value encoded with Encode
	var encodeBuf bytes.Buffer
	enc2 := NewIncrementalEncoder(&encodeBuf, 4096)
	equivalent := map[string]interface{}{"name": "Alice", "age": float64(25), "active": true}
	if err := enc2.Encode(equivalent); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	encodeOut := encodeBuf.Bytes()
	encodeOut = bytes.TrimSuffix(encodeOut, []byte("\n"))

	// Decode both and compare (map key order may differ, so compare decoded content)
	var writeDec, encodeDec interface{}
	if err := json.Unmarshal(writeOut, &writeDec); err != nil {
		t.Fatalf("json.Unmarshal(writeOut): %v", err)
	}
	if err := json.Unmarshal(encodeOut, &encodeDec); err != nil {
		t.Fatalf("json.Unmarshal(encodeOut): %v", err)
	}
	writeJSON, _ := json.Marshal(writeDec)
	encodeJSON, _ := json.Marshal(encodeDec)
	if !bytes.Equal(writeJSON, encodeJSON) {
		t.Errorf("Write* output decoded != Encode output decoded: write %s vs encode %s", writeJSON, encodeJSON)
	}
}

// TestStreamingWriteArrayMatchesEncode verifies that building an array via Write* API
// produces equivalent JSON to Encode of an equivalent slice.
func TestStreamingWriteArrayMatchesEncode(t *testing.T) {
	var writeBuf bytes.Buffer
	enc := NewIncrementalEncoder(&writeBuf, 4096)
	if err := enc.WriteArrayStart(); err != nil {
		t.Fatalf("WriteArrayStart: %v", err)
	}
	if err := enc.WriteInt64(1); err != nil {
		t.Fatalf("WriteInt64: %v", err)
	}
	if err := enc.WriteString("two"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := enc.WriteBool(true); err != nil {
		t.Fatalf("WriteBool: %v", err)
	}
	if err := enc.WriteNull(); err != nil {
		t.Fatalf("WriteNull: %v", err)
	}
	if err := enc.WriteArrayEnd(); err != nil {
		t.Fatalf("WriteArrayEnd: %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	writeOut := writeBuf.Bytes()

	equivalent := []interface{}{float64(1), "two", true, nil}
	var encodeBuf bytes.Buffer
	enc2 := NewEncoder(&encodeBuf)
	if err := enc2.Encode(equivalent); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	encodeOut := bytes.TrimSuffix(encodeBuf.Bytes(), []byte("\n"))

	var writeDec, encodeDec interface{}
	if err := json.Unmarshal(writeOut, &writeDec); err != nil {
		t.Fatalf("json.Unmarshal(writeOut): %v", err)
	}
	if err := json.Unmarshal(encodeOut, &encodeDec); err != nil {
		t.Fatalf("json.Unmarshal(encodeOut): %v", err)
	}
	writeJSON, _ := json.Marshal(writeDec)
	encodeJSON, _ := json.Marshal(encodeDec)
	if !bytes.Equal(writeJSON, encodeJSON) {
		t.Errorf("Write* array decoded != Encode: write %s vs encode %s", writeJSON, encodeJSON)
	}
}

// TestStreamingWriteIncrementalFlush builds a value with one large string so the buffer
// exceeds the flush threshold and verifies bytes are flushed to the writer.
func TestStreamingWriteIncrementalFlush(t *testing.T) {
	threshold := 256
	var buf bytes.Buffer
	enc := NewIncrementalEncoder(&buf, threshold)
	if err := enc.WriteObjectStart(); err != nil {
		t.Fatalf("WriteObjectStart: %v", err)
	}
	if err := enc.WriteObjectField("big"); err != nil {
		t.Fatalf("WriteObjectField: %v", err)
	}
	large := strings.Repeat("x", threshold*2)
	if err := enc.WriteString(large); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := enc.WriteObjectEnd(); err != nil {
		t.Fatalf("WriteObjectEnd: %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	out := buf.Bytes()
	if len(out) < len(large) {
		t.Errorf("expected at least %d bytes written (large string), got %d", len(large), len(out))
	}
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if m["big"] != large {
		t.Errorf("decoded big field length %d, expected %d", len(m["big"]), len(large))
	}
}

// TestStreamingWriteSetIndent verifies that SetIndent produces indented output from the Write* API.
func TestStreamingWriteSetIndent(t *testing.T) {
	var buf bytes.Buffer
	enc := NewIncrementalEncoder(&buf, 4096)
	enc.SetIndent("", "  ")
	if err := enc.WriteObjectStart(); err != nil {
		t.Fatalf("WriteObjectStart: %v", err)
	}
	if err := enc.WriteObjectField("a"); err != nil {
		t.Fatalf("WriteObjectField: %v", err)
	}
	if err := enc.WriteInt64(1); err != nil {
		t.Fatalf("WriteInt64: %v", err)
	}
	if err := enc.WriteObjectEnd(); err != nil {
		t.Fatalf("WriteObjectEnd: %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "\n") {
		t.Errorf("SetIndent should produce newlines, got: %s", out)
	}
	if !strings.Contains(out, "  \"a\"") {
		t.Errorf("SetIndent should produce indented key, got: %s", out)
	}
}

// TestStreamingWriteSetEscapeHTML verifies that SetEscapeHTML(false) does not escape <, >, & in WriteString.
func TestStreamingWriteSetEscapeHTML(t *testing.T) {
	var buf bytes.Buffer
	enc := NewIncrementalEncoder(&buf, 4096)
	enc.SetEscapeHTML(false)
	if err := enc.WriteArrayStart(); err != nil {
		t.Fatalf("WriteArrayStart: %v", err)
	}
	if err := enc.WriteString("<script>&</script>"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := enc.WriteArrayEnd(); err != nil {
		t.Fatalf("WriteArrayEnd: %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "\\u003c") || strings.Contains(out, "\\u0026") {
		t.Errorf("SetEscapeHTML(false) should not escape < or &, got: %s", out)
	}
}

// TestStreamingWriteInvalidState verifies error cases: WriteObjectEnd with empty stack, Encode during write session.
func TestStreamingWriteInvalidState(t *testing.T) {
	// WriteObjectEnd with empty stack
	var buf1 bytes.Buffer
	enc1 := NewIncrementalEncoder(&buf1, 4096)
	if err := enc1.WriteObjectEnd(); err != ErrInvalidWriteState {
		t.Errorf("WriteObjectEnd with empty stack: want ErrInvalidWriteState, got %v", err)
	}

	// Encode during write session (use fresh encoder)
	var buf2 bytes.Buffer
	enc2 := NewIncrementalEncoder(&buf2, 4096)
	if err := enc2.WriteObjectStart(); err != nil {
		t.Fatalf("WriteObjectStart: %v", err)
	}
	if err := enc2.Encode(42); err != ErrInvalidWriteState {
		t.Errorf("Encode during write session: want ErrInvalidWriteState, got %v", err)
	}
}

// TestStreamingWriteMultipleValues writes two consecutive Write* trees (two objects), flushes, and decodes two values (NDJSON).
func TestStreamingWriteMultipleValues(t *testing.T) {
	var buf bytes.Buffer
	enc := NewIncrementalEncoder(&buf, 4096)

	// First value
	if err := enc.WriteObjectStart(); err != nil {
		t.Fatalf("WriteObjectStart 1: %v", err)
	}
	if err := enc.WriteObjectField("id"); err != nil {
		t.Fatalf("WriteObjectField: %v", err)
	}
	if err := enc.WriteInt64(1); err != nil {
		t.Fatalf("WriteInt64: %v", err)
	}
	if err := enc.WriteObjectEnd(); err != nil {
		t.Fatalf("WriteObjectEnd 1: %v", err)
	}

	// Second value (newline written automatically before it)
	if err := enc.WriteObjectStart(); err != nil {
		t.Fatalf("WriteObjectStart 2: %v", err)
	}
	if err := enc.WriteObjectField("id"); err != nil {
		t.Fatalf("WriteObjectField: %v", err)
	}
	if err := enc.WriteInt64(2); err != nil {
		t.Fatalf("WriteInt64: %v", err)
	}
	if err := enc.WriteObjectEnd(); err != nil {
		t.Fatalf("WriteObjectEnd 2: %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Decode two NDJSON values (output may be compact or indented depending on config)
	dec := NewDecoder(strings.NewReader(buf.String()))
	var m1, m2 map[string]interface{}
	if err := dec.Decode(&m1); err != nil {
		t.Fatalf("decode value 1: %v", err)
	}
	if err := dec.Decode(&m2); err != nil {
		t.Fatalf("decode value 2: %v", err)
	}
	id1, id2 := m1["id"], m2["id"]
	var n1, n2 int64
	switch v := id1.(type) {
	case float64:
		n1 = int64(v)
	case int64:
		n1 = v
	default:
		t.Fatalf("id type %T", id1)
	}
	switch v := id2.(type) {
	case float64:
		n2 = int64(v)
	case int64:
		n2 = v
	default:
		t.Fatalf("id type %T", id2)
	}
	if n1 != 1 || n2 != 2 {
		t.Errorf("expected id 1 and 2, got %d and %d", n1, n2)
	}
}

// TestStreamingWriteValueTypes exercises WriteFloat64, WriteFloat32, WriteUint64, and WriteValue.
func TestStreamingWriteValueTypes(t *testing.T) {
	var buf bytes.Buffer
	enc := NewIncrementalEncoder(&buf, 4096)
	if err := enc.WriteObjectStart(); err != nil {
		t.Fatalf("WriteObjectStart: %v", err)
	}
	if err := enc.WriteObjectField("f64"); err != nil {
		t.Fatalf("WriteObjectField: %v", err)
	}
	if err := enc.WriteFloat64(3.14); err != nil {
		t.Fatalf("WriteFloat64: %v", err)
	}
	if err := enc.WriteObjectField("f32"); err != nil {
		t.Fatalf("WriteObjectField: %v", err)
	}
	if err := enc.WriteFloat32(2.5); err != nil {
		t.Fatalf("WriteFloat32: %v", err)
	}
	if err := enc.WriteObjectField("u64"); err != nil {
		t.Fatalf("WriteObjectField: %v", err)
	}
	if err := enc.WriteUint64(100); err != nil {
		t.Fatalf("WriteUint64: %v", err)
	}
	if err := enc.WriteObjectField("nested"); err != nil {
		t.Fatalf("WriteObjectField: %v", err)
	}
	if err := enc.WriteValue(map[string]int{"x": 1}); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}
	if err := enc.WriteObjectEnd(); err != nil {
		t.Fatalf("WriteObjectEnd: %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m["f64"].(float64) != 3.14 || m["u64"].(float64) != 100 {
		t.Errorf("unexpected values: %v", m)
	}
	nested, ok := m["nested"].(map[string]interface{})
	if !ok || nested["x"].(float64) != 1 {
		t.Errorf("unexpected nested: %v", m["nested"])
	}
}

// TestStreamingWriteValueWithoutSession returns ErrInvalidWriteState when a value writer is called without an active structure.
func TestStreamingWriteValueWithoutSession(t *testing.T) {
	var buf bytes.Buffer
	enc := NewIncrementalEncoder(&buf, 4096)
	if err := enc.WriteString("hello"); err != ErrInvalidWriteState {
		t.Errorf("WriteString without session: want ErrInvalidWriteState, got %v", err)
	}
}
