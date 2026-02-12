package hvjson

import (
	"sync"
	"testing"
	"time"
)

// TestHighConcurrencyNoStarvation simulates 600-800 goroutines encoding simultaneously
// This reproduces the writer starvation issue and validates the fix
func TestHighConcurrencyNoStarvation(t *testing.T) {
	// Create multiple different struct types to trigger cache building
	type TestStruct1 struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	type TestStruct2 struct {
		ID    string  `json:"id"`
		Score float64 `json:"score"`
	}
	type TestStruct3 struct {
		Active bool   `json:"active"`
		Label  string `json:"label"`
	}
	type TestStruct4 struct {
		Data   []string          `json:"data"`
		Nested map[string]string `json:"nested"`
	}

	tests := []struct {
		name  string
		value interface{}
	}{
		{"struct1", TestStruct1{Name: "test", Value: 42}},
		{"struct2", TestStruct2{ID: "abc123", Score: 98.5}},
		{"struct3", TestStruct3{Active: true, Label: "test"}},
		{"struct4", TestStruct4{Data: []string{"a", "b"}, Nested: map[string]string{"k": "v"}}},
	}

	// Test with 800 concurrent goroutines
	const numGoroutines = 800
	const timeout = 5 * time.Second

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wg sync.WaitGroup
			wg.Add(numGoroutines)

			// Channel to signal completion
			done := make(chan bool, 1)

			// Start all goroutines at once
			for i := 0; i < numGoroutines; i++ {
				go func() {
					defer wg.Done()

					// Marshal the struct
					_, err := Marshal(tt.value)
					if err != nil {
						t.Errorf("Marshal failed: %v", err)
					}
				}()
			}

			// Wait for completion in a separate goroutine
			go func() {
				wg.Wait()
				done <- true
			}()

			// Wait with timeout to detect starvation
			select {
			case <-done:
				t.Logf("✅ %s: Successfully completed %d concurrent marshals", tt.name, numGoroutines)
			case <-time.After(timeout):
				t.Fatalf("❌ %s: TIMEOUT - Writer starvation detected! %d goroutines did not complete in %v",
					tt.name, numGoroutines, timeout)
			}
		})
	}
}

// TestConcurrentCacheAccess tests that cache building doesn't cause panics or races
func TestConcurrentCacheAccess(t *testing.T) {
	// Define a fresh type that hasn't been cached yet
	type FreshStruct struct {
		Field1 string `json:"field1"`
		Field2 int    `json:"field2"`
		Field3 bool   `json:"field3"`
	}

	const numGoroutines = 1000
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// All goroutines try to access cache simultaneously
	start := make(chan bool)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			<-start // Wait for signal to start all at once

			value := FreshStruct{
				Field1: "test",
				Field2: id,
				Field3: id%2 == 0,
			}

			// This should trigger cache building
			_, err := Marshal(value)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Start all goroutines simultaneously
	close(start)

	// Wait for completion with timeout
	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(errors)
		if len(errors) > 0 {
			t.Fatalf("Got errors during concurrent cache access: %v", <-errors)
		}
		t.Logf("✅ Successfully handled %d concurrent cache accesses", numGoroutines)
	case <-time.After(3 * time.Second):
		t.Fatal("❌ TIMEOUT - Concurrent cache access deadlocked!")
	}
}

// TestEncoderFuncCacheStarvation tests encoder function compilation under load
func TestEncoderFuncCacheStarvation(t *testing.T) {
	// Multiple fresh types to trigger encoder compilation
	type Type1 struct{ A string }
	type Type2 struct{ B int }
	type Type3 struct{ C bool }
	type Type4 struct{ D float64 }
	type Type5 struct{ E []string }

	types := []interface{}{
		Type1{A: "test"},
		Type2{B: 42},
		Type3{C: true},
		Type4{D: 3.14},
		Type5{E: []string{"a", "b"}},
	}

	const numGoroutines = 500
	var wg sync.WaitGroup

	for _, typ := range types {
		wg.Add(numGoroutines)

		// All goroutines encode the same type simultaneously
		for i := 0; i < numGoroutines; i++ {
			go func(value interface{}) {
				defer wg.Done()
				_, err := FastMarshal(value)
				if err != nil {
					t.Errorf("FastMarshal failed: %v", err)
				}
			}(typ)
		}
	}

	// Wait with timeout
	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Logf("✅ Successfully handled %d concurrent encoder compilations", len(types)*numGoroutines)
	case <-time.After(5 * time.Second):
		t.Fatal("❌ TIMEOUT - Encoder function cache starved!")
	}
}

// BenchmarkHighConcurrency benchmarks the system under high concurrent load
func BenchmarkHighConcurrency(b *testing.B) {
	type BenchStruct struct {
		Name   string   `json:"name"`
		Value  int      `json:"value"`
		Score  float64  `json:"score"`
		Active bool     `json:"active"`
		Tags   []string `json:"tags"`
	}

	data := BenchStruct{
		Name:   "benchmark",
		Value:  42,
		Score:  98.5,
		Active: true,
		Tags:   []string{"go", "json", "fast"},
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := Marshal(data)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
