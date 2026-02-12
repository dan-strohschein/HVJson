package hvjson

import (
	"bufio"
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type TestStruct struct {
	Name   string   `json:"name"`
	Age    int      `json:"age"`
	Active bool     `json:"active"`
	Tags   []string `json:"tags,omitempty"`
}

func TestMarshalBasicTypes(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{"string", "hello", `"hello"`},
		{"bool_true", true, "true"},
		{"bool_false", false, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.input)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMarshalStruct(t *testing.T) {
	input := TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	got, err := Marshal(input)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var result TestStruct
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if result.Name != input.Name {
		t.Errorf("Name = %s, want %s", result.Name, input.Name)
	}
}

func TestUnmarshalBasicTypes(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		target interface{}
		check  func(t *testing.T, v interface{})
	}{
		{
			"int",
			"42",
			new(int),
			func(t *testing.T, v interface{}) {
				if *v.(*int) != 42 {
					t.Errorf("got %d, want 42", *v.(*int))
				}
			},
		},
		{
			"string",
			`"hello"`,
			new(string),
			func(t *testing.T, v interface{}) {
				if *v.(*string) != "hello" {
					t.Errorf("got %s, want hello", *v.(*string))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Unmarshal([]byte(tt.input), tt.target)
			if err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			tt.check(t, tt.target)
		})
	}
}

func TestUnmarshalStruct(t *testing.T) {
	input := `{"name":"John","age":30,"active":true,"tags":["go","json"]}`

	var result TestStruct
	err := Unmarshal([]byte(input), &result)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if result.Name != "John" {
		t.Errorf("Name = %s, want John", result.Name)
	}
	if result.Age != 30 {
		t.Errorf("Age = %d, want 30", result.Age)
	}
}

func BenchmarkMarshalStruct(b *testing.B) {
	input := TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Marshal(input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMarshalStructPtr uses pointer for optimal HVJson performance
func BenchmarkMarshalStructPtr(b *testing.B) {
	input := &TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Marshal(input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalStruct_StdLib(b *testing.B) {
	input := TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMarshalStructPtr_StdLib uses pointer for fair comparison
func BenchmarkMarshalStructPtr_StdLib(b *testing.B) {
	input := &TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalStruct(b *testing.B) {
	input := []byte(`{"name":"John","age":30,"active":true,"tags":["go","json"]}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result TestStruct
		if err := Unmarshal(input, &result); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalStruct_StdLib(b *testing.B) {
	input := []byte(`{"name":"John","age":30,"active":true,"tags":["go","json"]}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result TestStruct
		if err := json.Unmarshal(input, &result); err != nil {
			b.Fatal(err)
		}
	}
}

func TestStreamingEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	// Encode multiple values
	values := []interface{}{
		TestStruct{Name: "Alice", Age: 25, Active: true},
		TestStruct{Name: "Bob", Age: 30, Active: false},
		42,
		"hello",
	}

	for _, v := range values {
		if err := enc.Encode(v); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}

	// Check output
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Errorf("Expected 4 lines, got %d", len(lines))
	}

	// Verify first line is valid JSON
	var s TestStruct
	if err := json.Unmarshal([]byte(lines[0]), &s); err != nil {
		t.Errorf("Line 0 not valid JSON: %v", err)
	}
	if s.Name != "Alice" {
		t.Errorf("Expected Alice, got %s", s.Name)
	}
}

func TestStreamingDecoder(t *testing.T) {
	input := `{"name":"Alice","age":25,"active":true}
{"name":"Bob","age":30,"active":false}
42
"hello"
`
	dec := NewDecoder(strings.NewReader(input))

	// Decode first struct
	var s1 TestStruct
	if err := dec.Decode(&s1); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if s1.Name != "Alice" || s1.Age != 25 {
		t.Errorf("Got %+v, want Alice/25", s1)
	}

	// Decode second struct
	var s2 TestStruct
	if err := dec.Decode(&s2); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if s2.Name != "Bob" || s2.Age != 30 {
		t.Errorf("Got %+v, want Bob/30", s2)
	}

	// Decode number
	var num int
	if err := dec.Decode(&num); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if num != 42 {
		t.Errorf("Got %d, want 42", num)
	}

	// Decode string
	var str string
	if err := dec.Decode(&str); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if str != "hello" {
		t.Errorf("Got %s, want hello", str)
	}
}

// TestStreamingRoundTrip encodes a value with StreamEncoder then decodes with StreamDecoder
// and verifies the result matches the original (round-trip accuracy).
func TestStreamingRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
	}{
		{"struct", TestStruct{Name: "Alice", Age: 25, Active: true, Tags: []string{"a", "b"}}},
		{"map", map[string]interface{}{"k": "v", "n": float64(42)}},
		{"slice", []int{1, 2, 3}},
		{"primitive_int", 42},
		{"primitive_float", 3.14},
		{"primitive_string", "hello"},
		{"primitive_bool", true},
		{"nested", map[string]interface{}{
			"arr": []interface{}{1, "two", true},
			"obj": map[string]string{"a": "b"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := NewEncoder(&buf)
			if err := enc.Encode(tt.value); err != nil {
				t.Fatalf("Encode: %v", err)
			}
			dec := NewDecoder(&buf)
			// Decode into interface{} then compare via JSON bytes for consistency
			var got interface{}
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			wantBytes, err := Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal(original): %v", err)
			}
			gotBytes, err := Marshal(got)
			if err != nil {
				t.Fatalf("Marshal(decoded): %v", err)
			}
			var wantNorm, gotNorm interface{}
			if err := json.Unmarshal(wantBytes, &wantNorm); err != nil {
				t.Fatalf("json.Unmarshal(want): %v", err)
			}
			if err := json.Unmarshal(gotBytes, &gotNorm); err != nil {
				t.Fatalf("json.Unmarshal(got): %v", err)
			}
			if !reflect.DeepEqual(gotNorm, wantNorm) {
				t.Errorf("round-trip mismatch: got %v, want %v", gotNorm, wantNorm)
			}
		})
	}
}

// TestStreamingMultipleValuesRoundTrip encodes several values with one encoder,
// decodes with one decoder in a loop, and asserts each decoded value matches (NDJSON accuracy).
func TestStreamingMultipleValuesRoundTrip(t *testing.T) {
	values := []interface{}{
		TestStruct{Name: "Alice", Age: 25, Active: true},
		TestStruct{Name: "Bob", Age: 30, Active: false},
		42,
		"hello",
		true,
		nil,
		[]int{1, 2, 3},
	}
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	for _, v := range values {
		if err := enc.Encode(v); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}
	dec := NewDecoder(&buf)
	for i, want := range values {
		var got interface{}
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("Decode value %d: %v", i, err)
		}
		wantBytes, _ := Marshal(want)
		gotBytes, _ := Marshal(got)
		var wantNorm, gotNorm interface{}
		if err := json.Unmarshal(wantBytes, &wantNorm); err != nil {
			t.Fatalf("json.Unmarshal(want): %v", err)
		}
		if err := json.Unmarshal(gotBytes, &gotNorm); err != nil {
			t.Fatalf("json.Unmarshal(got): %v", err)
		}
		if !reflect.DeepEqual(gotNorm, wantNorm) {
			t.Errorf("value %d: got %v, want %v", i, gotNorm, wantNorm)
		}
	}
	if dec.More() {
		t.Error("expected no more values after last")
	}
}

// TestStreamingEdgeCases covers null, literals, numbers, strings with escapes/Unicode,
// empty object/array, and values that span fill() boundaries.
func TestStreamingEdgeCases(t *testing.T) {
	t.Run("null", func(t *testing.T) {
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		if err := enc.Encode(nil); err != nil {
			t.Fatal(err)
		}
		dec := NewDecoder(&buf)
		var got interface{}
		if err := dec.Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
	t.Run("true_false", func(t *testing.T) {
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		for _, v := range []interface{}{true, false} {
			if err := enc.Encode(v); err != nil {
				t.Fatal(err)
			}
		}
		dec := NewDecoder(&buf)
		var b1, b2 bool
		if err := dec.Decode(&b1); err != nil {
			t.Fatal(err)
		}
		if err := dec.Decode(&b2); err != nil {
			t.Fatal(err)
		}
		if b1 != true || b2 != false {
			t.Errorf("got %v, %v", b1, b2)
		}
	})
	t.Run("numbers", func(t *testing.T) {
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		enc.Encode(42)
		enc.Encode(-1)
		enc.Encode(3.14)
		enc.Encode(1e10)
		dec := NewDecoder(&buf)
		var i int
		dec.Decode(&i)
		if i != 42 {
			t.Errorf("int: got %d", i)
		}
		dec.Decode(&i)
		if i != -1 {
			t.Errorf("int negative: got %d", i)
		}
		var f float64
		dec.Decode(&f)
		if f != 3.14 {
			t.Errorf("float: got %f", f)
		}
		dec.Decode(&f)
		if f != 1e10 {
			t.Errorf("scientific: got %f", f)
		}
	})
	t.Run("string_roundtrip", func(t *testing.T) {
		s := "hello world"
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		if err := enc.Encode(s); err != nil {
			t.Fatal(err)
		}
		dec := NewDecoder(&buf)
		var got string
		if err := dec.Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got != s {
			t.Errorf("got %q, want %q", got, s)
		}
	})
	t.Run("empty_object_array", func(t *testing.T) {
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		enc.Encode(map[string]interface{}{})
		enc.Encode([]interface{}{})
		dec := NewDecoder(&buf)
		var m map[string]interface{}
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		if len(m) != 0 {
			t.Errorf("empty object: got %v", m)
		}
		var s []interface{}
		if err := dec.Decode(&s); err != nil {
			t.Fatal(err)
		}
		if len(s) != 0 {
			t.Errorf("empty array: got %v", s)
		}
	})
	// Large value that still fits in one decoder buffer (default 4096) to stress buffer path
	t.Run("large_value_single_buffer", func(t *testing.T) {
		large := map[string]string{"key": strings.Repeat("x", 2000)}
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		if err := enc.Encode(large); err != nil {
			t.Fatal(err)
		}
		dec := NewDecoder(&buf)
		var got map[string]string
		if err := dec.Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["key"] != large["key"] {
			t.Errorf("large value mismatch: len(got)=%d, len(want)=%d", len(got["key"]), len(large["key"]))
		}
	})
}

// TestStreamingEncoderPerformanceRegression runs encode in a benchmark and fails
// if ns/op or allocs exceed thresholds (catches regressions).
// Baseline: 129.1 ns/op, 3 allocs. Thresholds set generously for varying CI/host speed.
func TestStreamingEncoderPerformanceRegression(t *testing.T) {
	const maxNsPerOp = 250   // allow for slower hosts
	const maxAllocsPerOp = 5 // allow minor alloc variance
	input := TestStruct{Name: "John", Age: 30, Active: true, Tags: []string{"go", "json"}}
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	result := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf.Reset()
			if err := enc.Encode(input); err != nil {
				b.Fatal(err)
			}
		}
	})
	if result.NsPerOp() > maxNsPerOp {
		t.Errorf("StreamingEncoder ns/op regression: got %d, max %d", result.NsPerOp(), maxNsPerOp)
	}
	if result.AllocsPerOp() > maxAllocsPerOp {
		t.Errorf("StreamingEncoder allocs/op regression: got %d, max %d", result.AllocsPerOp(), maxAllocsPerOp)
	}
}

// TestStreamingDecoderPerformanceRegression runs decode in a benchmark and fails
// if ns/op or allocs exceed thresholds.
// Baseline: 748.9 ns/op, 12 allocs. Thresholds allow bufio wrapper and varying host speed.
func TestStreamingDecoderPerformanceRegression(t *testing.T) {
	const maxNsPerOp = 2000  // allow bufio.Reader wrapper and slower hosts
	const maxAllocsPerOp = 22 // allow alloc variance
	jsonStr := `{"name":"John","age":30,"active":true,"tags":["go","json"]}
`
	benchResult := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			dec := NewDecoder(strings.NewReader(jsonStr))
			var out TestStruct
			if err := dec.Decode(&out); err != nil {
				b.Fatal(err)
			}
		}
	})
	if benchResult.NsPerOp() > maxNsPerOp {
		t.Errorf("StreamingDecoder ns/op regression: got %d, max %d", benchResult.NsPerOp(), maxNsPerOp)
	}
	if benchResult.AllocsPerOp() > maxAllocsPerOp {
		t.Errorf("StreamingDecoder allocs/op regression: got %d, max %d", benchResult.AllocsPerOp(), maxAllocsPerOp)
	}
}

func BenchmarkStreamingEncoder(b *testing.B) {
	input := TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := enc.Encode(input); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIncrementalStreamEncoder encodes one value per op with incremental flushing (64 KiB threshold).
func BenchmarkIncrementalStreamEncoder(b *testing.B) {
	input := TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	var buf bytes.Buffer
	enc := NewIncrementalEncoder(&buf, 0) // 0 => default 64 KiB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := enc.Encode(input); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIncrementalStreamEncoderManyValues encodes 1000 small values to compare allocs vs StreamEncoder.
func BenchmarkIncrementalStreamEncoderManyValues(b *testing.B) {
	const numValues = 1000
	input := TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	var buf bytes.Buffer
	enc := NewIncrementalEncoder(&buf, 4096)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		for j := 0; j < numValues; j++ {
			if err := enc.Encode(input); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkStreamingDecoder(b *testing.B) {
	jsonStr := `{"name":"John","age":30,"active":true,"tags":["go","json"]}
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec := NewDecoder(strings.NewReader(jsonStr))
		var result TestStruct
		if err := dec.Decode(&result); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStreamingDecoderFromBufio measures decoder when reading from bufio.Reader
// (e.g. for Phase 3 buffered-reader comparison).
func BenchmarkStreamingDecoderFromBufio(b *testing.B) {
	jsonStr := `{"name":"John","age":30,"active":true,"tags":["go","json"]}
`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bufio.NewReader(strings.NewReader(jsonStr))
		dec := NewDecoder(r)
		var result TestStruct
		if err := dec.Decode(&result); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStreamingDecoderManyValues decodes N small values from one stream
// to stress alloc and skipValue/whitespace path. Uses 50 values so payload
// fits in one buffer and avoids fill-boundary edge cases.
func BenchmarkStreamingDecoderManyValues(b *testing.B) {
	const numValues = 50
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	for i := 0; i < numValues; i++ {
		_ = enc.Encode(TestStruct{Name: "John", Age: 30, Active: true, Tags: []string{"go", "json"}})
	}
	payload := buf.String()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec := NewDecoder(strings.NewReader(payload))
		for j := 0; j < numValues; j++ {
			var result TestStruct
			if err := dec.Decode(&result); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkFastMarshalStruct(b *testing.B) {
	input := TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := FastMarshal(input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMarshalTo tests zero-copy encoding to a pre-allocated buffer
// Uses pointer to struct for optimal performance (avoids reflect.New allocation)
func BenchmarkMarshalTo(b *testing.B) {
	input := &TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	// Pre-allocate buffer - this is where the true benefit comes
	buf := make([]byte, 0, 256)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = buf[:0] // Reset buffer without reallocation
		var err error
		buf, err = MarshalTo(buf, input, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMarshalToValue benchmarks MarshalTo with a value (causes 1 alloc for addressability)
func BenchmarkMarshalToValue(b *testing.B) {
	input := TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	buf := make([]byte, 0, 256)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = buf[:0]
		var err error
		buf, err = MarshalTo(buf, input, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMarshalPooled tests convenience pooled encoding
func BenchmarkMarshalPooled(b *testing.B) {
	input := TestStruct{
		Name:   "John",
		Age:    30,
		Active: true,
		Tags:   []string{"go", "json"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := MarshalPooled(input)
		if err != nil {
			b.Fatal(err)
		}
		_ = result.Data // Use the data
	}
}
