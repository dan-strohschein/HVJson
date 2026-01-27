package hvjson

import (
	"bytes"
	"testing"
)

func TestMarshalIndent(t *testing.T) {
	type Person struct {
		Name string   `json:"name"`
		Age  int      `json:"age"`
		Tags []string `json:"tags"`
	}

	data := Person{
		Name: "John",
		Age:  30,
		Tags: []string{"go", "json"},
	}

	result, err := MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent failed: %v", err)
	}

	// Check that it contains newlines (indented)
	if !bytes.Contains(result, []byte("\n")) {
		t.Errorf("MarshalIndent should contain newlines, got: %s", result)
	}

	// Check that it contains proper indentation
	if !bytes.Contains(result, []byte("  \"name\"")) {
		t.Errorf("MarshalIndent should have indented fields, got: %s", result)
	}

	t.Logf("MarshalIndent output:\n%s", result)
}

func TestFastMarshalIndent(t *testing.T) {
	type Person struct {
		Name string   `json:"name"`
		Age  int      `json:"age"`
		Tags []string `json:"tags"`
	}

	data := Person{
		Name: "Jane",
		Age:  25,
		Tags: []string{"fast", "indent"},
	}

	result, err := FastMarshalIndent(data, "", "    ")
	if err != nil {
		t.Fatalf("FastMarshalIndent failed: %v", err)
	}

	// Check that it contains newlines (indented)
	if !bytes.Contains(result, []byte("\n")) {
		t.Errorf("FastMarshalIndent should contain newlines, got: %s", result)
	}

	// Check 4-space indent
	if !bytes.Contains(result, []byte("    \"name\"")) {
		t.Errorf("FastMarshalIndent should have 4-space indented fields, got: %s", result)
	}

	t.Logf("FastMarshalIndent output:\n%s", result)
}

func TestUseNumber(t *testing.T) {
	jsonStr := `{"value": 12345678901234567890}`
	var m map[string]interface{}
	config := &Config{MaxDepth: 1000, UseNumber: true}
	err := UnmarshalWithConfig([]byte(jsonStr), &m, config)
	if err != nil {
		t.Fatalf("UnmarshalWithConfig failed: %v", err)
	}

	num, ok := m["value"].(Number)
	if !ok {
		t.Fatalf("Expected Number type, got %T", m["value"])
	}

	if num.String() != "12345678901234567890" {
		t.Errorf("Expected '12345678901234567890', got '%s'", num.String())
	}

	t.Logf("UseNumber: type=%T, value=%s", num, num.String())
}

func TestUseInt64(t *testing.T) {
	jsonStr := `{"int_val": 42, "float_val": 3.14}`
	var m map[string]interface{}
	config := &Config{MaxDepth: 1000, UseInt64: true}
	err := UnmarshalWithConfig([]byte(jsonStr), &m, config)
	if err != nil {
		t.Fatalf("UnmarshalWithConfig failed: %v", err)
	}

	// int_val should be int64
	intVal, ok := m["int_val"].(int64)
	if !ok {
		t.Fatalf("Expected int64 for int_val, got %T", m["int_val"])
	}
	if intVal != 42 {
		t.Errorf("Expected 42, got %d", intVal)
	}

	// float_val with UseInt64 - 3.14 is not a whole number so stays float64
	floatVal, ok := m["float_val"].(float64)
	if !ok {
		t.Fatalf("Expected float64 for float_val, got %T", m["float_val"])
	}
	if floatVal != 3.14 {
		t.Errorf("Expected 3.14, got %f", floatVal)
	}

	t.Logf("UseInt64: int_val=%T(%v), float_val=%T(%v)", 
		m["int_val"], m["int_val"], m["float_val"], m["float_val"])
}

func TestStreamEncoderSetIndent(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetIndent("", "  ")

	data := map[string]int{"a": 1, "b": 2}
	err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	result := buf.String()
	if !bytes.Contains([]byte(result), []byte("\n")) {
		t.Errorf("SetIndent should produce indented output, got: %s", result)
	}

	t.Logf("StreamEncoder.SetIndent output:\n%s", result)
}

func BenchmarkMarshalIndent(b *testing.B) {
	type Person struct {
		Name string   `json:"name"`
		Age  int      `json:"age"`
		Tags []string `json:"tags"`
	}

	data := Person{
		Name: "John",
		Age:  30,
		Tags: []string{"go", "json"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MarshalIndent(data, "", "  ")
	}
}

func BenchmarkFastMarshalIndent(b *testing.B) {
	type Person struct {
		Name string   `json:"name"`
		Age  int      `json:"age"`
		Tags []string `json:"tags"`
	}

	data := Person{
		Name: "John",
		Age:  30,
		Tags: []string{"go", "json"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = FastMarshalIndent(data, "", "  ")
	}
}

// TestDisallowUnknownFields tests the DisallowUnknownFields decoder option
func TestDisallowUnknownFields(t *testing.T) {
	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	// JSON with unknown field (all string values to avoid type issues)
	jsonWithUnknown := `{"name": "John", "age": 30, "extra": "ignored"}`
	jsonWithoutUnknown := `{"name": "Jane", "age": 25}`

	// Without DisallowUnknownFields - should succeed even with unknown fields
	var p1 Person
	err := Unmarshal([]byte(jsonWithUnknown), &p1)
	if err != nil {
		t.Errorf("Expected success without DisallowUnknownFields, got error: %v", err)
	} else {
		t.Logf("Without DisallowUnknownFields: name=%s, age=%d", p1.Name, p1.Age)
	}

	// Test using streaming decoder with DisallowUnknownFields
	var p2 Person
	dec := NewDecoder(bytes.NewReader([]byte(jsonWithUnknown)))
	dec.DisallowUnknownFields()
	err = dec.Decode(&p2)
	if err == nil {
		t.Errorf("Expected error with DisallowUnknownFields and unknown field, got success")
	} else {
		t.Logf("With DisallowUnknownFields (unknown field): got expected error: %v", err)
	}

	// JSON without unknown fields should succeed even with DisallowUnknownFields
	var p3 Person
	dec2 := NewDecoder(bytes.NewReader([]byte(jsonWithoutUnknown)))
	dec2.DisallowUnknownFields()
	err = dec2.Decode(&p3)
	if err != nil {
		t.Errorf("Expected success with DisallowUnknownFields but no unknown fields, got error: %v", err)
	} else {
		t.Logf("With DisallowUnknownFields (no unknown fields): name=%s, age=%d", p3.Name, p3.Age)
	}
}

// TestSetEscapeHTML tests the SetEscapeHTML encoder option
func TestSetEscapeHTML(t *testing.T) {
	type Data struct {
		HTML string `json:"html"`
	}

	data := Data{HTML: "<script>alert('xss')</script> & more > less <"}

	// Default behavior (EscapeHTML = true) should escape HTML chars
	result1, err := Marshal(data)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	// Should contain escaped HTML chars
	if bytes.Contains(result1, []byte("<")) || bytes.Contains(result1, []byte(">")) || bytes.Contains(result1, []byte("&")) {
		t.Errorf("Default should escape HTML chars, got: %s", result1)
	}
	if !bytes.Contains(result1, []byte("\\u003c")) {
		t.Errorf("Expected \\u003c in output, got: %s", result1)
	}
	t.Logf("With EscapeHTML=true (default): %s", result1)

	// StreamEncoder with SetEscapeHTML(false) should NOT escape HTML chars
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err = enc.Encode(data)
	if err != nil {
		t.Fatalf("StreamEncoder.Encode failed: %v", err)
	}
	result2 := buf.Bytes()
	// Should contain raw HTML chars
	if !bytes.Contains(result2, []byte("<")) || !bytes.Contains(result2, []byte(">")) || !bytes.Contains(result2, []byte("&")) {
		t.Errorf("SetEscapeHTML(false) should not escape HTML chars, got: %s", result2)
	}
	t.Logf("With SetEscapeHTML(false): %s", result2)
}

// TestOmitEmpty tests the omitempty tag in fast path
func TestOmitEmpty(t *testing.T) {
	type Data struct {
		Name    string   `json:"name"`
		Age     int      `json:"age,omitempty"`
		Score   float64  `json:"score,omitempty"`
		Active  bool     `json:"active,omitempty"`
		Tags    []string `json:"tags,omitempty"`
		Comment string   `json:"comment,omitempty"`
	}

	// All empty values should be omitted
	data := Data{Name: "John"} // All other fields are zero values

	result, err := FastMarshal(data)
	if err != nil {
		t.Fatalf("FastMarshal failed: %v", err)
	}

	// Should only contain "name"
	if bytes.Contains(result, []byte("age")) {
		t.Errorf("Expected 'age' to be omitted, got: %s", result)
	}
	if bytes.Contains(result, []byte("score")) {
		t.Errorf("Expected 'score' to be omitted, got: %s", result)
	}
	if bytes.Contains(result, []byte("active")) {
		t.Errorf("Expected 'active' to be omitted, got: %s", result)
	}
	if bytes.Contains(result, []byte("tags")) {
		t.Errorf("Expected 'tags' to be omitted, got: %s", result)
	}
	if bytes.Contains(result, []byte("comment")) {
		t.Errorf("Expected 'comment' to be omitted, got: %s", result)
	}
	t.Logf("With omitempty (empty values): %s", result)

	// With non-empty values, they should appear
	data2 := Data{
		Name:    "Jane",
		Age:     25,
		Score:   98.5,
		Active:  true,
		Tags:    []string{"fast"},
		Comment: "Good",
	}

	result2, err := FastMarshal(data2)
	if err != nil {
		t.Fatalf("FastMarshal failed: %v", err)
	}

	// All fields should be present
	if !bytes.Contains(result2, []byte("age")) {
		t.Errorf("Expected 'age' to be present, got: %s", result2)
	}
	if !bytes.Contains(result2, []byte("score")) {
		t.Errorf("Expected 'score' to be present, got: %s", result2)
	}
	t.Logf("With omitempty (non-empty values): %s", result2)
}
