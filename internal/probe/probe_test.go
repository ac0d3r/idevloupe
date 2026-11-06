package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ac0d3r/idevloupe/internal/fakedevice"
	"github.com/ac0d3r/idevloupe/pkg/device"
	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

func TestFind(t *testing.T) {
	tree := map[string]any{
		"IORegistryEntryName": "Root",
		"IORegistryEntryChildren": []any{
			map[string]any{
				"IORegistryEntryName":        "AppleH10CamIn",
				"FrontCameraModuleSerialNum": []byte("DN8123456\x00"),
				"Temperature":                uint64(30),
			},
			map[string]any{
				"IORegistryEntryName": "syscfg",
				"FCMS":                []byte("DN8123456\x00"),
				"Mod#":                "MQA52",
			},
		},
	}
	var lines []string
	for _, h := range Find(tree) {
		lines = append(lines, h.Path+" = "+h.Value)
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{"/Root/AppleH10CamIn/FrontCameraModuleSerialNum = DN8123456", "/Root/syscfg/FCMS = DN8123456"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Temperature") {
		t.Errorf("uninteresting key reported:\n%s", got)
	}
}

// The probe asks lockdown and MobileGestalt for each key, records what
// each answered, and keeps a source that fails as a whole to one line.
func TestProbeKeys(t *testing.T) {
	for _, gestalt := range []bool{true, false} {
		fd := &fakedevice.Device{
			UDID: "00008110-0001", Paired: true,
			Root:   map[string]any{"ProductType": "iPhone10,3", "DeviceClass": "iPhone"},
			Hidden: map[string]any{"BatterySerialNumber": "F8Y0000LOCKDOWN"},
		}
		if gestalt {
			fd.Gestalt = map[string]any{"RawPanelSerialNumber": "FXV0000GESTALT", "BatteryAuth": true}
		}
		addr, err := fd.Start()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(fd.Close)
		t.Setenv("USBMUXD_SOCKET_ADDRESS", addr)
		devs, err := usbmux.ListDevices()
		if err != nil || len(devs) != 1 {
			t.Fatalf("devices %v %v", devs, err)
		}
		l, err := device.Open(devs[0])
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "probe.plist")
		rep, err := Probe(l, path)
		l.Close()
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, k := range rep.Keys {
			got[k.Source+" "+k.Key] = k.Status
		}
		if got["lockdown BatterySerialNumber"] != "ok" || got["lockdown RawPanelSerialNumber"] != "missing" {
			t.Errorf("gestalt %v: lockdown answers %v", gestalt, got)
		}
		if gestalt {
			if got["MobileGestalt RawPanelSerialNumber"] != "ok" || got["MobileGestalt BatteryAuth"] != "ok" || got["MobileGestalt SysCfg"] != "missing" {
				t.Errorf("gestalt answers %v", got)
			}
		} else if !strings.HasPrefix(got["MobileGestalt *"], "error: ") || len(rep.Keys) != len(Keys)+1 {
			t.Errorf("deprecated gestalt: %d results, %q", len(rep.Keys), got["MobileGestalt *"])
		}
		sum := Summary(rep, 300)
		if gestalt && !strings.Contains(sum, "FXV0000GESTALT") {
			t.Errorf("summary lacks the gestalt value:\n%s", sum)
		}
		if _, err := os.Stat(path); err != nil {
			t.Error(err)
		}
	}
}
