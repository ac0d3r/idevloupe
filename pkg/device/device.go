// Package device opens trusted lockdown sessions with devices, pairing when
// needed.
package device

import (
	"errors"
	"fmt"

	"github.com/ac0d3r/idevloupe/pkg/lockdown"
	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

// ErrNotTrusted means the device has no valid pair record with this host;
// call Pair.
var ErrNotTrusted = errors.New("the device does not trust this computer")

// Basic is what lockdownd tells anyone, before trust.
type Basic struct {
	DeviceName     string
	ProductType    string
	ProductVersion string
	DeviceClass    string
}

// ReadBasic reads the values available without a session.
func ReadBasic(dev usbmux.Device) (Basic, error) {
	l, err := lockdown.Connect(dev)
	if err != nil {
		return Basic{}, err
	}
	defer l.Close()
	var b Basic
	get := func(k string) string {
		v, _ := l.GetValue("", k)
		s, _ := v.(string)
		return s
	}
	b.DeviceName = get("DeviceName")
	b.ProductType = get("ProductType")
	b.ProductVersion = get("ProductVersion")
	b.DeviceClass = get("DeviceClass")
	return b, nil
}

// Open returns a lockdown client with a trusted session. It returns
// ErrNotTrusted when the device must be paired first.
func Open(dev usbmux.Device) (*lockdown.Client, error) {
	mux, err := usbmux.Dial()
	if err != nil {
		return nil, err
	}
	rec, err := mux.ReadPairRecord(dev.UDID)
	mux.Close()
	if errors.Is(err, usbmux.ErrNotPaired) {
		return nil, ErrNotTrusted
	}
	if err != nil {
		return nil, err
	}
	l, err := lockdown.Connect(dev)
	if err != nil {
		return nil, err
	}
	if err := l.StartSession(rec); err != nil {
		l.Close()
		if errors.Is(err, lockdown.ErrInvalidHostID) {
			return nil, ErrNotTrusted
		}
		return nil, fmt.Errorf("start session: %w", err)
	}
	return l, nil
}

// Pair asks the device to trust this computer. It returns
// lockdown.ErrPairingPending while the alert waits for the user; call it
// again with the same p until it returns nil.
func Pair(dev usbmux.Device, p *lockdown.Pairing) error {
	l, err := lockdown.Connect(dev)
	if err != nil {
		return err
	}
	defer l.Close()
	_, err = l.Pair(p)
	return err
}
