// Package lockdown is a client of lockdownd, the device daemon on port 62078
// that answers device values, opens trusted sessions and starts services.
//
// Protocol knowledge comes from go-ios (github.com/danielpaulus/go-ios, MIT)
// and libimobiledevice.
package lockdown

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"idevloupe/internal/plistx"
	"idevloupe/internal/usbmux"
)

// Port is lockdownd's port on the device.
const Port = 62078

const label = "idevloupe"

// Errors lockdownd reports that callers act on.
var (
	ErrMissingValue     = errors.New("lockdown: value missing")
	ErrInvalidHostID    = errors.New("lockdown: pair record rejected by device (InvalidHostID)")
	ErrPairingPending   = errors.New("pairing: waiting for the user to trust this computer")
	ErrUserDenied       = errors.New("pairing: the user chose not to trust this computer")
	ErrPasswordLocked   = errors.New("pairing: the device is locked")
	ErrServiceForbidden = errors.New("lockdown: service not allowed")
)

// Error is an error string lockdownd returned.
type Error struct {
	Request string
	Code    string
}

func (e *Error) Error() string { return fmt.Sprintf("lockdown %s: %s", e.Request, e.Code) }

// Client is a connection to lockdownd.
type Client struct {
	conn      net.Conn
	dev       usbmux.Device
	sessionID string
	rec       *usbmux.PairRecord
	broken    bool
}

// Broken reports whether the connection must be replaced.
func (l *Client) Broken() bool { return l.broken }

// Connect opens a connection to lockdownd on the device, without a session.
func Connect(dev usbmux.Device) (*Client, error) {
	c, err := usbmux.ConnectPort(dev.DeviceID, Port)
	if err != nil {
		return nil, err
	}
	l := &Client{conn: c, dev: dev}
	if _, err := l.QueryType(); err != nil {
		c.Close()
		return nil, err
	}
	return l, nil
}

// Close ends the session, if any, and closes the connection.
func (l *Client) Close() error {
	if l.sessionID != "" && !l.broken {
		_, _ = l.call(map[string]any{"Request": "StopSession", "SessionID": l.sessionID})
	}
	return l.conn.Close()
}

// Device returns the device the client talks to.
func (l *Client) Device() usbmux.Device { return l.dev }

// ErrBroken means an earlier exchange failed mid-way (a timeout, a closed
// connection, a reply to another request): the stream may hold a late reply,
// so the connection must not be used again. Open a new one.
var ErrBroken = errors.New("lockdown: connection unusable after an earlier error")

func (l *Client) call(req map[string]any) (map[string]any, error) {
	return l.callTimeout(req, 20*time.Second)
}

func (l *Client) callTimeout(req map[string]any, timeout time.Duration) (map[string]any, error) {
	if l.broken {
		return nil, ErrBroken
	}
	req["Label"] = label
	_ = l.conn.SetDeadline(time.Now().Add(timeout))
	defer l.conn.SetDeadline(time.Time{})
	if err := plistx.WriteFramed(l.conn, req); err != nil {
		l.broken = true
		return nil, err
	}
	resp, err := plistx.ReadFramed(l.conn)
	if err != nil {
		// A timed out reply may still arrive and would answer the next
		// request.
		l.broken = true
		return nil, err
	}
	// lockdownd echoes the request; a mismatch means replies are out of
	// step, and acting on them would report one service as another.
	if want, _ := req["Request"].(string); want != "" {
		if got, ok := resp["Request"].(string); ok && got != want {
			l.broken = true
			return nil, fmt.Errorf("%w: sent %s, got reply to %s", ErrBroken, want, got)
		}
	}
	if e, ok := resp["Error"].(string); ok && e != "" {
		r, _ := req["Request"].(string)
		return resp, &Error{Request: r, Code: e}
	}
	return resp, nil
}

// QueryType returns lockdownd's type, "com.apple.mobile.lockdown".
func (l *Client) QueryType() (string, error) {
	resp, err := l.call(map[string]any{"Request": "QueryType"})
	if err != nil {
		return "", err
	}
	t, _ := resp["Type"].(string)
	return t, nil
}

// GetValue reads a value. An empty domain is the root domain; an empty key
// returns the whole domain as a dictionary.
func (l *Client) GetValue(domain, key string) (any, error) {
	req := map[string]any{"Request": "GetValue"}
	if domain != "" {
		req["Domain"] = domain
	}
	if key != "" {
		req["Key"] = key
	}
	// disk_usage computes the usage on the fly, which takes long on full
	// devices.
	timeout := 20 * time.Second
	if strings.HasPrefix(domain, "com.apple.disk_usage") {
		timeout = 90 * time.Second
	}
	resp, err := l.callTimeout(req, timeout)
	if err != nil {
		var le *Error
		if errors.As(err, &le) && le.Code == "MissingValue" {
			return nil, ErrMissingValue
		}
		return nil, err
	}
	for k, want := range map[string]string{"Key": key, "Domain": domain} {
		if got, ok := resp[k].(string); ok && want != "" && got != want {
			l.broken = true
			return nil, fmt.Errorf("%w: asked %s %q, got %q", ErrBroken, k, want, got)
		}
	}
	v, ok := resp["Value"]
	if !ok {
		return nil, ErrMissingValue
	}
	return v, nil
}

// GetDomain reads a whole domain as a dictionary.
func (l *Client) GetDomain(domain string) (map[string]any, error) {
	v, err := l.GetValue(domain, "")
	if err != nil {
		return nil, err
	}
	m, ok := plistx.Dict(v)
	if !ok {
		return nil, fmt.Errorf("lockdown: domain %q is not a dictionary", domain)
	}
	return m, nil
}

// StartSession opens a trusted session with the pair record and switches the
// connection to TLS when lockdownd asks for it.
func (l *Client) StartSession(rec *usbmux.PairRecord) error {
	resp, err := l.call(map[string]any{
		"Request":         "StartSession",
		"ProtocolVersion": "2",
		"HostID":          rec.HostID,
		"SystemBUID":      rec.SystemBUID,
	})
	if err != nil {
		var le *Error
		if errors.As(err, &le) && (le.Code == "InvalidHostID" || le.Code == "InvalidPairRecord") {
			return ErrInvalidHostID
		}
		return err
	}
	l.sessionID, _ = resp["SessionID"].(string)
	l.rec = rec
	if ssl, _ := resp["EnableSessionSSL"].(bool); ssl {
		tc, err := ClientTLS(l.conn, rec)
		if err != nil {
			return fmt.Errorf("lockdown session TLS: %w", err)
		}
		l.conn = tc
	}
	return nil
}

// ServiceInfo is how to reach a service lockdownd started.
type ServiceInfo struct {
	Name string
	Port uint16
	SSL  bool
}

// StartService asks lockdownd to start a service. It needs a session.
func (l *Client) StartService(name string) (ServiceInfo, error) {
	resp, err := l.call(map[string]any{"Request": "StartService", "Service": name})
	var le0 *Error
	if errors.As(err, &le0) && le0.Code == "EscrowLocked" && l.rec != nil && len(l.rec.EscrowBag) > 0 {
		// The device locked again since pairing: the escrow bag unlocks
		// the keys the service needs.
		resp, err = l.call(map[string]any{"Request": "StartService", "Service": name, "EscrowBag": l.rec.EscrowBag})
	}
	if err != nil {
		var le *Error
		if errors.As(err, &le) && (le.Code == "InvalidService" || le.Code == "ServiceProhibited") {
			return ServiceInfo{}, fmt.Errorf("%w: %s (%s)", ErrServiceForbidden, name, le.Code)
		}
		return ServiceInfo{}, err
	}
	if got, ok := resp["Service"].(string); ok && got != name {
		l.broken = true
		return ServiceInfo{}, fmt.Errorf("%w: started %s, got reply for %s", ErrBroken, name, got)
	}
	port, _ := plistx.Int(resp["Port"])
	if port <= 0 || port > 65535 {
		return ServiceInfo{}, fmt.Errorf("lockdown: service %s returned no port", name)
	}
	ssl, _ := resp["EnableServiceSSL"].(bool)
	return ServiceInfo{Name: name, Port: uint16(port), SSL: ssl}, nil
}

// DialService connects to a started service, over TLS when it needs it.
func (l *Client) DialService(info ServiceInfo) (net.Conn, error) {
	c, err := usbmux.ConnectPort(l.dev.DeviceID, info.Port)
	if err != nil {
		return nil, err
	}
	if info.SSL {
		if l.rec == nil {
			c.Close()
			return nil, errors.New("lockdown: service needs TLS but there is no pair record")
		}
		tc, err := ClientTLS(c, l.rec)
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("service %s TLS: %w", info.Name, err)
		}
		return tc, nil
	}
	return c, nil
}

// OpenService starts a service and connects to it.
func (l *Client) OpenService(name string) (net.Conn, error) {
	info, err := l.StartService(name)
	if err != nil {
		return nil, err
	}
	return l.DialService(info)
}

// ClientTLS wraps c in a TLS client authenticated with the host certificate
// of the pair record. The device's certificate is self-issued by the pairing,
// so it is not verified against a CA.
func ClientTLS(c net.Conn, rec *usbmux.PairRecord) (*tls.Conn, error) {
	cert, err := tls.X509KeyPair(rec.HostCertificate, rec.HostPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("host certificate: %w", err)
	}
	conf := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // device certs are pair-issued, see above
		Certificates:       []tls.Certificate{cert},
		MinVersion:         tls.VersionTLS10,
		// Explicit suites keep older iOS versions, which offer only RSA key
		// exchange, working; TLS 1.3 suites are always enabled.
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
	}
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	tc := tls.Client(c, conf)
	if err := tc.Handshake(); err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Time{})
	return tc, nil
}
