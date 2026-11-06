package lockdown

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"

	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

// The device verifies that the host certificate chains to the root it was
// given in the pair request; check that, and a TLS session both ways.
func TestPairCerts(t *testing.T) {
	devKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	pub := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&devKey.PublicKey)})
	certs, err := makePairCerts(pub)
	if err != nil {
		t.Fatal(err)
	}
	parse := func(p []byte) *x509.Certificate {
		b, _ := pem.Decode(p)
		c, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	root, host, dev := parse(certs.root), parse(certs.host), parse(certs.device)
	pool := x509.NewCertPool()
	pool.AddCert(root)
	for name, c := range map[string]*x509.Certificate{"host": host, "device": dev} {
		if _, err := c.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
			t.Errorf("%s cert does not chain to root: %v", name, err)
		}
	}
	if !dev.PublicKey.(*rsa.PublicKey).Equal(&devKey.PublicKey) {
		t.Error("device cert does not carry the device key")
	}

	// Device side: its key with the device cert, requiring the host cert.
	devTLS := tls.Certificate{Certificate: [][]byte{dev.Raw}, PrivateKey: devKey}
	a, b := net.Pipe()
	done := make(chan error, 1)
	go func() {
		s := tls.Server(a, &tls.Config{Certificates: []tls.Certificate{devTLS}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool})
		done <- s.Handshake()
	}()
	rec := &usbmux.PairRecord{HostCertificate: certs.host, HostPrivateKey: certs.hostKey}
	if _, err := ClientTLS(b, rec); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("server handshake: %v", err)
	}
}
