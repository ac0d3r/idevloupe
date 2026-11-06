// Package fakedevice is a fake usbmuxd with one fake iPhone behind it,
// speaking the wire protocols the real ones do (usbmux framing, lockdown,
// TLS sessions, pairing with a trust dialog, diagnostics_relay,
// installation_proxy, MCInstall). Tests point USBMUXD_SOCKET_ADDRESS at it.
package fakedevice

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	"howett.net/plist"

	"github.com/ac0d3r/idevloupe/pkg/plistx"
)

// Device is the fake phone's data.
type Device struct {
	UDID     string
	Root     map[string]any
	Domains  map[string]map[string]any
	Battery  map[string]any
	Platform map[string]any
	Apps     []map[string]any
	// Paired starts the device with a pair record in usbmuxd.
	Paired bool
	// PendingPolls is how many Pair requests answer
	// PairingDialogResponsePending before the user taps Trust.
	PendingPolls   int
	JailbrokenAFC2 bool
	// GasGauge is the "All" diagnostics' battery section.
	GasGauge map[string]any
	// Profiles is MCInstall's ProfileMetadata.
	Profiles map[string]any
	// EscrowLocked makes StartService demand the escrow bag.
	EscrowLocked bool
	// RefuseDiagnostics is how many connections to diagnostics_relay the
	// device refuses before it accepts one, as right after another client.
	RefuseDiagnostics int
	// Planes are whole IORegistry planes, by name.
	Planes map[string]map[string]any
	// CrashFiles are the files crashreportcopymobile serves, by path.
	CrashFiles map[string][]byte
	// LateReplyDomain makes the reply to this domain arrive twice, as a
	// reply that came after the client's timeout does.
	LateReplyDomain string
	// Hidden are root keys left out of the whole-domain answer, as
	// lockdownd does for some.
	Hidden map[string]any

	mu         sync.Mutex
	record     []byte // pair record data saved in usbmuxd
	hostID     string
	pairPolls  int
	ports      map[uint16]string
	nextPort   uint16
	serverCert certPair
	ln         net.Listener
	// Requests records lockdown requests, for assertions.
	Requests []string
	// PairHostIDs records the HostID of each Pair request.
	PairHostIDs []string
}

// Start listens on a loopback TCP port, as usbmuxd does on Windows, which
// every system has, and returns its address for USBMUXD_SOCKET_ADDRESS.
func (d *Device) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	d.ln = ln
	d.ports = map[uint16]string{}
	d.nextPort = 50000
	d.serverCert = selfSigned()
	if d.Paired {
		host := selfSigned()
		d.hostID = "HOST-ID-1"
		rec := map[string]any{
			"HostID": d.hostID, "SystemBUID": "BUID-1",
			"HostCertificate": host.certPEM, "HostPrivateKey": host.keyPEM,
			"DeviceCertificate": d.serverCert.certPEM, "RootCertificate": host.certPEM, "RootPrivateKey": host.keyPEM,
		}
		d.record, _ = plist.Marshal(rec, plist.XMLFormat)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serveMux(c)
		}
	}()
	return "tcp://" + ln.Addr().String(), nil
}

// Close stops the server.
func (d *Device) Close() {
	d.ln.Close()
}

type certPair struct {
	tls.Certificate
	certPEM, keyPEM []byte
}

func selfSigned() certPair {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fake"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	cp := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kp := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	c, _ := tls.X509KeyPair(cp, kp)
	return certPair{c, cp, kp}
}

func readMux(c net.Conn) (map[string]any, uint32, error) {
	var hdr [16]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return nil, 0, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	body := make([]byte, n-16)
	if _, err := io.ReadFull(c, body); err != nil {
		return nil, 0, err
	}
	m, err := plistx.Decode(body)
	return m, binary.LittleEndian.Uint32(hdr[12:]), err
}

func writeMux(c net.Conn, tag uint32, m map[string]any) {
	body, _ := plist.Marshal(m, plist.XMLFormat)
	buf := make([]byte, 16+len(body))
	binary.LittleEndian.PutUint32(buf[0:], uint32(16+len(body)))
	binary.LittleEndian.PutUint32(buf[4:], 1)
	binary.LittleEndian.PutUint32(buf[8:], 8)
	binary.LittleEndian.PutUint32(buf[12:], tag)
	copy(buf[16:], body)
	_, _ = c.Write(buf)
}

func result(n int) map[string]any { return map[string]any{"MessageType": "Result", "Number": n} }

func (d *Device) attached() map[string]any {
	return map[string]any{"MessageType": "Attached", "DeviceID": 1, "Properties": map[string]any{
		"DeviceID": 1, "SerialNumber": "USB-SERIAL-NOT-UDID", "UDID": d.UDID, "ConnectionType": "USB", "ProductID": 4776,
	}}
}

func (d *Device) serveMux(c net.Conn) {
	defer c.Close()
	for {
		m, tag, err := readMux(c)
		if err != nil {
			return
		}
		switch m["MessageType"] {
		case "ListDevices":
			writeMux(c, tag, map[string]any{"DeviceList": []any{d.attached()}})
		case "Listen":
			writeMux(c, tag, result(0))
			writeMux(c, 0, d.attached())
			_, _ = io.Copy(io.Discard, c)
			return
		case "ReadBUID":
			writeMux(c, tag, map[string]any{"BUID": "BUID-1"})
		case "ReadPairRecord":
			d.mu.Lock()
			rec := d.record
			d.mu.Unlock()
			if rec == nil {
				writeMux(c, tag, result(2))
			} else {
				writeMux(c, tag, map[string]any{"PairRecordData": rec})
			}
		case "SavePairRecord":
			data, _ := m["PairRecordData"].([]byte)
			d.mu.Lock()
			d.record = data
			d.mu.Unlock()
			writeMux(c, tag, result(0))
		case "Connect":
			p, _ := plistx.Int(m["PortNumber"])
			port := uint16(p)
			port = port<<8 | port>>8
			if port == 62078 {
				writeMux(c, tag, result(0))
				d.serveLockdown(c)
				return
			}
			d.mu.Lock()
			svc := d.ports[port]
			d.mu.Unlock()
			refuse := svc == "com.apple.mobile.diagnostics_relay" && d.RefuseDiagnostics > 0
			if refuse {
				d.mu.Lock()
				d.RefuseDiagnostics--
				d.mu.Unlock()
			}
			if svc == "" || refuse {
				writeMux(c, tag, result(3))
				continue
			}
			writeMux(c, tag, result(0))
			tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{d.serverCert.Certificate}})
			d.serveService(svc, tc)
			return
		default:
			writeMux(c, tag, result(1))
		}
	}
}

func (d *Device) serveLockdown(c net.Conn) {
	conn := c
	for {
		req, err := plistx.ReadFramed(conn)
		if err != nil {
			return
		}
		r, _ := req["Request"].(string)
		d.mu.Lock()
		d.Requests = append(d.Requests, r)
		d.mu.Unlock()
		resp := map[string]any{"Request": r}
		late := false
		switch r {
		case "QueryType":
			resp["Type"] = "com.apple.mobile.lockdown"
		case "GetValue":
			dom, _ := req["Domain"].(string)
			key, _ := req["Key"].(string)
			// lockdownd echoes what was asked.
			if dom != "" {
				resp["Domain"] = dom
			}
			if key != "" {
				resp["Key"] = key
			}
			late = dom != "" && dom == d.LateReplyDomain
			var src map[string]any
			if dom == "" {
				src = d.Root
			} else {
				src = d.Domains[dom]
			}
			switch {
			case src == nil:
				resp["Error"] = "MissingValue"
			case key == "":
				resp["Value"] = src
			case dom == "" && d.Hidden[key] != nil:
				resp["Value"] = d.Hidden[key]
			case key == "DevicePublicKey":
				leaf, _ := x509.ParseCertificate(d.serverCert.Certificate.Certificate[0])
				pub := leaf.PublicKey.(*rsa.PublicKey)
				resp["Value"] = pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(pub)})
			default:
				if v, ok := src[key]; ok {
					resp["Value"] = v
				} else {
					resp["Error"] = "MissingValue"
				}
			}
		case "Pair":
			d.mu.Lock()
			if pr, ok := plistx.Dict(req["PairRecord"]); ok {
				id, _ := pr["HostID"].(string)
				d.PairHostIDs = append(d.PairHostIDs, id)
			}
			d.pairPolls++
			pending := d.pairPolls <= d.PendingPolls
			if !pending {
				pr, _ := plistx.Dict(req["PairRecord"])
				d.hostID, _ = pr["HostID"].(string)
			}
			d.mu.Unlock()
			if pending {
				resp["Error"] = "PairingDialogResponsePending"
			} else {
				resp["EscrowBag"] = []byte("escrow")
			}
		case "StartSession":
			d.mu.Lock()
			ok := req["HostID"] == d.hostID && d.hostID != ""
			d.mu.Unlock()
			if !ok {
				resp["Error"] = "InvalidHostID"
				break
			}
			resp["SessionID"] = "SESSION"
			resp["EnableSessionSSL"] = true
			_ = plistx.WriteFramed(conn, resp)
			tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{d.serverCert.Certificate}})
			if err := tc.Handshake(); err != nil {
				return
			}
			conn = tc
			continue
		case "StartService":
			svc, _ := req["Service"].(string)
			if svc == "com.apple.afc2" && !d.JailbrokenAFC2 || svc == "com.apple.iosdiagnostics.relay" {
				resp["Error"] = "InvalidService"
				break
			}
			if _, has := req["EscrowBag"]; d.EscrowLocked && !has {
				resp["Error"] = "EscrowLocked"
				break
			}
			d.mu.Lock()
			d.nextPort++
			d.ports[d.nextPort] = svc
			resp["Port"] = d.nextPort
			d.mu.Unlock()
			resp["Service"] = svc
			resp["EnableServiceSSL"] = true
		case "StopSession":
		default:
			resp["Error"] = "InvalidRequest"
		}
		if err := plistx.WriteFramed(conn, resp); err != nil {
			return
		}
		if late {
			_ = plistx.WriteFramed(conn, resp)
		}
	}
}

func (d *Device) serveService(svc string, c net.Conn) {
	defer c.Close()
	switch svc {
	case "com.apple.crashreportmover":
		_, _ = c.Write([]byte("ping"))
		return
	case "com.apple.crashreportcopymobile":
		d.serveAFC(c)
		return
	}
	for {
		req, err := plistx.ReadFramed(c)
		if err != nil {
			return
		}
		switch svc {
		case "com.apple.mobile.diagnostics_relay":
			switch req["Request"] {
			case "IORegistry":
				var reg map[string]any
				switch req["EntryClass"] {
				case "IOPMPowerSource":
					reg = d.Battery
				case "IOPlatformExpertDevice":
					reg = d.Platform
				case nil:
					if plane, ok := req["CurrentPlane"].(string); ok && req["EntryName"] == nil {
						reg = d.Planes[plane]
					}
				}
				if reg == nil {
					_ = plistx.WriteFramed(c, map[string]any{"Status": "Failure"})
				} else {
					_ = plistx.WriteFramed(c, map[string]any{"Status": "Success", "Diagnostics": map[string]any{"IORegistry": reg}})
				}
			case "All":
				_ = plistx.WriteFramed(c, map[string]any{"Status": "Success", "Diagnostics": map[string]any{"GasGauge": d.GasGauge, "NAND": map[string]any{"Status": "Success"}}})
			case "MobileGestalt":
				_ = plistx.WriteFramed(c, map[string]any{"Status": "Success", "Diagnostics": map[string]any{"MobileGestalt": map[string]any{"Status": "MobileGestaltDeprecated"}}})
			case "Goodbye":
				_ = plistx.WriteFramed(c, map[string]any{"Status": "Success"})
				return
			}
		case "com.apple.mobile.installation_proxy":
			half := len(d.Apps) / 2
			list := func(a []map[string]any) []any {
				out := make([]any, len(a))
				for i := range a {
					out[i] = a[i]
				}
				return out
			}
			_ = plistx.WriteFramed(c, map[string]any{"Status": "BrowsingApplications", "CurrentList": list(d.Apps[:half])})
			_ = plistx.WriteFramed(c, map[string]any{"Status": "BrowsingApplications", "CurrentList": list(d.Apps[half:])})
			_ = plistx.WriteFramed(c, map[string]any{"Status": "Complete"})
		case "com.apple.mobile.MCInstall":
			switch req["RequestType"] {
			case "GetProfileList":
				ids := []any{}
				for id := range d.Profiles {
					ids = append(ids, id)
				}
				meta := d.Profiles
				if meta == nil {
					meta = map[string]any{}
				}
				_ = plistx.WriteFramed(c, map[string]any{"Status": "Acknowledged", "OrderedIdentifiers": ids, "ProfileMetadata": meta})
			case "GetCloudConfiguration":
				_ = plistx.WriteFramed(c, map[string]any{"Status": "Acknowledged", "CloudConfiguration": map[string]any{}})
			}
		default:
			return
		}
	}
}

// serveAFC answers the AFC requests the client sends: read a directory,
// stat, open, read and close.
func (d *Device) serveAFC(c net.Conn) {
	files := d.CrashFiles
	handles := map[uint64][]byte{}
	var next uint64 = 1
	for {
		var h [40]byte
		if _, err := io.ReadFull(c, h[:]); err != nil {
			return
		}
		entire := binary.LittleEndian.Uint64(h[8:])
		this := binary.LittleEndian.Uint64(h[16:])
		if binary.LittleEndian.Uint64(h[0:]) != 0x4141504c36414643 || this < 40 || entire < this || entire > 64<<20 {
			return // not AFC: a client talking to the wrong service
		}
		num := binary.LittleEndian.Uint64(h[24:])
		op := binary.LittleEndian.Uint64(h[32:])
		rest := make([]byte, entire-40)
		if _, err := io.ReadFull(c, rest); err != nil {
			return
		}
		hdr := rest[:this-40]
		path := strings.TrimRight(string(hdr), "\x00")
		reply := func(op uint64, rh, payload []byte) {
			buf := make([]byte, 40)
			binary.LittleEndian.PutUint64(buf[0:], 0x4141504c36414643)
			binary.LittleEndian.PutUint64(buf[8:], uint64(40+len(rh)+len(payload)))
			binary.LittleEndian.PutUint64(buf[16:], uint64(40+len(rh)))
			binary.LittleEndian.PutUint64(buf[24:], num)
			binary.LittleEndian.PutUint64(buf[32:], op)
			_, _ = c.Write(append(append(buf, rh...), payload...))
		}
		status := func(code uint64) {
			var b [8]byte
			binary.LittleEndian.PutUint64(b[:], code)
			reply(1, b[:], nil)
		}
		switch op {
		case 0x03: // read dir
			if path != "/" {
				status(8) // object not found
				continue
			}
			var out []byte
			out = append(out, ".\x00..\x00"...)
			for p := range files {
				out = append(out, strings.TrimPrefix(p, "/")+"\x00"...)
			}
			reply(0x02, nil, out)
		case 0x0A: // file info
			data, ok := files[path]
			if !ok {
				status(8)
				continue
			}
			info := fmt.Sprintf("st_size\x00%d\x00st_ifmt\x00S_IFREG\x00st_mtime\x00%d\x00", len(data), time.Now().UnixNano())
			reply(0x02, nil, []byte(info))
		case 0x0D: // open
			name := strings.TrimRight(string(hdr[8:]), "\x00")
			data, ok := files[name]
			if !ok {
				status(8)
				continue
			}
			handles[next] = data
			var b [8]byte
			binary.LittleEndian.PutUint64(b[:], next)
			next++
			reply(0x0E, b[:], nil)
		case 0x0F: // read
			fh := binary.LittleEndian.Uint64(hdr[0:])
			n := binary.LittleEndian.Uint64(hdr[8:])
			data := handles[fh]
			if uint64(len(data)) < n {
				n = uint64(len(data))
			}
			handles[fh] = data[n:]
			reply(0x02, nil, data[:n])
		case 0x14: // close
			delete(handles, binary.LittleEndian.Uint64(hdr[0:]))
			status(0)
		default:
			status(1)
		}
	}
}
