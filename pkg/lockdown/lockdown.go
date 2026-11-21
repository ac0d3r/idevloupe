// Package lockdown is a lockdownd client. The protocol follows go-ios
// (github.com/danielpaulus/go-ios, MIT) and libimobiledevice.
package lockdown

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ac0d3r/idevloupe/pkg/plistx"
	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

const Port = 62078

var (
	ErrMissingValue     = errors.New("lockdown: value missing")
	ErrInvalidHostID    = errors.New("lockdown: pair record rejected by device (InvalidHostID)")
	ErrPairingPending   = errors.New("pairing: waiting for the user to trust this computer")
	ErrUserDenied       = errors.New("pairing: the user chose not to trust this computer")
	ErrPasswordLocked   = errors.New("pairing: the device is locked")
	ErrServiceForbidden = errors.New("lockdown: service not allowed")
)

type Error struct {
	Request string
	Code    string
}

func (e *Error) Error() string { return fmt.Sprintf("lockdown %s: %s", e.Request, e.Code) }

type Client struct {
	conn      net.Conn
	dev       usbmux.Device
	sessionID string
	rec       *usbmux.PairRecord
	broken    bool
}

func (l *Client) Broken() bool { return l.broken }

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

func (l *Client) Close() error {
	if l.sessionID != "" && !l.broken {
		_, _ = l.call(map[string]any{"Request": "StopSession", "SessionID": l.sessionID})
	}
	return l.conn.Close()
}

func (l *Client) Device() usbmux.Device { return l.dev }

// ErrBroken: a failed exchange may leave a late reply in the stream, so the
// connection must be replaced.
var ErrBroken = errors.New("lockdown: connection unusable after an earlier error")

func (l *Client) call(req map[string]any) (map[string]any, error) {
	return l.callTimeout(req, 20*time.Second)
}

func (l *Client) callTimeout(req map[string]any, timeout time.Duration) (map[string]any, error) {
	if l.broken {
		return nil, ErrBroken
	}
	req["Label"] = usbmux.ProgName
	_ = l.conn.SetDeadline(time.Now().Add(timeout))
	defer l.conn.SetDeadline(time.Time{})
	if err := plistx.WriteFramed(l.conn, req); err != nil {
		l.broken = true
		return nil, err
	}
	resp, err := plistx.ReadFramed(l.conn)
	if err != nil {
		// A timed-out reply may still arrive and answer the next request.
		l.broken = true
		return nil, err
	}
	// A mismatched echo means replies are out of step.
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

func (l *Client) QueryType() (string, error) {
	resp, err := l.call(map[string]any{"Request": "QueryType"})
	if err != nil {
		return "", err
	}
	t, _ := resp["Type"].(string)
	return t, nil
}

func (l *Client) GetValue(domain, key string) (any, error) {
	req := map[string]any{"Request": "GetValue"}
	if domain != "" {
		req["Domain"] = domain
	}
	if key != "" {
		req["Key"] = key
	}
	// disk_usage is computed on the fly and is slow on full devices.
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

type ServiceInfo struct {
	Name string
	Port uint16
	SSL  bool
}

func (l *Client) StartService(name string) (ServiceInfo, error) {
	resp, err := l.call(map[string]any{"Request": "StartService", "Service": name})
	var le0 *Error
	if errors.As(err, &le0) && le0.Code == "EscrowLocked" && l.rec != nil && len(l.rec.EscrowBag) > 0 {
		// The device relocked since pairing: the escrow bag unlocks the service's keys.
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

func (l *Client) OpenService(name string) (net.Conn, error) {
	info, err := l.StartService(name)
	if err != nil {
		return nil, err
	}
	return l.DialService(info)
}

// The device certificate is self-issued by pairing; there is no CA to verify it against.
func ClientTLS(c net.Conn, rec *usbmux.PairRecord) (*tls.Conn, error) {
	cert, err := tls.X509KeyPair(rec.HostCertificate, rec.HostPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("host certificate: %w", err)
	}
	conf := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec
		Certificates:       []tls.Certificate{cert},
		MinVersion:         tls.VersionTLS10,
		// Explicit suites keep older iOS (RSA key exchange only) working.
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
