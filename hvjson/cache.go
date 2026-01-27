package hvjson

import (
	"reflect"
	"strings"
	"sync"
	"unicode"
)

type fieldInfo struct {
	index       int
	offset      uintptr      // Field offset for unsafe access
	typ         reflect.Type // Field type
	jsonName    string
	encodedName []byte // Pre-encoded: "fieldName" (with quotes, no colon)
	omitEmpty   bool
	skip        bool
	encoder     encoderFunc // Pre-compiled encoder for this field type
}

type fieldsCache struct {
	list          []*fieldInfo
	byExactName   map[string]*fieldInfo
	byFoldedName  map[string]*fieldInfo
	estimatedSize int // Estimated JSON size for buffer allocation
}

var (
	fieldsCacheMu sync.RWMutex
	fieldsMap     = make(map[reflect.Type]*fieldsCache)
	buildOnceMu   sync.Mutex
	buildOnce     = make(map[reflect.Type]*sync.Once)
)

func getCachedFields(t reflect.Type) *fieldsCache {
	// Fast path: check if already cached
	fieldsCacheMu.RLock()
	cache, ok := fieldsMap[t]
	fieldsCacheMu.RUnlock()

	if ok {
		return cache
	}

	// Slow path: ensure only one goroutine builds cache for this type
	// This prevents writer starvation under high concurrency
	buildOnceMu.Lock()
	once, exists := buildOnce[t]
	if !exists {
		once = &sync.Once{}
		buildOnce[t] = once
	}
	buildOnceMu.Unlock()

	// Only one goroutine will execute this block per type
	once.Do(func() {
		built := buildFieldsCache(t)
		fieldsCacheMu.Lock()
		fieldsMap[t] = built
		fieldsCacheMu.Unlock()
	})

	// Read the now-cached value
	fieldsCacheMu.RLock()
	cache = fieldsMap[t]
	fieldsCacheMu.RUnlock()

	return cache
}

func buildFieldsCache(t reflect.Type) *fieldsCache {
	cache := &fieldsCache{
		list:         make([]*fieldInfo, 0),
		byExactName:  make(map[string]*fieldInfo),
		byFoldedName: make(map[string]*fieldInfo),
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}

		info := &fieldInfo{
			index:  i,
			offset: field.Offset,
			typ:    field.Type,
		}

		if tag != "" {
			parts := strings.Split(tag, ",")
			if len(parts) > 0 && parts[0] != "" {
				info.jsonName = parts[0]
			}
			for _, opt := range parts[1:] {
				if opt == "omitempty" {
					info.omitEmpty = true
				}
			}
		}

		if info.jsonName == "" {
			info.jsonName = field.Name
		}

		// Pre-encode the field name with quotes
		info.encodedName = make([]byte, 0, len(info.jsonName)+2)
		info.encodedName = append(info.encodedName, '"')
		info.encodedName = append(info.encodedName, info.jsonName...)
		info.encodedName = append(info.encodedName, '"')

		// Get compiled encoder for this field type
		info.encoder = getEncoderFunc(field.Type)

		cache.list = append(cache.list, info)
		cache.byExactName[info.jsonName] = info
		cache.byFoldedName[foldName(info.jsonName)] = info
	}

	// Calculate estimated JSON size for buffer allocation
	cache.estimatedSize = 2 // {}
	for _, field := range cache.list {
		cache.estimatedSize += len(field.encodedName) + 1 + 15 // name + : + avg value
		if !field.omitEmpty {
			cache.estimatedSize += 1 // comma
		}
	}

	return cache
}

func foldName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
