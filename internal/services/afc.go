package services

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// AFC (Apple File Conduit) is the file protocol of com.apple.afc and of
// com.apple.crashreportcopymobile. Only reading is implemented. The packet
// layout follows go-ios (MIT) and libimobiledevice.

const (
	afcMagic      = 0x4141504c36414643 // "CFA6LPAA"
	afcHeaderSize = 40
	afcMaxPacket  = 64 << 20
)

// AFC operations, and the read-only open mode.
const (
	afcStatus     = 0x01
	afcReadDir    = 0x03
	afcFileInfo   = 0x0A
	afcFileOpen   = 0x0D
	afcFileRead   = 0x0F
	afcFileClose  = 0x14
	afcModeRdOnly = 0x01
)

// AFCError is a non-zero AFC status.
type AFCError uint64

func (e AFCError) Error() string { return fmt.Sprintf("afc status %d", uint64(e)) }

// AFC is an AFC client over a service connection.
type AFC struct {
	c   net.Conn
	num uint64
	// broken is set when an exchange failed mid-way; see roundTrip.
	broken bool
}

// NewAFC wraps a connection to an AFC service.
func NewAFC(c net.Conn) *AFC { return &AFC{c: c} }

// Close closes the connection.
func (a *AFC) Close() error { return a.c.Close() }

// roundTrip sends one request and reads its reply. A transport error, or a
// reply to another packet, leaves the stream out of step, so the connection
// is not used again.
func (a *AFC) roundTrip(op uint64, hdr []byte) (hdrOut, payloadOut []byte, err error) {
	if a.broken {
		return nil, nil, errServiceBroken
	}
	hdrOut, payloadOut, err = a.exchange(op, hdr)
	var status AFCError
	if err != nil && !errors.As(err, &status) {
		a.broken = true
	}
	return hdrOut, payloadOut, err
}

// exchange sends a request, which as the client only reads is a header
// without payload, and reads the reply.
func (a *AFC) exchange(op uint64, hdr []byte) (hdrOut, payloadOut []byte, err error) {
	_ = a.c.SetDeadline(time.Now().Add(30 * time.Second))
	defer a.c.SetDeadline(time.Time{})
	a.num++
	this := uint64(afcHeaderSize + len(hdr))
	buf := make([]byte, afcHeaderSize, int(this))
	binary.LittleEndian.PutUint64(buf[0:], afcMagic)
	binary.LittleEndian.PutUint64(buf[8:], this)
	binary.LittleEndian.PutUint64(buf[16:], this)
	binary.LittleEndian.PutUint64(buf[24:], a.num)
	binary.LittleEndian.PutUint64(buf[32:], op)
	buf = append(buf, hdr...)
	if _, err := a.c.Write(buf); err != nil {
		return nil, nil, err
	}

	var h [afcHeaderSize]byte
	if _, err := io.ReadFull(a.c, h[:]); err != nil {
		return nil, nil, err
	}
	if binary.LittleEndian.Uint64(h[0:]) != afcMagic {
		return nil, nil, fmt.Errorf("afc: bad magic")
	}
	entire := binary.LittleEndian.Uint64(h[8:])
	thisLen := binary.LittleEndian.Uint64(h[16:])
	if num := binary.LittleEndian.Uint64(h[24:]); num != a.num {
		return nil, nil, fmt.Errorf("afc: reply to packet %d, expected %d", num, a.num)
	}
	rop := binary.LittleEndian.Uint64(h[32:])
	if thisLen < afcHeaderSize || entire < thisLen || entire > afcMaxPacket {
		return nil, nil, fmt.Errorf("afc: bad packet lengths %d/%d", thisLen, entire)
	}
	hdrOut = make([]byte, thisLen-afcHeaderSize)
	payloadOut = make([]byte, entire-thisLen)
	if _, err := io.ReadFull(a.c, hdrOut); err != nil {
		return nil, nil, err
	}
	if _, err := io.ReadFull(a.c, payloadOut); err != nil {
		return nil, nil, err
	}
	if rop == afcStatus {
		if len(hdrOut) < 8 {
			return nil, nil, fmt.Errorf("afc: short status")
		}
		if code := binary.LittleEndian.Uint64(hdrOut); code != 0 {
			return nil, nil, AFCError(code)
		}
	}
	return hdrOut, payloadOut, nil
}

func cstrings(b []byte) []string {
	var out []string
	for _, p := range bytes.Split(b, []byte{0}) {
		if len(p) > 0 {
			out = append(out, string(p))
		}
	}
	return out
}

// ReadDir lists a directory, without "." and "..".
func (a *AFC) ReadDir(path string) ([]string, error) {
	_, payload, err := a.roundTrip(afcReadDir, append([]byte(path), 0))
	if err != nil {
		return nil, fmt.Errorf("afc readdir %s: %w", path, err)
	}
	var names []string
	for _, n := range cstrings(payload) {
		if n != "." && n != ".." {
			names = append(names, n)
		}
	}
	return names, nil
}

// AFCFileInfo is the part of a file's information the inspection uses.
type AFCFileInfo struct {
	Size  int64
	MTime time.Time
	IsDir bool
}

// Stat returns information about a path.
func (a *AFC) Stat(path string) (AFCFileInfo, error) {
	_, payload, err := a.roundTrip(afcFileInfo, append([]byte(path), 0))
	if err != nil {
		return AFCFileInfo{}, fmt.Errorf("afc stat %s: %w", path, err)
	}
	kv := cstrings(payload)
	var fi AFCFileInfo
	for i := 0; i+1 < len(kv); i += 2 {
		switch kv[i] {
		case "st_size":
			fi.Size, _ = strconv.ParseInt(kv[i+1], 10, 64)
		case "st_mtime":
			ns, _ := strconv.ParseInt(kv[i+1], 10, 64)
			fi.MTime = time.Unix(0, ns)
		case "st_ifmt":
			fi.IsDir = kv[i+1] == "S_IFDIR"
		}
	}
	return fi, nil
}

// ReadFile reads a whole file, up to max bytes.
func (a *AFC) ReadFile(path string, max int64) ([]byte, error) {
	hdr := make([]byte, 8, 9+len(path))
	binary.LittleEndian.PutUint64(hdr, afcModeRdOnly)
	hdr = append(append(hdr, path...), 0)
	h, _, err := a.roundTrip(afcFileOpen, hdr)
	if err != nil {
		return nil, fmt.Errorf("afc open %s: %w", path, err)
	}
	if len(h) < 8 {
		return nil, fmt.Errorf("afc open %s: no handle", path)
	}
	handle := binary.LittleEndian.Uint64(h)
	defer func() {
		var ch [8]byte
		binary.LittleEndian.PutUint64(ch[:], handle)
		_, _, _ = a.roundTrip(afcFileClose, ch[:])
	}()
	var out []byte
	const chunk = 1 << 20
	for int64(len(out)) < max {
		var rh [16]byte
		binary.LittleEndian.PutUint64(rh[0:], handle)
		binary.LittleEndian.PutUint64(rh[8:], chunk)
		_, data, err := a.roundTrip(afcFileRead, rh[:])
		if err != nil {
			return out, fmt.Errorf("afc read %s: %w", path, err)
		}
		if len(data) == 0 {
			break
		}
		out = append(out, data...)
	}
	return out, nil
}
