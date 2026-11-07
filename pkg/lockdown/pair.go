package lockdown

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // subject key identifiers are SHA-1 by RFC 5280
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

// Pairing is one attempt to pair, kept across the calls of Pair made while
// the device's alert waits, so that every call offers the same record and
// the keys are generated once. The zero value is ready to use.
type Pairing struct {
	devicePub []byte
	buid      string
	hostID    string
	certs     *pairCerts
}

// Pair asks the device to trust this computer. The device shows its
// "Trust This Computer?" alert; until the user answers, Pair returns
// ErrPairingPending and should be called again with the same p. On success
// the record is saved in usbmuxd, as iTunes and Finder do, and returned.
//
// The client must be connected without a session.
func (l *Client) Pair(p *Pairing) (*usbmux.PairRecord, error) {
	pubV, err := l.GetValue("", "DevicePublicKey")
	if err != nil {
		return nil, fmt.Errorf("read DevicePublicKey: %w", err)
	}
	pub, ok := pubV.([]byte)
	if !ok {
		return nil, errors.New("DevicePublicKey is not data")
	}
	if p.certs == nil || !bytes.Equal(p.devicePub, pub) {
		mux, err := usbmux.Dial()
		if err != nil {
			return nil, err
		}
		buid, err := mux.ReadBUID()
		mux.Close()
		if err != nil {
			return nil, err
		}
		certs, err := makePairCerts(pub)
		if err != nil {
			return nil, err
		}
		*p = Pairing{devicePub: pub, buid: buid, hostID: newUUID(), certs: certs}
	}
	wifi, _ := l.GetValue("", "WiFiAddress")
	wifiMAC, _ := wifi.(string)
	certs, buid, hostID := p.certs, p.buid, p.hostID

	resp, err := l.call(map[string]any{
		"Request":         "Pair",
		"ProtocolVersion": "2",
		"PairRecord": map[string]any{
			"DeviceCertificate": certs.device,
			"HostCertificate":   certs.host,
			"RootCertificate":   certs.root,
			"SystemBUID":        buid,
			"HostID":            hostID,
		},
		"PairingOptions": map[string]any{"ExtendedPairingErrors": true},
	})
	if err != nil {
		var le *Error
		if errors.As(err, &le) {
			switch le.Code {
			case "PairingDialogResponsePending":
				return nil, ErrPairingPending
			case "UserDeniedPairing":
				return nil, ErrUserDenied
			case "PasswordProtected":
				return nil, ErrPasswordLocked
			}
		}
		return nil, err
	}
	escrow, _ := resp["EscrowBag"].([]byte)
	rec := &usbmux.PairRecord{
		HostID:            hostID,
		SystemBUID:        buid,
		HostCertificate:   certs.host,
		HostPrivateKey:    certs.hostKey,
		DeviceCertificate: certs.device,
		RootCertificate:   certs.root,
		RootPrivateKey:    certs.rootKey,
		EscrowBag:         escrow,
		WiFiMACAddress:    wifiMAC,
	}
	mux, err := usbmux.Dial()
	if err != nil {
		return nil, err
	}
	defer mux.Close()
	if err := mux.SavePairRecord(l.dev.UDID, l.dev.DeviceID, rec); err != nil {
		return nil, fmt.Errorf("save pair record: %w", err)
	}
	return rec, nil
}

type pairCerts struct {
	root, rootKey, host, hostKey, device []byte
}

func makePairCerts(devicePubPEM []byte) (*pairCerts, error) {
	block, _ := pem.Decode(devicePubPEM)
	if block == nil {
		return nil, errors.New("DevicePublicKey is not PEM")
	}
	devPub, err := parseRSAPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	hostKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	now := time.Now().Add(-time.Hour)
	until := now.AddDate(10, 0, 0)

	rootTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(0),
		Subject:               pkix.Name{},
		NotBefore:             now,
		NotAfter:              until,
		SignatureAlgorithm:    x509.SHA256WithRSA,
		BasicConstraintsValid: true,
		IsCA:                  true,
		SubjectKeyId:          ski(&rootKey.PublicKey),
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTpl, rootTpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		return nil, fmt.Errorf("root cert: %w", err)
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return nil, err
	}
	leaf := func(pub *rsa.PublicKey) *x509.Certificate {
		return &x509.Certificate{
			SerialNumber:          big.NewInt(0),
			Subject:               pkix.Name{},
			NotBefore:             now,
			NotAfter:              until,
			SignatureAlgorithm:    x509.SHA256WithRSA,
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			BasicConstraintsValid: true,
			IsCA:                  false,
			SubjectKeyId:          ski(pub),
		}
	}
	hostDER, err := x509.CreateCertificate(rand.Reader, leaf(&hostKey.PublicKey), rootCert, &hostKey.PublicKey, rootKey)
	if err != nil {
		return nil, fmt.Errorf("host cert: %w", err)
	}
	devDER, err := x509.CreateCertificate(rand.Reader, leaf(devPub), rootCert, devPub, rootKey)
	if err != nil {
		return nil, fmt.Errorf("device cert: %w", err)
	}
	certPEM := func(der []byte) []byte { return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}) }
	keyPEM := func(k *rsa.PrivateKey) []byte {
		return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	}
	return &pairCerts{
		root: certPEM(rootDER), rootKey: keyPEM(rootKey),
		host: certPEM(hostDER), hostKey: keyPEM(hostKey),
		device: certPEM(devDER),
	}, nil
}

// parseRSAPublicKey accepts PKCS#1 (what devices send, "RSA PUBLIC KEY") and
// PKIX encodings.
func parseRSAPublicKey(der []byte) (*rsa.PublicKey, error) {
	if k, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return k, nil
	}
	// Some devices pad the modulus in ways the strict parser rejects.
	var raw struct{ N, E *big.Int }
	if _, err := asn1.Unmarshal(der, &raw); err == nil && raw.N != nil && raw.E != nil {
		return &rsa.PublicKey{N: raw.N, E: int(raw.E.Int64())}, nil
	}
	k, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse DevicePublicKey: %w", err)
	}
	rk, ok := k.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("DevicePublicKey is not RSA")
	}
	return rk, nil
}

func ski(pub *rsa.PublicKey) []byte {
	der := x509.MarshalPKCS1PublicKey(pub)
	sum := sha1.Sum(der) //nolint:gosec
	return sum[:]
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]))
}
