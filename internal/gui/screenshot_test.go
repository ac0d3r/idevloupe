package gui

import (
	"os"
	"testing"

	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

// TestScreenshots renders the download page's screenshots, of the
// fictional device of sampleRaw, in Chinese and English:
// SHOT_DIR=docs go test -run TestScreenshots ./internal/gui
func TestScreenshots(t *testing.T) {
	if os.Getenv("SHOT_DIR") == "" {
		t.Skip("SHOT_DIR not set")
	}
	a := testApp()
	d := &devState{dev: usbmux.Device{DeviceID: 1, UDID: "00008110-001A2B3C4D5E6F70", ConnectionType: "USB"}, phase: phaseDone, raw: sampleRaw(), time: testTime}
	a.devices, a.selected = []*devState{d}, d.dev.UDID
	shotIn(t, a, "screenshot-zh", false, "zh-Hans")
	shotIn(t, a, "screenshot-en", false, "en")
}
