# HVJson

High Velocity JSON → a pure Golang JSON marshaller that uses full SIMD across all current CPUs. If Sonic had a full Golang implementation, this would be it.

## Overview

HVJson is a high-performance JSON encoding/decoding library for Go that leverages SIMD (Single Instruction, Multiple Data) operations for maximum throughput. Built on top of [syndrdb-simd](https://github.com/dan-strohschein/syndrdb-simd), it provides significant performance improvements over stdlib while maintaining API compatibility.

## Performance

| Operation | HVJson | encoding/json | Improvement |
|-----------|--------|---------------|-------------|
| **Marshal** | 113.7 ns/op | 137.3 ns/op | **17.2% faster** |
| **FastMarshal** | 104.9 ns/op | 137.3 ns/op | **23.6% faster** |
| **Unmarshal** | 393.4 ns/op | 684.5 ns/op | **42.5% faster** |

*Benchmarked on Apple M3 Pro (darwin/arm64), Go 1.21+*

## Features

- **SIMD-Accelerated Operations**: Uses AVX2 (x86-64) and NEON (ARM64) instructions for:
  - JSON string scanning and special character detection (5-8x faster)
  - String escaping with vectorized escape injection (3-5x faster)
  - Integer formatting (2-3x faster than strconv)
  - Whitespace skipping (20-40% of JSON processing)
  - UTF-8 validation (10-20x faster for ASCII)
  
- **Pre-compiled Type Encoders**: Generated encoding functions per type eliminate runtime reflection

- **Optimized Integer Encoding**: Pre-computed lookup table for integers 0-999

- **Pre-encoded Field Names**: Field names cached with quotes to eliminate repeated string encoding
  
- **Streaming Support**: Full io.Reader/io.Writer compatibility
  - `NewEncoder(io.Writer)` for streaming output
  - `NewDecoder(io.Reader)` for streaming input
  - `SetIndent(prefix, indent)` for formatted streaming output
  
- **MarshalIndent Support**: Formatted JSON output with customizable indentation
  - Pre-computed indent tables for common patterns (2-space, 4-space, tab)
  - `FastMarshalIndent` uses compiled encoders for maximum indented output speed
  - Only ~25 ns overhead vs non-indented output

- **UseNumber/UseInt64**: Control number parsing behavior
  - `UseNumber`: Preserve exact number strings as `Number` type
  - `UseInt64`: Return `int64` for integers instead of `float64`

- **Smart Buffer Management**: sync.Pool-based buffer reuse minimizes allocations

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
data, err := hvjson.Marshal(user)

// Unmarshal
var result User
err = hvjson.Unmarshal(data, &result)
```

### Streaming

```go
import "github.com/dan-strohschein/HVJson/hvjson"

// Stream Encoding
var buf bytes.Buffer
enc := hvjson.NewEncoder(&buf)
enc.Encode(user1)
enc.Encode(user2)

// Stream Decoding
dec := hvjson.NewDecoder(reader)
var user User
for {
    if err := dec.Decode(&user); err == io.EOF {
        break
    }
    // Process user...
}
```

### Fast Path (Experimental)

```go
// Use pre-compiled encoders for maximum performance
data, err := hvjson.FastMarshal(user)
```

### Indented Output

```go
// Standard indented output
data, err := hvjson.MarshalIndent(user, "", "  ")

// Fast path with indentation (maximum performance)
data, err := hvjson.FastMarshalIndent(user, "", "  ")

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

## Configuration

```go
// Default configuration (validation enabled)
config := hvjson.ConfigDefault()

// Maximum performance (skip validation)
config := hvjson.ConfigFastest()

// Custom configuration
config := &hvjson.Config{
    NoValidateJSONSkip: false,     // Validate UTF-8
    UseStreamingUTF8:   true,      // Streaming for large docs
    MaxDepth:           10000,     // Nesting limit
    DisableCache:       false,     // Enable field caching
    UseNumber:          false,     // Return Number type for interface{}
    UseInt64:           false,     // Return int64 for integers
    DoIndent:           false,     // Enable indentation
    IndentPrefix:       "",        // Line prefix
    IndentString:       "  ",      // Indent per level
}

data, err := hvjson.MarshalWithConfig(user, config)
```

## Performance

HVJson prioritizes performance through:

1. **SIMD Operations** (via syndrdb-simd): 
   - `HasSpecialChars`: Vectorized JSON special character detection (5-8x faster)
   - `EscapeJSONString`: SIMD-accelerated string escaping (3-5x faster)
   - `FormatInt64/FormatUint64`: Optimized integer formatting (2-3x faster)
   - `SkipWhitespace`: Fast-path through JSON whitespace
   - `ValidateUTF8`: 10-20x faster for ASCII content

2. **Pre-computation & Caching**:
   - Pre-encoded field names with quotes (`"fieldName"`)
   - Integer lookup table (0-999) eliminates strconv calls
   - Buffer size estimation prevents reallocations
   - Compiled type-specific encoders cached per struct type

3. **Unsafe Memory Access**:
   - Direct pointer arithmetic to field offsets
   - Bypasses reflection for field access in hot paths
   - Type-specific encoding without runtime type switches

4. **Buffer Pooling**: Three-tier pool (1KB, 64KB, 1MB) reduces GC pressure

### Detailed Benchmarks

```
goos: darwin
goarch: arm64
cpu: Apple M3 Pro

BenchmarkMarshalStruct-12           113.7 ns/op    128 B/op    2 allocs/op
BenchmarkMarshalStruct_StdLib-12    137.3 ns/op    128 B/op    2 allocs/op

BenchmarkFastMarshalStruct-12       104.9 ns/op    128 B/op    2 allocs/op

BenchmarkMarshalIndent-12           152.0 ns/op    192 B/op    3 allocs/op
BenchmarkFastMarshalIndent-12       144.5 ns/op    160 B/op    3 allocs/op

BenchmarkUnmarshalStruct-12         393.4 ns/op    280 B/op   10 allocs/op
BenchmarkUnmarshalStruct_StdLib-12  684.5 ns/op    360 B/op   11 allocs/op

BenchmarkStreamingEncoder-12        127.7 ns/op    129 B/op    3 allocs/op
BenchmarkStreamingDecoder-12        749.1 ns/op   4408 B/op   12 allocs/op
```

### Performance Summary

| Metric | HVJson | stdlib | Improvement |
|--------|--------|--------|-------------|
| Marshal (standard) | 113.7 ns/op | 137.3 ns/op | 17.2% faster |
| Marshal (fast path) | 104.9 ns/op | 137.3 ns/op | 23.6% faster |
| MarshalIndent | 152.0 ns/op | N/A | Pre-computed tables |
| FastMarshalIndent | 144.5 ns/op | N/A | ~25 ns indent overhead |
| Unmarshal | 393.4 ns/op | 684.5 ns/op | 42.5% faster |
| Unmarshal memory | 280 B/op | 360 B/op | 22% less |
| Unmarshal allocs | 10 allocs | 11 allocs | 1 fewer |

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
| SkipWhitespace | Skip spaces/tabs/newlines | ✅ AVX2/NEON |
| FindQuote | Locate string boundaries | ✅ AVX2/NEON |
| FindStructuralChar | Find `{`, `}`, `[`, `]`, etc. | ✅ AVX2/NEON |
| ValidateUTF8 | UTF-8 validation | ✅ AVX2/NEON |
| IsDigit | Digit validation | ✅ AVX2/NEON |

## Optimization Techniques

### What Works Best
1. **Pre-computation**: Caching field names and buffer sizes eliminates hot-path overhead
2. **Unsafe pointers**: Direct memory access bypasses reflection's runtime cost
3. **Lookup tables**: Pre-computed integer strings (0-999) eliminate strconv calls
4. **SIMD acceleration**: Vectorized operations for string scanning and escaping
5. **Compiler trust**: Simple, idiomatic code enables Go's auto-vectorization

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
├── api.go          - Public API (Marshal, Unmarshal, Config)
├── decoder.go      - JSON decoder with SIMD operations
├── encoder.go      - JSON encoder with escape handling
├── cache.go        - Struct field caching
├── buffers.go      - Buffer pool management
├── errors.go       - Error types and formatting
└── *_test.go       - Test suite and benchmarks
```

## Current Status

**Alpha Release** - Core functionality implemented and tested. SIMD operations are integrated where available, with fallbacks for remaining operations.

### Working
- ✅ Marshal/Unmarshal for all JSON types
- ✅ Struct field caching with json tags
- ✅ Buffer pooling
- ✅ SIMD whitespace skipping
- ✅ SIMD UTF-8 validation
- ✅ Error reporting with position tracking
- ✅ MarshalIndent with pre-computed indent tables
- ✅ FastMarshalIndent with compiled encoders
- ✅ UseNumber for exact number preservation
- ✅ UseInt64 for integer type control
- ✅ StreamEncoder.SetIndent for formatted streaming
- ✅ Full SIMD acceleration for string operations

### In Progress
- 🚧 Comprehensive benchmarks vs sonic
- 🚧 DisallowUnknownFields support
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
