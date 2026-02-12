# HVJson

High Velocity JSON → a pure Golang JSON marshaller that uses full SIMD across all current CPUs. If Sonic had a full Golang implementation, this would be it.

## Overview

HVJson is a high-performance JSON encoding/decoding library for Go that leverages SIMD (Single Instruction, Multiple Data) operations for maximum throughput. Built on top of [syndrdb-simd](https://github.com/dan-strohschein/syndrdb-simd), it provides significant performance improvements over stdlib while maintaining API compatibility.

## Performance

| Operation | HVJson | encoding/json | Improvement |
|-----------|--------|---------------|-------------|
| **Marshal (pointer)** | 88.0 ns/op, 0 allocs | 121.6 ns/op, 1 alloc | **27.6% faster, zero allocs** |
| **Marshal (value)** | 118.5 ns/op | 138.2 ns/op | **14.2% faster** |
| **MarshalTo (zero-copy)** | 88.4 ns/op, 0 allocs | N/A | **Zero-copy API** |
| **Unmarshal** | 529.4 ns/op | 736.5 ns/op | **28.1% faster** |

*Benchmarked on Apple M3 Pro (darwin/arm64), Go 1.21+*

### Best Practice: Use Pointers for Maximum Performance

```go
// ✅ Optimal: Pass pointer - 88 ns/op, 0 allocations
data, err := hvjson.Marshal(&myStruct)

// ⚠️ Slower: Pass value - 118 ns/op, 2 allocations  
data, err := hvjson.Marshal(myStruct)
```

## Features

- **SIMD-Accelerated Operations**: Uses AVX2 (x86-64) and NEON (ARM64) instructions for:
  - JSON string scanning and special character detection (5-8x faster)
  - String escaping with vectorized escape injection (3-5x faster)
  - Integer formatting (2-3x faster than strconv)
  - Float formatting (2-4x faster than strconv)
  - Whitespace skipping (20-40% of JSON processing)
  - UTF-8 validation (10-20x faster for ASCII)
  
- **Pre-compiled Type Encoders**: Generated encoding functions per type eliminate runtime reflection

- **Optimized Integer Encoding**: Pre-computed lookup table for integers 0-9999

- **Pre-encoded Field Names**: Field names cached with quotes to eliminate repeated string encoding

- **Zero-Copy APIs**: 
  - `MarshalTo(dst, v, config)` - Encode directly to caller's buffer
  - `MarshalPooled(v)` - Use pooled buffers for convenience
  
- **Streaming Support**: Full io.Reader/io.Writer compatibility
  - `NewEncoder(io.Writer)` for streaming output
  - `NewIncrementalEncoder(io.Writer, flushThreshold)` for incremental streaming (bounded memory, SIMD)
  - `NewDecoder(io.Reader)` for streaming input
  - `SetIndent(prefix, indent)` for formatted streaming output
  - `DisallowUnknownFields()` for strict decoding
  - `SetEscapeHTML(on)` for HTML character escaping control
  
- **MarshalIndent Support**: Formatted JSON output with customizable indentation
  - Pre-computed indent tables for common patterns (2-space, 4-space, tab)
  - `FastMarshalIndent` uses compiled encoders for maximum indented output speed
  - Only ~25 ns overhead vs non-indented output

- **UseNumber/UseInt64**: Control number parsing behavior
  - `UseNumber`: Preserve exact number strings as `Number` type
  - `UseInt64`: Return `int64` for integers instead of `float64`

- **HTML Escaping Control**: `EscapeHTML` config option (default: true, matches stdlib)
  - Escapes `<`, `>`, `&` as `\u003c`, `\u003e`, `\u0026`

- **Smart Buffer Management**: sync.Pool-based buffer reuse minimizes allocations

- **Optimized Cycle Detection**: Inline 8-pointer array avoids map allocation for simple structs

- **Struct Field Caching**: Reflection results cached for repeated marshal/unmarshal operations

- **Production-Ready Error Handling**: Detailed syntax errors with line/column positions

- **Zero CGO Dependencies**: Pure Go implementation with platform-specific assembly only in syndrdb-simd

- **Compatible API**: Drop-in replacement for `encoding/json`

## Installation

```bash
go get github.com/dan-strohschein/HVJson
```

## Quick Start

### Basic Usage

```go
import "github.com/dan-strohschein/HVJson/hvjson"

// Marshal
type User struct {
    Name  string `json:"name"`
    Email string `json:"email,omitempty"`
    Age   int    `json:"age"`
}

user := User{Name: "Alice", Age: 30}

// Best performance: use pointer
data, err := hvjson.Marshal(&user)

// Unmarshal
var result User
err = hvjson.Unmarshal(data, &result)
```

### Zero-Copy Encoding (Maximum Performance)

```go
// Pre-allocate a reusable buffer
buf := make([]byte, 0, 1024)

// Encode directly to buffer - zero allocations per call
for _, item := range items {
    buf = buf[:0]  // Reset without reallocation
    buf, err = hvjson.MarshalTo(buf, &item, nil)
    // Use buf...
}
```

### Pooled Encoding (Convenience)

```go
// Uses internal buffer pool
result, err := hvjson.MarshalPooled(&user)
json := result.String()  // or result.Data for []byte
```

### Streaming

```go
import "github.com/dan-strohschein/HVJson/hvjson"

// Stream Encoding
var buf bytes.Buffer
enc := hvjson.NewEncoder(&buf)
enc.Encode(user1)
enc.Encode(user2)

// Stream Decoding with strict mode
dec := hvjson.NewDecoder(reader)
dec.DisallowUnknownFields()  // Error on unknown fields
var user User
for {
    if err := dec.Decode(&user); err == io.EOF {
        break
    }
    // Process user...
}
```

### HTML Escaping Control

```go
// Streaming encoder - disable HTML escaping
enc := hvjson.NewEncoder(&buf)
enc.SetEscapeHTML(false)  // Don't escape <, >, &
enc.Encode(data)

// Config-based
config := &hvjson.Config{
    EscapeHTML: false,  // Default is true (matches stdlib)
    MaxDepth:   10000,
}
data, err := hvjson.MarshalWithConfig(&user, config)
```

### Fast Path (Pre-compiled Encoders)

```go
// Use pre-compiled encoders for maximum performance
data, err := hvjson.FastMarshal(&user)
```

### Indented Output

```go
// Standard indented output
data, err := hvjson.MarshalIndent(&user, "", "  ")

// Fast path with indentation (maximum performance)
data, err := hvjson.FastMarshalIndent(&user, "", "  ")

// Streaming with indentation
var buf bytes.Buffer
enc := hvjson.NewEncoder(&buf)
enc.SetIndent("", "  ")
enc.Encode(user)
```

### UseNumber and UseInt64

```go
// Preserve exact number representation
config := &hvjson.Config{UseNumber: true, MaxDepth: 10000}
var m map[string]interface{}
hvjson.UnmarshalWithConfig(data, &m, config)
num := m["value"].(hvjson.Number)
fmt.Println(num.String())  // Exact string representation
i, _ := num.Int64()         // Convert to int64
f, _ := num.Float64()       // Convert to float64

// Force integers to int64 instead of float64
config := &hvjson.Config{UseInt64: true, MaxDepth: 10000}
hvjson.UnmarshalWithConfig(data, &m, config)
// m["count"] is now int64(42) instead of float64(42)
```

### DisallowUnknownFields

```go
// Strict mode: error on unknown JSON fields
config := &hvjson.Config{
    DisallowUnknownFields: true,
    MaxDepth: 10000,
}
err := hvjson.UnmarshalWithConfig(data, &result, config)
// Returns error if JSON contains fields not in struct
```

## Configuration

```go
// Default configuration (validation enabled, HTML escaping on)
config := hvjson.ConfigDefault()

// Maximum performance (skip validation)
config := hvjson.ConfigFastest()

// Custom configuration
config := &hvjson.Config{
    NoValidateJSONSkip:    false,  // Validate UTF-8
    UseStreamingUTF8:      true,   // Streaming for large docs
    MaxDepth:              10000,  // Nesting limit
    DisableCache:          false,  // Enable field caching
    UseNumber:             false,  // Return Number type for interface{}
    UseInt64:              false,  // Return int64 for integers
    DoIndent:              false,  // Enable indentation
    IndentPrefix:          "",     // Line prefix
    IndentString:          "  ",   // Indent per level
    DisallowUnknownFields: false,  // Error on unknown struct fields
    EscapeHTML:            true,   // Escape <, >, & (matches stdlib)
}

data, err := hvjson.MarshalWithConfig(&user, config)
```

## API Reference

### Marshal Functions

#### `Marshal(v interface{}) ([]byte, error)`
Encodes a Go value to JSON. Automatically uses FastMarshal for struct types for optimal performance.

**Parameters:**
- `v` - Any Go value to encode

**Returns:**
- `[]byte` - JSON-encoded data
- `error` - Error if encoding fails

**Example:**
```go
user := User{Name: "Alice", Age: 30}
data, err := hvjson.Marshal(&user)  // Use pointer for zero allocations
```

---

#### `MarshalString(v interface{}) (string, error)`
Convenience function that encodes to JSON and returns a string instead of []byte.

**Parameters:**
- `v` - Any Go value to encode

**Returns:**
- `string` - JSON-encoded string
- `error` - Error if encoding fails

**Example:**
```go
jsonStr, err := hvjson.MarshalString(&user)
fmt.Println(jsonStr)  // {"name":"Alice","age":30}
```

---

#### `MarshalWithConfig(v interface{}, config *Config) ([]byte, error)`
Encodes a Go value to JSON using custom configuration.

**Parameters:**
- `v` - Any Go value to encode
- `config` - Configuration options (or nil for defaults)

**Returns:**
- `[]byte` - JSON-encoded data
- `error` - Error if encoding fails

**Example:**
```go
config := &hvjson.Config{
    EscapeHTML: false,
    MaxDepth:   10000,
}
data, err := hvjson.MarshalWithConfig(&user, config)
```

---

#### `MarshalTo(dst []byte, v interface{}, config *Config) ([]byte, error)`
Zero-copy encoding that appends JSON to the provided buffer. Best for performance-critical code.

**Parameters:**
- `dst` - Destination buffer to append to (can be empty)
- `v` - Any Go value to encode
- `config` - Configuration options (or nil for defaults)

**Returns:**
- `[]byte` - Extended buffer with JSON data
- `error` - Error if encoding fails

**Example:**
```go
// Reusable buffer for zero allocations
buf := make([]byte, 0, 1024)
for _, item := range items {
    buf = buf[:0]  // Reset without reallocation
    buf, err = hvjson.MarshalTo(buf, &item, nil)
    // Use buf...
}
```

---

#### `MarshalPooled(v interface{}) (*MarshalResult, error)`
Encodes using an internal buffer pool. More convenient than MarshalTo but slightly slower.

**Parameters:**
- `v` - Any Go value to encode

**Returns:**
- `*MarshalResult` - Result with Data field containing JSON
- `error` - Error if encoding fails

**Example:**
```go
result, err := hvjson.MarshalPooled(&user)
json := result.Data  // or result.String()
```

---

#### `MarshalIndent(v interface{}, prefix, indent string) ([]byte, error)`
Encodes with human-readable indentation. Uses pre-computed indent tables for performance.

**Parameters:**
- `v` - Any Go value to encode
- `prefix` - String to prefix each line with (usually "")
- `indent` - Indentation string per level (e.g., "  " or "\t")

**Returns:**
- `[]byte` - Indented JSON-encoded data
- `error` - Error if encoding fails

**Example:**
```go
data, err := hvjson.MarshalIndent(&user, "", "  ")
// {
//   "name": "Alice",
//   "age": 30
// }
```

---

#### `FastMarshal(v interface{}) ([]byte, error)`
Uses pre-compiled type encoders for maximum performance. Best for struct types.

**Parameters:**
- `v` - Any Go value to encode (optimized for structs)

**Returns:**
- `[]byte` - JSON-encoded data
- `error` - Error if encoding fails

**Example:**
```go
data, err := hvjson.FastMarshal(&user)
```

---

#### `FastMarshalIndent(v interface{}, prefix, indent string) ([]byte, error)`
Fast path encoding with indentation. Combines pre-compiled encoders with indent formatting.

**Parameters:**
- `v` - Any Go value to encode
- `prefix` - String to prefix each line with
- `indent` - Indentation string per level

**Returns:**
- `[]byte` - Indented JSON-encoded data
- `error` - Error if encoding fails

**Example:**
```go
data, err := hvjson.FastMarshalIndent(&user, "", "    ")
```

---

### Unmarshal Functions

#### `Unmarshal(data []byte, v interface{}) error`
Decodes JSON data into a Go value.

**Parameters:**
- `data` - JSON-encoded data
- `v` - Pointer to value to decode into

**Returns:**
- `error` - Error if decoding fails

**Example:**
```go
var user User
err := hvjson.Unmarshal(data, &user)
```

---

#### `UnmarshalString(data string, v interface{}) error`
Convenience function that decodes from a JSON string.

**Parameters:**
- `data` - JSON-encoded string
- `v` - Pointer to value to decode into

**Returns:**
- `error` - Error if decoding fails

**Example:**
```go
var user User
err := hvjson.UnmarshalString(`{"name":"Alice"}`, &user)
```

---

#### `UnmarshalWithConfig(data []byte, v interface{}, config *Config) error`
Decodes JSON with custom configuration options.

**Parameters:**
- `data` - JSON-encoded data
- `v` - Pointer to value to decode into
- `config` - Configuration options

**Returns:**
- `error` - Error if decoding fails

**Example:**
```go
config := &hvjson.Config{
    DisallowUnknownFields: true,
    UseNumber: true,
}
var result map[string]interface{}
err := hvjson.UnmarshalWithConfig(data, &result, config)
```

---

### Configuration Functions

#### `ConfigDefault() *Config`
Returns the default configuration with validation enabled and HTML escaping on.

**Returns:**
- `*Config` - Default configuration

**Example:**
```go
config := hvjson.ConfigDefault()
config.MaxDepth = 5000  // Customize as needed
```

---

#### `ConfigFastest() *Config`
Returns configuration optimized for maximum speed (skips validation).

**Returns:**
- `*Config` - Performance-optimized configuration

**Example:**
```go
config := hvjson.ConfigFastest()
data, err := hvjson.MarshalWithConfig(&user, config)
```

---

### Streaming API

#### `NewEncoder(w io.Writer) *StreamEncoder`
Creates a streaming encoder that writes JSON to an io.Writer.

**Parameters:**
- `w` - Writer to output JSON to

**Returns:**
- `*StreamEncoder` - Streaming encoder instance

**Example:**
```go
var buf bytes.Buffer
enc := hvjson.NewEncoder(&buf)
enc.Encode(user1)
enc.Encode(user2)
```

---

#### `(*StreamEncoder) Encode(v interface{}) error`
Encodes a value and writes it to the underlying writer.

**Parameters:**
- `v` - Value to encode

**Returns:**
- `error` - Error if encoding fails

**Example:**
```go
enc := hvjson.NewEncoder(os.Stdout)
for _, user := range users {
    if err := enc.Encode(user); err != nil {
        log.Fatal(err)
    }
}
```

---

#### `(*StreamEncoder) SetIndent(prefix, indent string)`
Configures indentation for formatted output.

**Parameters:**
- `prefix` - String to prefix each line with
- `indent` - Indentation string per level

**Example:**
```go
enc := hvjson.NewEncoder(&buf)
enc.SetIndent("", "  ")
enc.Encode(user)  // Outputs indented JSON
```

---

#### `(*StreamEncoder) SetEscapeHTML(on bool)`
Controls whether <, >, and & are escaped as Unicode sequences.

**Parameters:**
- `on` - true to escape HTML characters, false otherwise

**Example:**
```go
enc := hvjson.NewEncoder(&buf)
enc.SetEscapeHTML(false)  // Don't escape <, >, &
enc.Encode(data)
```

---

### Incremental streaming (bounded memory)

`StreamEncoder` encodes each value fully into memory before writing. For very large values or many documents, use the incremental streaming encoder so the buffer is flushed to the writer when it reaches a size threshold (default 64 KiB). Same SIMD encoding; only the write path is incremental.

#### `NewIncrementalEncoder(w io.Writer, flushThreshold int) *IncrementalStreamEncoder`
Creates an encoder that flushes to `w` whenever the internal buffer reaches `flushThreshold` bytes. If `flushThreshold <= 0`, `DefaultIncrementalFlushThreshold` (64 KiB) is used. Tuned for speed; use a smaller value for lower memory.

**Parameters:**
- `w` - Writer to output JSON to
- `flushThreshold` - Buffer size in bytes before flushing; 0 for default (64 KiB)

**Returns:**
- `*IncrementalStreamEncoder` - Incremental streaming encoder instance

**Example:**
```go
var buf bytes.Buffer
enc := hvjson.NewIncrementalEncoder(&buf, 4096)  // flush every 4 KiB
for _, doc := range largeDocs {
    if err := enc.Encode(doc); err != nil {
        log.Fatal(err)
    }
}
```

The underlying `Writer` should not return partial writes (wrap with `bufio.Writer` if needed). `SetIndent` and `SetEscapeHTML` are supported the same as `StreamEncoder`.

---

#### `NewDecoder(r io.Reader) *StreamDecoder`
Creates a streaming decoder that reads JSON from an io.Reader.

**Parameters:**
- `r` - Reader to read JSON from

**Returns:**
- `*StreamDecoder` - Streaming decoder instance

**Example:**
```go
dec := hvjson.NewDecoder(resp.Body)
var result Response
if err := dec.Decode(&result); err != nil {
    log.Fatal(err)
}
```

---

#### `(*StreamDecoder) Decode(v interface{}) error`
Decodes the next JSON value from the stream.

**Parameters:**
- `v` - Pointer to value to decode into

**Returns:**
- `error` - Error if decoding fails (io.EOF when stream ends)

**Example:**
```go
dec := hvjson.NewDecoder(file)
for {
    var user User
    if err := dec.Decode(&user); err == io.EOF {
        break
    } else if err != nil {
        log.Fatal(err)
    }
    processUser(user)
}
```

---

#### `(*StreamDecoder) DisallowUnknownFields()`
Configures decoder to return errors for unknown struct fields.

**Example:**
```go
dec := hvjson.NewDecoder(reader)
dec.DisallowUnknownFields()  // Strict mode
var user User
err := dec.Decode(&user)  // Errors if JSON has extra fields
```

---

#### `(*StreamDecoder) UseNumber()`
Configures decoder to decode numbers as Number type instead of float64.

**Example:**
```go
dec := hvjson.NewDecoder(reader)
dec.UseNumber()
var m map[string]interface{}
dec.Decode(&m)
num := m["value"].(hvjson.Number)
i, _ := num.Int64()  // Precise integer conversion
```

---

### Types

#### `type Config struct`
Configuration options for encoding and decoding.

**Fields:**
```go
type Config struct {
    NoValidateJSONSkip    bool   // Skip UTF-8 validation
    UseStreamingUTF8      bool   // Use streaming validation for large docs
    MaxDepth              int    // Maximum nesting depth (default: 10000)
    DisableCache          bool   // Disable struct field caching
    UseNumber             bool   // Return Number type for interface{}
    UseInt64              bool   // Return int64 for integers
    DoIndent              bool   // Enable indentation
    IndentPrefix          string // Line prefix for indented output
    IndentString          string // Indent string per level
    DisallowUnknownFields bool   // Error on unknown struct fields
    EscapeHTML            bool   // Escape <, >, & (default: true)
}
```

---

#### `type Number string`
Preserves exact JSON number representation for precision.

**Methods:**
- `String() string` - Returns the literal number text
- `Int64() (int64, error)` - Converts to int64
- `Float64() (float64, error)` - Converts to float64

**Example:**
```go
config := &hvjson.Config{UseNumber: true}
var m map[string]interface{}
hvjson.UnmarshalWithConfig(data, &m, config)
num := m["bigNumber"].(hvjson.Number)
fmt.Println(num.String())  // Exact representation
```

---

#### `type MarshalResult struct`
Result from MarshalPooled containing JSON data.

**Fields:**
- `Data []byte` - JSON-encoded data

**Methods:**
- `String() string` - Returns JSON as string

**Example:**
```go
result, err := hvjson.MarshalPooled(&user)
fmt.Println(result.String())
// Access raw bytes: result.Data
```

## Performance Details

HVJson prioritizes performance through:

1. **SIMD Operations** (via syndrdb-simd): 
   - `HasSpecialChars`: Vectorized JSON special character detection (5-8x faster)
   - `EscapeJSONString`: SIMD-accelerated string escaping (3-5x faster)
   - `FormatInt64/FormatUint64`: Optimized integer formatting (2-3x faster)
   - `FormatFloat64`: SIMD float formatting (2-4x faster)
   - `SkipWhitespace`: Fast-path through JSON whitespace
   - `ValidateUTF8`: 10-20x faster for ASCII content

2. **Pre-computation & Caching**:
   - Pre-encoded field names with quotes (`"fieldName"`)
   - Integer lookup table (0-9999) eliminates strconv calls
   - Buffer size estimation prevents reallocations
   - Compiled type-specific encoders cached per struct type
   - Inline 8-pointer cycle detection (avoids map allocation)

3. **Unsafe Memory Access**:
   - Direct pointer arithmetic to field offsets
   - Bypasses reflection for field access in hot paths
   - Type-specific encoding without runtime type switches

4. **Buffer Pooling**: Three-tier pool (1KB, 64KB, 1MB) reduces GC pressure

5. **Tuned SIMD Thresholds**: 8-byte threshold for string SIMD on ARM64/NEON

### Detailed Benchmarks

```
goos: darwin
goarch: arm64
cpu: Apple M3 Pro

BenchmarkMarshalStruct-12              118.5 ns/op    128 B/op    2 allocs/op
BenchmarkMarshalStructPtr-12            88.0 ns/op      0 B/op    0 allocs/op
BenchmarkMarshalStruct_StdLib-12       138.2 ns/op    128 B/op    2 allocs/op
BenchmarkMarshalStructPtr_StdLib-12    121.6 ns/op     64 B/op    1 allocs/op

BenchmarkMarshalTo-12                   88.4 ns/op      0 B/op    0 allocs/op
BenchmarkFastMarshalStruct-12          119.6 ns/op    128 B/op    2 allocs/op

BenchmarkMarshalIndent-12              145.9 ns/op    160 B/op    3 allocs/op
BenchmarkFastMarshalIndent-12          143.4 ns/op    160 B/op    3 allocs/op

BenchmarkUnmarshalStruct-12            529.4 ns/op    376 B/op   16 allocs/op
BenchmarkUnmarshalStruct_StdLib-12     736.5 ns/op    360 B/op   11 allocs/op

BenchmarkStreamingEncoder-12           195.0 ns/op    128 B/op    2 allocs/op
BenchmarkStreamingDecoder-12           1876 ns/op    8799 B/op   21 allocs/op
BenchmarkStreamingDecoderFromBufio-12  1778 ns/op    8799 B/op   21 allocs/op
BenchmarkStreamingDecoderManyValues-12 38301 ns/op   27236 B/op  805 allocs/op
```

*Streaming benchmarks: encoder uses single write + newline (2 allocs); decoder uses pooled decoder, bufio.Reader, SIMD whitespace, and optional buffer pool. `BenchmarkStreamingDecoderFromBufio` measures decoder with an explicit bufio.Reader; `BenchmarkStreamingDecoderManyValues` decodes 50 NDJSON values per op.*

### Performance Summary

| Metric | HVJson | stdlib | Improvement |
|--------|--------|--------|-------------|
| Marshal (pointer) | 88.0 ns/op, 0 allocs | 121.6 ns/op, 1 alloc | **27.6% faster** |
| Marshal (value) | 118.5 ns/op | 138.2 ns/op | **14.2% faster** |
| MarshalTo (zero-copy) | 88.4 ns/op, 0 allocs | N/A | **Zero allocations** |
| MarshalIndent | 145.9 ns/op | N/A | Pre-computed tables |
| Unmarshal | 529.4 ns/op | 736.5 ns/op | **28.1% faster** |
| Streaming encoder | 195.0 ns/op, 2 allocs | N/A | Single write, pooled |
| Streaming decoder | 1876 ns/op | N/A | Buffered, SIMD whitespace |
| Streaming decoder (many values) | 38301 ns/op (50 values) | N/A | NDJSON throughput |

## Benchmarks

Run benchmarks comparing HVJson to `encoding/json`:

```bash
cd hvjson
go test -bench=. -benchmem
```

## SIMD Operations Used

| Operation | Purpose | SIMD Acceleration |
|-----------|---------|-------------------|
| HasSpecialChars | Detect JSON special chars | ✅ AVX2/NEON |
| EscapeJSONString | Escape special characters | ✅ AVX2/NEON |
| EscapedSize | Calculate escaped string size | ✅ AVX2/NEON |
| FormatInt64 | Integer to ASCII conversion | ✅ AVX2/NEON |
| FormatUint64 | Unsigned int to ASCII | ✅ AVX2/NEON |
| FormatFloat64 | Float to ASCII conversion | ✅ AVX2/NEON |
| SkipWhitespace | Skip spaces/tabs/newlines | ✅ AVX2/NEON |
| FindQuote | Locate string boundaries | ✅ AVX2/NEON |
| FindStructuralChar | Find `{`, `}`, `[`, `]`, etc. | ✅ AVX2/NEON |
| ValidateUTF8 | UTF-8 validation | ✅ AVX2/NEON |
| IsDigit | Digit validation | ✅ AVX2/NEON |

## Optimization Techniques

### What Works Best
1. **Pre-computation**: Caching field names and buffer sizes eliminates hot-path overhead
2. **Unsafe pointers**: Direct memory access bypasses reflection's runtime cost
3. **Lookup tables**: Pre-computed integer strings (0-9999) eliminate strconv calls
4. **SIMD acceleration**: Vectorized operations for string scanning and escaping
5. **Compiler trust**: Simple, idiomatic code enables Go's auto-vectorization
6. **Pointer usage**: Passing pointers avoids addressability allocations

### Key Insights
Modern Go compilers (1.21+) are excellent at optimizing simple, sequential code. The best performance comes from:
- Writing clear, simple algorithms
- Eliminating unnecessary work (pre-computation, caching)
- Reducing allocations and reflection
- Using SIMD for bulk data operations
- Trusting the compiler to vectorize hot loops

## Error Handling

HVJson provides detailed error information:

```go
err := hvjson.Unmarshal([]byte(`{"invalid": }`), &result)
if syntaxErr, ok := err.(*hvjson.SyntaxError); ok {
    fmt.Printf("Error at line %d, column %d: %s\n", 
        syntaxErr.Line, syntaxErr.Column, syntaxErr.Message)
}
```

Error codes include:
- `ErrorInvalidJSON`: Malformed JSON
- `ErrorInvalidString`: String parsing error
- `ErrorInvalidNumber`: Number parsing error
- `ErrorTypeMismatch`: Type incompatibility
- `ErrorStackOverflow`: Exceeded max depth
- `ErrorInvalidUTF8`: UTF-8 validation failure

## Architecture

```
hvjson/
├── api.go          - Public API (Marshal, Unmarshal, Config, MarshalTo, MarshalPooled)
├── decoder.go      - JSON decoder with SIMD operations
├── encoder.go      - JSON encoder with SIMD escape handling
├── fast.go         - Pre-compiled type encoders for FastMarshal
├── streaming.go    - Streaming Encoder/Decoder
├── cache.go        - Struct field caching
├── buffers.go      - Buffer pool management
├── errors.go       - Error types and formatting
└── *_test.go       - Test suite and benchmarks
```

## Current Status

**Beta Release** - Core functionality implemented, tested, and optimized. Full SIMD acceleration integrated.

### Working
- ✅ Marshal/Unmarshal for all JSON types
- ✅ Struct field caching with json tags
- ✅ Buffer pooling
- ✅ SIMD whitespace skipping
- ✅ SIMD UTF-8 validation
- ✅ SIMD integer/float formatting
- ✅ Error reporting with position tracking
- ✅ MarshalIndent with pre-computed indent tables
- ✅ FastMarshalIndent with compiled encoders
- ✅ UseNumber for exact number preservation
- ✅ UseInt64 for integer type control
- ✅ StreamEncoder.SetIndent for formatted streaming
- ✅ IncrementalStreamEncoder for incremental flush (bounded memory, same SIMD)
- ✅ Full SIMD acceleration for string operations
- ✅ DisallowUnknownFields for strict decoding
- ✅ SetEscapeHTML for HTML character control
- ✅ MarshalTo for zero-copy encoding
- ✅ MarshalPooled for pooled buffer encoding
- ✅ Inline cycle detection (8-pointer optimization)
- ✅ Expanded integer lookup table (0-9999)

### In Progress
- 🚧 Comprehensive benchmarks vs sonic
- 🚧 Additional edge case testing

## Contributing

Contributions welcome! Areas of focus:
- Performance optimization and benchmarking
- Additional SIMD acceleration in syndrdb-simd
- Edge case testing and fuzzing
- Documentation improvements

## License

MIT License - See LICENSE file for details

## Related Projects

- [syndrdb-simd](https://github.com/dan-strohschein/syndrdb-simd) - SIMD operations library
- [bytedance/sonic](https://github.com/bytedance/sonic) - Inspiration for performance goals
