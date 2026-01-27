package hvjson

import (
	"fmt"
	"strings"
)

type ErrorCode int

const (
	ErrorInvalidJSON ErrorCode = iota
	ErrorInvalidField
	ErrorInvalidValue
	ErrorTypeMismatch
	ErrorStackOverflow
	ErrorInvalidUTF8
	ErrorInvalidNumber
	ErrorInvalidString
)

var errorCodeStrings = map[ErrorCode]string{
	ErrorInvalidJSON:   "invalid JSON",
	ErrorInvalidField:  "invalid field",
	ErrorInvalidValue:  "invalid value",
	ErrorTypeMismatch:  "type mismatch",
	ErrorStackOverflow: "stack overflow",
	ErrorInvalidUTF8:   "invalid UTF-8",
	ErrorInvalidNumber: "invalid number",
	ErrorInvalidString: "invalid string",
}

type SyntaxError struct {
	Pos     int
	Line    int
	Column  int
	Src     string
	Code    ErrorCode
	Message string
}

func (e *SyntaxError) Error() string {
	codeStr := errorCodeStrings[e.Code]
	if codeStr == "" {
		codeStr = "unknown error"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("hvjson: %s at position %d", codeStr, e.Pos))

	if e.Line > 0 && e.Column > 0 {
		b.WriteString(fmt.Sprintf(" (line %d, column %d)", e.Line, e.Column))
	}

	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}

	return b.String()
}

func NewSyntaxError(src []byte, pos int, code ErrorCode, message string) *SyntaxError {
	line := 1
	column := 1

	for i := 0; i < pos && i < len(src); i++ {
		if src[i] == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}

	return &SyntaxError{
		Pos:     pos,
		Line:    line,
		Column:  column,
		Src:     string(src),
		Code:    code,
		Message: message,
	}
}

func WrapSIMDError(src []byte, err error, code ErrorCode) error {
	if err == nil {
		return nil
	}
	return NewSyntaxError(src, 0, code, err.Error())
}
