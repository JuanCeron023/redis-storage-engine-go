package store

import (
	"strconv"
)

// parseInt parses a base-10 integer stored as bytes.
func parseInt(b []byte) (int64, error) {
	return strconv.ParseInt(string(b), 10, 64)
}

// formatInt renders an int64 as a base-10 string.
func formatInt(n int64) string {
	return strconv.FormatInt(n, 10)
}
