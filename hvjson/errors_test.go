package hvjson

import (
	"strings"
	"testing"
)

func TestSyntaxErrorFormatting(t *testing.T) {
	src := []byte(`{
  "name": "test",
  "value": invalid,
  "other": 123
}`)

	err := NewSyntaxError(src, 29, ErrorInvalidValue, "unexpected identifier")
	errStr := err.Error()

	if !strings.Contains(errStr, "invalid value") {
		t.Errorf("error should contain error code description")
	}
	if !strings.Contains(errStr, "position 29") {
		t.Errorf("error should contain position")
	}

	t.Logf("Error output:\n%s", errStr)
}

func TestUnmarshalErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"invalid JSON", `{invalid}`},
		{"unterminated string", `{"name": "test`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]interface{}
			err := Unmarshal([]byte(tt.input), &result)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			t.Logf("Error: %v", err)
		})
	}
}
