package hvjson

import "sync"

const (
	smallBufferSize  = 1024
	mediumBufferSize = 65536
	largeBufferSize  = 1048576
)

type bufferPool struct {
	small  sync.Pool
	medium sync.Pool
	large  sync.Pool
}

var globalBufferPool = &bufferPool{
	small: sync.Pool{
		New: func() interface{} {
			b := make([]byte, 0, smallBufferSize)
			return &b
		},
	},
	medium: sync.Pool{
		New: func() interface{} {
			b := make([]byte, 0, mediumBufferSize)
			return &b
		},
	},
	large: sync.Pool{
		New: func() interface{} {
			b := make([]byte, 0, largeBufferSize)
			return &b
		},
	},
}

func getBuffer(size int) []byte {
	if size <= smallBufferSize {
		bufPtr := globalBufferPool.small.Get().(*[]byte)
		buf := *bufPtr
		return buf[:0]
	} else if size <= mediumBufferSize {
		bufPtr := globalBufferPool.medium.Get().(*[]byte)
		buf := *bufPtr
		return buf[:0]
	} else if size <= largeBufferSize {
		bufPtr := globalBufferPool.large.Get().(*[]byte)
		buf := *bufPtr
		return buf[:0]
	}
	return make([]byte, 0, size)
}

func putBuffer(buf []byte) {
	capacity := cap(buf)
	if capacity > largeBufferSize*2 {
		return
	}
	buf = buf[:0]
	if capacity <= smallBufferSize {
		globalBufferPool.small.Put(&buf)
	} else if capacity <= mediumBufferSize {
		globalBufferPool.medium.Put(&buf)
	} else if capacity <= largeBufferSize {
		globalBufferPool.large.Put(&buf)
	}
}

type Buffer struct {
	buf []byte
}

func NewBuffer(size int) *Buffer {
	return &Buffer{buf: getBuffer(size)}
}

func (b *Buffer) Bytes() []byte {
	return b.buf
}

func (b *Buffer) Write(p []byte) (n int, err error) {
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *Buffer) WriteByte(c byte) error {
	b.buf = append(b.buf, c)
	return nil
}

func (b *Buffer) WriteString(s string) (n int, err error) {
	b.buf = append(b.buf, s...)
	return len(s), nil
}

func (b *Buffer) Release() {
	if b.buf != nil {
		putBuffer(b.buf)
		b.buf = nil
	}
}
