// Package plistx holds helpers for the loosely typed values that come out of
// property lists (map[string]any trees decoded by howett.net/plist), and the
// 4-byte big-endian length framing that lockdown and most lockdown services
// use on the wire.
package plistx

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"howett.net/plist"
)

// MaxMessage bounds a single framed message so a corrupt length cannot make
// us allocate gigabytes.
const MaxMessage = 64 << 20

// WriteFramed encodes v as an XML plist and writes it with a 4-byte
// big-endian length prefix.
func WriteFramed(w io.Writer, v any) error {
	body, err := plist.Marshal(v, plist.XMLFormat)
	if err != nil {
		return fmt.Errorf("encode plist: %w", err)
	}
	buf := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(buf, uint32(len(body)))
	copy(buf[4:], body)
	_, err = w.Write(buf)
	return err
}

// ReadFramed reads one length-prefixed plist and decodes it into a map.
func ReadFramed(r io.Reader) (map[string]any, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxMessage {
		return nil, fmt.Errorf("plist message of %d bytes exceeds limit", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return Decode(body)
}

// Decode decodes a plist (XML or binary) whose root is a dictionary.
func Decode(body []byte) (map[string]any, error) {
	var m map[string]any
	if _, err := plist.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("decode plist: %w", err)
	}
	if m == nil {
		return nil, errors.New("plist root is not a dictionary")
	}
	return m, nil
}

// String returns v as a string. Byte slices holding text (as IORegistry's
// device tree properties, NUL terminated) are converted too.
func String(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case []byte:
		s := strings.TrimRight(string(x), "\x00")
		if !utf8.ValidString(s) {
			return "", false
		}
		for _, r := range s {
			if r < 0x20 && r != '\t' {
				return "", false
			}
		}
		return s, true
	}
	return "", false
}

// Int returns v as an int64, accepting every numeric kind plist decoding
// produces.
func Int(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		return int64(x), true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		// Signed quantities (a battery's amperage) arrive as two's
		// complement in unsigned integers.
		return int64(x), true
	case float32:
		return int64(x), true
	case float64:
		return int64(x), true
	}
	return 0, false
}

// Bool returns v as a bool.
func Bool(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

// Dict returns v as a dictionary.
func Dict(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// Path walks nested dictionaries: Path(m, "Diagnostics", "IORegistry").
func Path(m map[string]any, keys ...string) (any, bool) {
	var cur any = m
	for _, k := range keys {
		d, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = d[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}
