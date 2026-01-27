package hvjson

import (
	"reflect"
	"testing"
)

func TestDebugUnmarshal(t *testing.T) {
	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	// Check field cache
	personType := reflect.TypeOf(Person{})
	cache := getCachedFields(personType)

	t.Logf("Field cache for Person:")
	t.Logf("  list:")
	for i, field := range cache.list {
		t.Logf("    [%d] jsonName=%q, index=%d, typ=%s", i, field.jsonName, field.index, field.typ)
	}
	t.Logf("  byExactName:")
	for name, field := range cache.byExactName {
		t.Logf("    [%q] -> index:%d, typ:%s", name, field.index, field.typ)
	}
	t.Logf("  byFoldedName:")
	for name, field := range cache.byFoldedName {
		t.Logf("    [%q] -> index:%d, typ:%s", name, field.index, field.typ)
	}

	jsonData := `{"name": "John", "age": 30, "extra": "ignored"}`
	var p Person
	err := Unmarshal([]byte(jsonData), &p)
	if err != nil {
		t.Errorf("Error: %v", err)
	} else {
		t.Logf("Success: name=%s, age=%d", p.Name, p.Age)
	}

	// Test with different field order
	jsonData2 := `{"age": 30, "name": "Jane"}`
	var p2 Person
	err = Unmarshal([]byte(jsonData2), &p2)
	if err != nil {
		t.Errorf("Error (different order): %v", err)
	} else {
		t.Logf("Success (different order): name=%s, age=%d", p2.Name, p2.Age)
	}
}
