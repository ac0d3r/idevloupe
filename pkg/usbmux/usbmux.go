// Package usbmux is a usbmuxd client. The wire protocol follows go-ios
// (github.com/danielpaulus/go-ios, MIT) and libimobiledevice.
package usbmux

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"howett.net/plist"

	"github.com/ac0d3r/idevloupe/pkg/plistx"
)

const (
	headerSize     = 16
	versionPlist   = 1
	messagePlist   = 8
	libUSBMuxVer   = 3
	dialTimeout    = 5 * time.Second
	requestTimeout = 15 * time.Second
)

const (
	resultOK          = 0
	resultBadCommand  = 1
	resultBadDevice   = 2
	ResultConnRefused = 3
	resultBadVersion  = 6
)

var ErrNotPaired = errors.New("usbmux: no pair record for device")

type ResultError struct {
	Op   string
	Code int64
}

func (e *ResultError) Error() string {
	name := map[int64]string{
		resultBadCommand:  "bad command",
		resultBadDevice:   "device not connected",
		ResultConnRefused: "connection refused by device",
		resultBadVersion:  "bad protocol version",
	}[e.Code]
	if name == "" {
		name = "unknown error"
	}
	return fmt.Sprintf("usbmux %s: %s (%d)", e.Op, name, e.Code)
}

type Device struct {
	DeviceID       int
	UDID           string
	ConnectionType string
}

func Address() (network, addr string) {
	if v := os.Getenv("USBMUXD_SOCKET_ADDRESS"); v != "" {
		if scheme, rest, ok := strings.Cut(v, "://"); ok {
			return strings.ToLower(scheme), rest
		}
		if strings.Contains(v, ":") {
			return "tcp", v
		}
		return "unix", v
	}
	if runtime.GOOS == "windows" {
		return "tcp", "127.0.0.1:27015"
	}
	return "unix", "/var/run/usbmuxd"
}

type Conn struct {
	c   net.Conn
	tag uint32
}

var ErrNoDaemon = errors.New("usbmuxd is not reachable")

func Dial() (*Conn, error) {
	network, addr := Address()
	c, err := net.DialTimeout(network, addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w (%s %s): %w", ErrNoDaemon, network, addr, err)
	}
	return &Conn{c: c}, nil
}

// ProgName and BundleID are sent to usbmuxd; set them before the first connection.
var (
	ProgName = "usbmux"
	BundleID = ""
)

func (m *Conn) Close() error { return m.c.Close() }

func (m *Conn) send(msg map[string]any) error {
	msg["ClientVersionString"] = ProgName + "-1.0"
	msg["ProgName"] = ProgName
	if BundleID != "" {
		msg["BundleID"] = BundleID
	}
	msg["kLibUSBMuxVersion"] = libUSBMuxVer
	body, err := plist.Marshal(msg, plist.XMLFormat)
	if err != nil {
		return err
	}
	m.tag++
	buf := make([]byte, headerSize+len(body))
	binary.LittleEndian.PutUint32(buf[0:], uint32(headerSize+len(body)))
	binary.LittleEndian.PutUint32(buf[4:], versionPlist)
	binary.LittleEndian.PutUint32(buf[8:], messagePlist)
	binary.LittleEndian.PutUint32(buf[12:], m.tag)
	copy(buf[headerSize:], body)
	_, err = m.c.Write(buf)
	return err
}

func (m *Conn) read() (map[string]any, error) {
	var hdr [headerSize]byte
	if _, err := io.ReadFull(m.c, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[0:])
	if n < headerSize || n > plistx.MaxMessage {
		return nil, fmt.Errorf("usbmux: invalid message length %d", n)
	}
	body := make([]byte, n-headerSize)
	if _, err := io.ReadFull(m.c, body); err != nil {
		return nil, err
	}
	return plistx.Decode(body)
}

func (m *Conn) request(msg map[string]any) (map[string]any, error) {
	_ = m.c.SetDeadline(time.Now().Add(requestTimeout))
	defer m.c.SetDeadline(time.Time{})
	if err := m.send(msg); err != nil {
		return nil, err
	}
	return m.read()
}

func checkResult(op string, resp map[string]any) error {
	if t, _ := resp["MessageType"].(string); t != "Result" {
		return nil
	}
	code, _ := plistx.Int(resp["Number"])
	if code != resultOK {
		return &ResultError{Op: op, Code: code}
	}
	return nil
}

func parseDevice(m map[string]any) Device {
	props, _ := plistx.Dict(m["Properties"])
	var d Device
	id, _ := plistx.Int(m["DeviceID"])
	d.DeviceID = int(id)
	if props != nil {
		// Newer usbmuxd reports UDID; older ones put it in SerialNumber.
		d.UDID, _ = props["UDID"].(string)
		if d.UDID == "" {
			d.UDID, _ = props["SerialNumber"].(string)
		}
		d.ConnectionType, _ = props["ConnectionType"].(string)
		if d.DeviceID == 0 {
			id, _ = plistx.Int(props["DeviceID"])
			d.DeviceID = int(id)
		}
	}
	return d
}

func (m *Conn) ListDevices() ([]Device, error) {
	resp, err := m.request(map[string]any{"MessageType": "ListDevices"})
	if err != nil {
		return nil, err
	}
	if err := checkResult("ListDevices", resp); err != nil {
		return nil, err
	}
	list, _ := resp["DeviceList"].([]any)
	var usb, other []Device
	for _, e := range list {
		em, ok := plistx.Dict(e)
		if !ok {
			continue
		}
		d := parseDevice(em)
		if d.UDID == "" {
			continue
		}
		if d.ConnectionType == "USB" {
			usb = append(usb, d)
		} else {
			other = append(other, d)
		}
	}
	return append(usb, other...), nil
}

func (m *Conn) ReadBUID() (string, error) {
	resp, err := m.request(map[string]any{"MessageType": "ReadBUID"})
	if err != nil {
		return "", err
	}
	if err := checkResult("ReadBUID", resp); err != nil {
		return "", err
	}
	buid, _ := resp["BUID"].(string)
	if buid == "" {
		return "", errors.New("usbmux: empty BUID")
	}
	return buid, nil
}

type PairRecord struct {
	HostID            string
	SystemBUID        string
	HostCertificate   []byte
	HostPrivateKey    []byte
	DeviceCertificate []byte
	RootCertificate   []byte
	RootPrivateKey    []byte
	EscrowBag         []byte
	WiFiMACAddress    string
}

func (m *Conn) ReadPairRecord(udid string) (*PairRecord, error) {
	resp, err := m.request(map[string]any{"MessageType": "ReadPairRecord", "PairRecordID": udid})
	if err != nil {
		return nil, err
	}
	data, ok := resp["PairRecordData"].([]byte)
	if !ok {
		// On failure usbmuxd answers a Result instead.
		return nil, ErrNotPaired
	}
	var rec PairRecord
	if _, err := plist.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("decode pair record: %w", err)
	}
	if rec.HostID == "" || len(rec.HostCertificate) == 0 || len(rec.HostPrivateKey) == 0 {
		return nil, ErrNotPaired
	}
	return &rec, nil
}

func (m *Conn) SavePairRecord(udid string, deviceID int, rec *PairRecord) error {
	data, err := plist.Marshal(rec, plist.XMLFormat)
	if err != nil {
		return err
	}
	resp, err := m.request(map[string]any{
		"MessageType":    "SavePairRecord",
		"PairRecordID":   udid,
		"PairRecordData": data,
		"DeviceID":       deviceID,
	})
	if err != nil {
		return err
	}
	return checkResult("SavePairRecord", resp)
}

// Connect turns m into a raw stream to the device port; m must not be used afterwards.
func (m *Conn) Connect(deviceID int, port uint16) (net.Conn, error) {
	// usbmuxd expects the port in network byte order.
	swapped := port<<8 | port>>8
	resp, err := m.request(map[string]any{
		"MessageType": "Connect",
		"DeviceID":    deviceID,
		"PortNumber":  swapped,
	})
	if err != nil {
		return nil, err
	}
	if err := checkResult("Connect", resp); err != nil {
		return nil, err
	}
	return m.c, nil
}

func ConnectPort(deviceID int, port uint16) (net.Conn, error) {
	m, err := Dial()
	if err != nil {
		return nil, err
	}
	c, err := m.Connect(deviceID, port)
	if err != nil {
		m.Close()
		return nil, err
	}
	return c, nil
}

func ListDevices() ([]Device, error) {
	m, err := Dial()
	if err != nil {
		return nil, err
	}
	defer m.Close()
	return m.ListDevices()
}

type Event struct {
	Attached bool
	Device   Device // for detach, only DeviceID is set
}

func Listen(ctx context.Context) (<-chan Event, error) {
	m, err := Dial()
	if err != nil {
		return nil, err
	}
	resp, err := m.request(map[string]any{"MessageType": "Listen"})
	if err != nil {
		m.Close()
		return nil, err
	}
	if err := checkResult("Listen", resp); err != nil {
		m.Close()
		return nil, err
	}
	ch := make(chan Event, 16)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			m.Close()
		case <-done:
		}
	}()
	go func() {
		defer close(ch)
		defer close(done)
		defer m.Close()
		for {
			msg, err := m.read()
			if err != nil {
				return
			}
			var ev Event
			switch msg["MessageType"] {
			case "Attached":
				ev = Event{Attached: true, Device: parseDevice(msg)}
			case "Detached":
				id, _ := plistx.Int(msg["DeviceID"])
				ev = Event{Device: Device{DeviceID: int(id)}}
			default:
				continue
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
