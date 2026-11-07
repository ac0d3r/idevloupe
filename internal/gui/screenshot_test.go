package gui

import (
	"os"
	"testing"

	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

// TestScreenshots renders the download page's screenshots, of the
// fictional device of sampleRaw, in Chinese and English: the home, before
// an inspection, and the report:
// SHOT_DIR=docs go test -run TestScreenshots ./internal/gui
func TestScreenshots(t *testing.T) {
	if os.Getenv("SHOT_DIR") == "" {
		t.Skip("SHOT_DIR not set")
	}
	for _, l := range []struct{ lang, suffix, name string }{
		{"zh-Hans", "zh", "Ada 的 iPhone"},
		{"en", "en", "Ada's iPhone"},
	} {
		dev := usbmux.Device{DeviceID: 1, UDID: "00008110-001A2B3C4D5E6F70", ConnectionType: "USB"}
		raw := sampleRaw()
		raw.Root["DeviceName"] = l.name

		home := testApp()
		h := &devState{dev: dev, phase: phaseReady, info: raw, infoTime: testTime}
		h.basic.DeviceName = l.name
		home.devices, home.selected = []*devState{h}, dev.UDID
		shotIn(t, home, "screenshot-home-"+l.suffix, false, l.lang)

		a := testApp()
		d := &devState{dev: dev, phase: phaseDone, raw: raw, time: testTime}
		d.basic.DeviceName = l.name
		a.devices, a.selected = []*devState{d}, dev.UDID
		shotIn(t, a, "screenshot-"+l.suffix, false, l.lang)
	}
}
