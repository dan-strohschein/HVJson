package hvjson

import (
	"bytes"
	"encoding/json"
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
