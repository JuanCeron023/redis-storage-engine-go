// Package resp implements the Redis Serialization Protocol (RESP2).
//
// Implementing RESP directly means CacheNova is wire-compatible with the
// standard `redis-cli` and every existing Redis client library, without
// pulling in a Redis dependency. The parser handles the five RESP types:
// simple strings (+), errors (-), integers (:), bulk strings ($), and
// arrays (*), which is everything needed to serve real client traffic.
package resp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Value is a decoded RESP value. Only the field matching Type is meaningful.
type Value struct {
	Type  byte // '+', '-', ':', '$', '*'
	Str   string
	Int   int64
	Bulk  []byte
	Array []Value
	IsNil bool
}

// ErrProtocol indicates a malformed RESP stream.
var ErrProtocol = errors.New("resp: protocol error")

// Reader decodes RESP values from a buffered stream.
type Reader struct{ r *bufio.Reader }

// NewReader wraps rd in a RESP reader.
func NewReader(rd io.Reader) *Reader { return &Reader{r: bufio.NewReader(rd)} }

func (r *Reader) readLine() ([]byte, error) {
	line, err := r.r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	// Strip trailing CRLF.
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, ErrProtocol
	}
	return line[:len(line)-2], nil
}

// ReadValue decodes a single RESP value. Inline commands are also accepted so
// a human typing into `nc`/`telnet` still works.
func (r *Reader) ReadValue() (Value, error) {
	prefix, err := r.r.ReadByte()
	if err != nil {
		return Value{}, err
	}
	switch prefix {
	case '+':
		line, err := r.readLine()
		return Value{Type: '+', Str: string(line)}, err
	case '-':
		line, err := r.readLine()
		return Value{Type: '-', Str: string(line)}, err
	case ':':
		line, err := r.readLine()
		if err != nil {
			return Value{}, err
		}
		n, err := strconv.ParseInt(string(line), 10, 64)
		return Value{Type: ':', Int: n}, err
	case '$':
		return r.readBulk()
	case '*':
		return r.readArray()
	default:
		// Treat as an inline command: read the rest of the line and split.
		_ = r.r.UnreadByte()
		line, err := r.readLine()
		if err != nil {
			return Value{}, err
		}
		return inlineToArray(line), nil
	}
}

func (r *Reader) readBulk() (Value, error) {
	line, err := r.readLine()
	if err != nil {
		return Value{}, err
	}
	n, err := strconv.Atoi(string(line))
	if err != nil {
		return Value{}, ErrProtocol
	}
	if n < 0 {
		return Value{Type: '$', IsNil: true}, nil
	}
	buf := make([]byte, n+2) // include CRLF
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return Value{}, err
	}
	return Value{Type: '$', Bulk: buf[:n]}, nil
}

func (r *Reader) readArray() (Value, error) {
	line, err := r.readLine()
	if err != nil {
		return Value{}, err
	}
	n, err := strconv.Atoi(string(line))
	if err != nil {
		return Value{}, ErrProtocol
	}
	if n < 0 {
		return Value{Type: '*', IsNil: true}, nil
	}
	arr := make([]Value, n)
	for i := 0; i < n; i++ {
		v, err := r.ReadValue()
		if err != nil {
			return Value{}, err
		}
		arr[i] = v
	}
	return Value{Type: '*', Array: arr}, nil
}

func inlineToArray(line []byte) Value {
	fields := splitFields(string(line))
	arr := make([]Value, len(fields))
	for i, f := range fields {
		arr[i] = Value{Type: '$', Bulk: []byte(f)}
	}
	return Value{Type: '*', Array: arr}
}

func splitFields(s string) []string {
	var out []string
	cur := ""
	for _, ch := range s {
		if ch == ' ' || ch == '\t' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(ch)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// --- Encoding helpers -------------------------------------------------------

// EncodeSimpleString encodes "+OK\r\n"-style responses.
func EncodeSimpleString(s string) []byte { return []byte("+" + s + "\r\n") }

// EncodeError encodes "-ERR ...\r\n" responses.
func EncodeError(s string) []byte { return []byte("-" + s + "\r\n") }

// EncodeInt encodes ":123\r\n" responses.
func EncodeInt(n int64) []byte { return []byte(fmt.Sprintf(":%d\r\n", n)) }

// EncodeBulk encodes a bulk string, or a nil bulk when b is nil.
func EncodeBulk(b []byte) []byte {
	if b == nil {
		return []byte("$-1\r\n")
	}
	return []byte(fmt.Sprintf("$%d\r\n%s\r\n", len(b), b))
}

// EncodeArray encodes a RESP array of already-encoded elements.
func EncodeArray(elements [][]byte) []byte {
	out := []byte(fmt.Sprintf("*%d\r\n", len(elements)))
	for _, e := range elements {
		out = append(out, e...)
	}
	return out
}
