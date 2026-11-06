package syscfg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// block builds a syscfg block of short entries and long ones, laid out as
// an iPhone 8's: the long values at the end of the block.
func block(short map[string]string, long map[string]string) []byte {
	const size = 512
	b := make([]byte, size)
	copy(b, "gfCS")
	binary.LittleEndian.PutUint32(b[4:], size)
	binary.LittleEndian.PutUint32(b[8:], size)
	binary.LittleEndian.PutUint32(b[12:], 0x20002)
	binary.LittleEndian.PutUint32(b[20:], uint32(len(short)+len(long)))
	rev := func(s string) []byte { return []byte{s[3], s[2], s[1], s[0]} }
	off, end := headerSize, size
	for tag, v := range short {
		copy(b[off:], rev(tag))
		copy(b[off+4:off+20], v)
		off += entrySize
	}
	for tag, v := range long {
		end -= len(v)
		copy(b[end:], v)
		copy(b[off:], rev("CNTB"))
		copy(b[off+4:], rev(tag))
		binary.LittleEndian.PutUint32(b[off+8:], uint32(len(v)))
		binary.LittleEndian.PutUint32(b[off+12:], uint32(end))
		off += entrySize
	}
	return b
}

func TestParse(t *testing.T) {
	b := block(map[string]string{"SrNm": "FFNZMYBEJC6C", "Mod#": "MQ722", "Regn": "LL/A"},
		map[string]string{"Batt": "FG99425SCDAHXYDA2\x00", "LCM#": "DKH943426BXHNK0A7+S12657P0BPTAA1J43"})
	v, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	for tag, want := range map[string]string{
		"SrNm": "FFNZMYBEJC6C", "Mod#": "MQ722", "Regn": "LL/A",
		"Batt": "FG99425SCDAHXYDA2", "LCM#": "DKH943426BXHNK0A7+S12657P0BPTAA1J43",
	} {
		if got, ok := Text(v[tag]); !ok || got != want {
			t.Errorf("%s = %q, want %q", tag, got, want)
		}
	}
	if len(v["SrNm"]) != 16 {
		t.Errorf("a short value is its 16 bytes, got %d", len(v["SrNm"]))
	}
}

func TestParseRejects(t *testing.T) {
	if _, err := Parse([]byte("not a block at all, not at all")); !errors.Is(err, ErrNotSysCfg) {
		t.Errorf("garbage: %v", err)
	}
	b := block(nil, map[string]string{"Batt": "FG99425SCDAHXYDA2"})
	binary.LittleEndian.PutUint32(b[headerSize+12:], 1<<30) // the value's offset
	if _, err := Parse(b); err == nil {
		t.Error("an entry pointing outside the block parsed")
	}
	b = block(nil, nil)
	binary.LittleEndian.PutUint32(b[20:], 1000) // more entries than fit
	if _, err := Parse(b); err == nil {
		t.Error("an entry count beyond the block parsed")
	}
}

func TestText(t *testing.T) {
	if _, ok := Text([]byte{0x3c, 0xcd, 0x36, 0x12}); ok {
		t.Error("a MAC address read as text")
	}
	if s, ok := Text(bytes.Repeat([]byte{0}, 16)); ok {
		t.Errorf("zeros read as %q", s)
	}
}
