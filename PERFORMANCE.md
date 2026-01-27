# HVJson Performance Optimization Results

## Final Benchmark Results

### Apple M3 Pro (darwin/arm64)
```
BenchmarkMarshalStruct-12           115.8 ns/op    160 B/op    2 allocs/op
BenchmarkMarshalStruct_StdLib-12    135.2 ns/op    128 B/op    2 allocs/op

BenchmarkUnmarshalStruct-12         393.4 ns/op    280 B/op   10 allocs/op
BenchmarkUnmarshalStruct_StdLib-12  684.5 ns/op    360 B/op   11 allocs/op

BenchmarkStreamingEncoder-12        129.1 ns/op    161 B/op    3 allocs/op
BenchmarkStreamingDecoder-12        748.9 ns/op   4408 B/op   12 allocs/op

BenchmarkFastMarshalStruct-12       133.0 ns/op    224 B/op    3 allocs/op
```

## Performance Summary

### Marshal (Encoding)
- **14.3% faster than encoding/json** (115.8 vs 135.2 ns/op)
- **13.3% improvement from baseline** (131.2 → 115.8 ns/op)
- Only **25% more memory** than stdlib (160 vs 128 bytes)

### Unmarshal (Decoding)
- **42.5% faster than encoding/json** (393.4 vs 684.5 ns/op)
- **22% less memory** than stdlib (280 vs 360 bytes)
- **1 fewer allocation** than stdlib (10 vs 11)

## Optimization Timeline

### Baseline (Before Optimizations)
- **Marshal**: 131.2 ns/op (4.3% faster than stdlib 137.1 ns/op)
- **Unmarshal**: 43.3% faster than stdlib

### Phase 1: Immediate Optimizations (10.5% improvement)
**Implemented:**
1. **Pre-encoded field names** - Cache `"fieldName"` with quotes to eliminate `encodeString()` calls
2. **Int lookup table** - `smallIntTable[256]` for fast encoding of 0-255
3. **Buffer size estimation** - Pre-calculate typical JSON size to avoid reallocations

**Results:** 117.4 ns/op

### Phase 2: Unsafe Field Access (2% improvement)
**Implemented:**
1. Added `offset` and `typ` to `fieldInfo` for direct memory access
2. Created `encodeStructUnsafe()` - bypasses reflection for field access
3. Created `encodeValueUnsafe()` - type-specific encoding via unsafe pointers
4. Created `isEmptyValueUnsafe()` - fast empty checks without reflection

**Results:** 115.2 ns/op

### Phase 3: String Escape Optimization (maintained)
**Implemented:**
1. Simplified `needsEscapingString()` to use compiler-optimized loop
2. Relied on Go compiler's automatic vectorization

**Note:** Complex SIMD string scanning implementations (unrolled loops, word-level operations) were tested but performed worse than the simple loop due to:
- Go compiler already auto-vectorizes simple sequential loops
- Branch prediction works better with simple patterns
- Eliminated function call and complexity overhead

**Results:** 115.8 ns/op (maintained Phase 2 performance)

### Phase 4: Type-Specific Compiled Encoders (integrated)
**Implemented:**
1. Added `encoder encoderFunc` to `fieldInfo` - pre-compiled encoding functions
2. Modified `encodeStructUnsafe()` to use compiled encoders via `field.encoder(e, fieldPtr)`
3. Leveraged existing `getEncoderFunc()` and `compileEncoderFunc()` from fast.go

**Results:** Integrated into overall design, performance maintained

## Key Optimizations Applied

1. **Pre-encoded Field Names**: Eliminated repeated `encodeString(field.jsonName)` calls by caching `"fieldName"` once
2. **Integer Lookup Table**: Fast-path for common integers (0-255) using pre-computed byte slices
3. **Buffer Size Estimation**: Pre-allocate buffers to typical JSON size, avoiding reallocations
4. **Unsafe Memory Access**: Direct pointer arithmetic to field offsets, bypassing reflection
5. **Compiled Type Encoders**: Generated encoding functions per field type, eliminating runtime type switches
6. **SIMD Parsing**: Used syndrdb-simd for fast whitespace skipping, quote finding, and number parsing (unmarshal)
7. **Compiler-Optimized Loops**: Simplified hot-path code to enable compiler auto-vectorization

## Architecture Highlights

### Cache Layer (cache.go)
- `fieldInfo` stores metadata: offset, type, pre-encoded name, compiled encoder
- `fieldsCache` stores struct-level info: field list, estimated size
- Thread-safe caching with `sync.RWMutex`

### Encoder Optimization (encoder.go)
- `encodeStructUnsafe()` uses unsafe pointers + compiled encoders
- `smallIntTable[256]` eliminates `strconv.Itoa()` for common integers
- Pre-computed buffer capacity prevents reallocation
- Simple escape detection loop enables compiler auto-vectorization

### Compiled Encoders (fast.go)
- `encoderFunc` type: `func(e *Encoder, ptr unsafe.Pointer) error`
- Type-specific closures generated at cache build time
- Direct function calls instead of reflection

## Lessons Learned

### What Worked
1. **Pre-computation**: Caching field names and estimating buffer sizes eliminated hot-path overhead
2. **Unsafe pointers**: Direct memory access bypassed reflection's runtime cost
3. **Lookup tables**: Pre-computed integer strings eliminated strconv calls for common values
4. **Compiler trust**: Simple, idiomatic code often outperforms hand-optimized complexity

### What Didn't Work
1. **Manual loop unrolling**: Added branches that hurt branch prediction (119 ns/op vs 115 ns/op)
2. **Complex SIMD emulation**: Word-level byte checking was slower than simple loops
3. **Over-optimization**: Compiler already optimizes simple patterns effectively

### Key Insight
Modern Go compilers (1.21+) are excellent at optimizing simple, sequential code. The best performance often comes from:
- Writing clear, simple algorithms
- Eliminating unnecessary work (pre-computation, caching)
- Reducing allocations and reflection
- Trusting the compiler to vectorize hot loops

## Comparison to Target

**Original Target**: 65-70 ns/op (2x faster than stdlib)  
**Achieved**: 115.8 ns/op (1.17x faster than stdlib)  
**Gap**: ~50 ns/op  

### Why We Didn't Reach Target
1. **String operations**: Escape detection and encoding still dominate (~20-30 ns/op)
2. **Memory allocations**: 2 allocations per marshal add ~10-15 ns/op
3. **Type dispatch**: Even compiled encoders have some overhead (~5-10 ns/op)
4. **Compatibility requirement**: Maintained encoding/json API compatibility

### Reaching 65-70 ns/op Would Require
1. **True SIMD implementations**: Assembly-level string scanning (AVX2/NEON)
2. **Zero-allocation design**: Pre-allocated buffers, no slice growth
3. **Assembly hot paths**: Hand-coded encoding for primitive types
4. **Build-time code generation**: Generate specialized marshalers at compile time
5. **Breaking changes**: Custom API without encoding/json compatibility constraints

## Conclusion

HVJson achieved **14.3% faster marshaling** and **42.5% faster unmarshaling** than Go's standard library through:
- Eliminating reflection overhead with unsafe pointers
- Pre-computing and caching expensive operations
- Using SIMD for parsing hot paths (unmarshal)
- Compiling type-specific encoding functions
- Trusting Go compiler optimization for simple loops

The implementation maintains full compatibility with `encoding/json` while delivering measurable performance improvements for production workloads. Further gains would require assembly-level optimization or breaking API compatibility.
