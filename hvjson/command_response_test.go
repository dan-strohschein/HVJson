package hvjson

import (
	"fmt"
	"testing"
)

// Mock Document type since models.Document is not available in this project
type Document struct {
	ID    string                 `json:"id"`
	Data  map[string]interface{} `json:"data"`
	Score float64                `json:"score,omitempty"`
}

type CommandResponse struct {
	ResultCount     int
	Result          interface{}
	ExecutionTimeMS float64
	Error           *string `json:"error,omitempty"`
	TimeoutOccurred bool    `json:"timeout_occurred,omitempty"`

	PooledMaps []map[string]interface{} `json:"-"`

	StreamFields []string `json:"-"`

	PooledDocuments []*Document `json:"-"`
}

func TestCommandResponse_MarshalUnmarshal(t *testing.T) {
	// Create test data with random information
	errorMsg := "test error message"

	original := CommandResponse{
		ResultCount: 42,
		Result: map[string]interface{}{
			"status":  "success",
			"count":   100,
			"message": "Operation completed",
			"data": []interface{}{
				"item1",
				"item2",
				map[string]interface{}{
					"nested": "value",
					"number": 3.14,
				},
			},
		},
		ExecutionTimeMS: 125.75,
		Error:           &errorMsg,
		TimeoutOccurred: false,

		// These fields should NOT appear in JSON due to json:"-" tag
		PooledMaps: []map[string]interface{}{
			{"key1": "value1"},
			{"key2": "value2"},
		},
		StreamFields: []string{"field1", "field2", "field3"},
		PooledDocuments: []*Document{
			{ID: "doc1", Data: map[string]interface{}{"title": "Document 1"}, Score: 0.95},
			{ID: "doc2", Data: map[string]interface{}{"title": "Document 2"}, Score: 0.87},
		},
	}

	// Marshal to JSON using pointer
	jsonData, err := Marshal(&original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Make a copy of the data to prevent any buffer reuse issues
	jsonDataCopy := make([]byte, len(jsonData))
	copy(jsonDataCopy, jsonData)

	// Print the JSON string to console
	jsonString := string(jsonDataCopy)
	fmt.Println("\n=== Marshalled JSON ===")
	fmt.Println(jsonString)
	fmt.Println("======================")

	// Pretty print with indentation
	jsonDataIndented, err := MarshalIndent(&original, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent failed: %v", err)
	}
	fmt.Println("=== Pretty JSON ===")
	fmt.Println(string(jsonDataIndented))
	fmt.Println("===================")

	// Unmarshal back to verify round-trip
	var decoded CommandResponse
	err = Unmarshal(jsonDataCopy, &decoded)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify the data
	if decoded.ResultCount != original.ResultCount {
		t.Errorf("ResultCount mismatch: got %d, want %d", decoded.ResultCount, original.ResultCount)
	}

	if decoded.ExecutionTimeMS != original.ExecutionTimeMS {
		t.Errorf("ExecutionTimeMS mismatch: got %f, want %f", decoded.ExecutionTimeMS, original.ExecutionTimeMS)
	}

	if decoded.TimeoutOccurred != original.TimeoutOccurred {
		t.Errorf("TimeoutOccurred mismatch: got %v, want %v", decoded.TimeoutOccurred, original.TimeoutOccurred)
	}

	if decoded.Error == nil {
		t.Error("Error should not be nil")
	} else if *decoded.Error != *original.Error {
		t.Errorf("Error mismatch: got %s, want %s", *decoded.Error, *original.Error)
	}

	// Verify Result (as map)
	if decoded.Result == nil {
		t.Error("Result should not be nil")
	} else {
		resultMap, ok := decoded.Result.(map[string]interface{})
		if !ok {
			t.Errorf("Result should be map[string]interface{}, got %T", decoded.Result)
		} else {
			if resultMap["status"] != "success" {
				t.Errorf("Result.status mismatch: got %v, want 'success'", resultMap["status"])
			}
		}
	}

	// Verify json:"-" fields were NOT marshalled/unmarshalled
	if decoded.PooledMaps != nil {
		t.Error("PooledMaps should be nil (not marshalled)")
	}
	if decoded.StreamFields != nil {
		t.Error("StreamFields should be nil (not marshalled)")
	}
	if decoded.PooledDocuments != nil {
		t.Error("PooledDocuments should be nil (not marshalled)")
	}

	fmt.Println("✅ All marshalling/unmarshalling tests passed!")
}

func TestCommandResponse_WithNilError(t *testing.T) {
	// Test with nil error (omitempty should exclude it)
	original := CommandResponse{
		ResultCount:     10,
		Result:          "simple result",
		ExecutionTimeMS: 50.25,
		Error:           nil, // Should be omitted
		TimeoutOccurred: false,
	}

	jsonData, err := Marshal(&original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	jsonString := string(jsonData)
	fmt.Println("\n=== JSON with nil Error (should omit 'error' field) ===")
	fmt.Println(jsonString)
	fmt.Println("========================================================")

	// Verify 'error' field is not in JSON
	if contains(jsonString, "\"error\"") {
		t.Error("JSON should not contain 'error' field when it's nil (omitempty)")
	}

	// Unmarshal and verify
	var decoded CommandResponse
	if err := Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Error != nil {
		t.Error("Error should be nil after unmarshalling omitted field")
	}
}

func TestCommandResponse_TimeoutOccurred(t *testing.T) {
	// Test with timeout occurred
	original := CommandResponse{
		ResultCount:     0,
		Result:          nil,
		ExecutionTimeMS: 5000.0,
		Error:           nil,
		TimeoutOccurred: true, // Should be included
	}

	jsonData, err := Marshal(&original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	jsonString := string(jsonData)
	fmt.Println("\n=== JSON with TimeoutOccurred=true ===")
	fmt.Println(jsonString)
	fmt.Println("======================================")

	// Verify timeout_occurred is in JSON
	if !contains(jsonString, "\"timeout_occurred\"") {
		t.Error("JSON should contain 'timeout_occurred' field when true")
	}

	// Unmarshal and verify
	var decoded CommandResponse
	if err := Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !decoded.TimeoutOccurred {
		t.Error("TimeoutOccurred should be true after unmarshalling")
	}
}

func TestCommandResponse_ComplexResult(t *testing.T) {
	// Test with various result types
	testCases := []struct {
		name   string
		result interface{}
	}{
		{
			name:   "String result",
			result: "simple string result",
		},
		{
			name:   "Number result",
			result: 12345,
		},
		{
			name: "Array result",
			result: []interface{}{
				"item1",
				42,
				3.14,
				true,
				map[string]interface{}{"nested": "data"},
			},
		},
		{
			name: "Nested map result",
			result: map[string]interface{}{
				"users": []interface{}{
					map[string]interface{}{"id": 1, "name": "Alice"},
					map[string]interface{}{"id": 2, "name": "Bob"},
				},
				"metadata": map[string]interface{}{
					"total": 2,
					"page":  1,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			original := CommandResponse{
				ResultCount:     1,
				Result:          tc.result,
				ExecutionTimeMS: 10.5,
			}

			jsonData, err := Marshal(&original)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			fmt.Printf("\n=== %s ===\n", tc.name)
			fmt.Println(string(jsonData))
			fmt.Println()

			// Verify round-trip
			var decoded CommandResponse
			if err := Unmarshal(jsonData, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.Result == nil && tc.result != nil {
				t.Error("Result should not be nil after unmarshalling")
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) &&
			(func() bool {
				for i := 0; i <= len(s)-len(substr); i++ {
					if s[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			})())
}
