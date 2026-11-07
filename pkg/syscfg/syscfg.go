// Package syscfg decodes an iPhone's factory configuration (syscfg), the
// block the factory writes with the serial numbers of the parts it fitted,
// as MobileGestalt's SysCfg key returns it.
//
// The block starts with a 24-byte header, "SCfg" stored little-endian
// ("gfCS"), then the total size, the maximum size, the version, an
// endianness flag and the number of entries. Each entry is 20 bytes: a
// four-character tag, stored reversed, and 16 bytes of value. A longer value
// is an entry tagged "CNTB" holding the real tag, the value's length and its
// offset from the start of the block.
package syscfg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	headerSize = 24
	entrySize  = 20
)

var magic = []byte("gfCS")

// ErrNotSysCfg means the data does not start with syscfg's header.
var ErrNotSysCfg = errors.New("syscfg: not a syscfg block")

// Parse returns the block's values by tag, such as "SrNm" (the serial
// number), "Batt" (the battery's) or "FCMS" (the front camera module's).
func Parse(b []byte) (map[string][]byte, error) {
	if len(b) < headerSize || !bytes.Equal(b[:4], magic) {
		return nil, ErrNotSysCfg
	}
	n := int(binary.LittleEndian.Uint32(b[20:24]))
	if n < 0 || headerSize+n*entrySize > len(b) {
		return nil, fmt.Errorf("syscfg: %d entries do not fit in %d bytes", n, len(b))
	}
	out := make(map[string][]byte, n)
	for i := range n {
		e := b[headerSize+i*entrySize : headerSize+(i+1)*entrySize]
		tag := reversed(e[:4])
		if tag != "CNTB" {
			out[tag] = bytes.Clone(e[4:])
			continue
		}
		tag = reversed(e[4:8])
		size := int(binary.LittleEndian.Uint32(e[8:12]))
		at := int(binary.LittleEndian.Uint32(e[12:16]))
		if size < 0 || at < 0 || at+size > len(b) || at+size < at {
			return nil, fmt.Errorf("syscfg: %s: %d bytes at %d lie outside the block", tag, size, at)
		}
		out[tag] = bytes.Clone(b[at : at+size])
	}
	return out, nil
}

func reversed(b []byte) string {
	r := make([]byte, len(b))
	for i, c := range b {
		r[len(b)-1-i] = c
	}
	return string(r)
}

// Text returns a value as text, without its trailing NULs, when it is all
// printable ASCII.
func Text(v []byte) (string, bool) {
	v = bytes.TrimRight(v, "\x00")
	if len(v) == 0 {
		return "", false
	}
	for _, c := range v {
		if c < 0x20 || c > 0x7e {
			return "", false
		}
	}
	return string(v), true
}
