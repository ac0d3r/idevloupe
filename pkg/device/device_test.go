package device_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ac0d3r/idevloupe/internal/fakedevice"
	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/internal/probe"
	"github.com/ac0d3r/idevloupe/pkg/device"
	"github.com/ac0d3r/idevloupe/pkg/lockdown"
	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

func fake(t *testing.T, paired bool) *fakedevice.Device {
	t.Helper()
	d := &fakedevice.Device{
		UDID:   "00008110-0001",
		Paired: paired,
		Root: map[string]any{
			"DeviceName": "Test iPhone", "DeviceClass": "iPhone", "ProductType": "iPhone16,1",
			"ProductVersion": "26.1", "BuildVersion": "23B85", "SerialNumber": "K7Q2M4XW9T",
			"ModelNumber": "MTQA3", "RegionInfo": "CH/A", "ProductionSOC": true,
			"ActivationState": "Activated", "TelephonyCapability": true,
			"InternationalMobileEquipmentIdentity": "356789104523178", "BasebandVersion": "2.20.03",
			"WiFiAddress": "aa:bb:cc:dd:ee:ff",
		},
		Domains: map[string]map[string]any{
			"com.apple.disk_usage": {"TotalDiskCapacity": uint64(512e9)},
			"com.apple.fmip":       {"IsAssociated": false},
		},
		Battery: map[string]any{
			"DesignCapacity": uint64(3274), "NominalChargeCapacity": uint64(3100), "CycleCount": uint64(120),
			"BatteryData": map[string]any{"MaximumCapacityPercent": uint64(96)},
		},
		Platform: map[string]any{"serial-number": []byte("K7Q2M4XW9T\x00"), "model-number": []byte("MTQA3\x00"), "region-info": []byte("CH/A\x00")},
		Apps: []map[string]any{
			{"CFBundleIdentifier": "com.apple.mobilesafari", "ApplicationType": "System"},
			{"CFBundleIdentifier": "com.example.app", "ApplicationType": "User"},
			{"CFBundleIdentifier": "com.opa334.TrollStore", "CFBundleDisplayName": "TrollStore", "ApplicationType": "System"},
		},
		PendingPolls: 2,
		GasGauge:     map[string]any{"CycleCount": uint64(120), "DesignCapacity": uint64(3274), "FullChargeCapacity": uint64(3100)},
		Profiles: map[string]any{"com.acme.mdm": map[string]any{
			"PayloadDisplayName": "Acme 设备管理", "PayloadOrganization": "Acme", "PayloadRemovalDisallowed": true,
		}},
		EscrowLocked: true,
		Hidden:       map[string]any{"ReleaseType": "Internal"},
		CrashFiles: map[string][]byte{
			"/Analytics-2026-10-01-080000.ips.ca.synced": []byte(`{"name":"x","last_value_MaximumCapacityPercent":91,"last_value_CycleCount":388}`),
			"/log-aggregated-2026-11-03-080000.ips":      []byte(`{"bug_type":"211"}` + "\n" + `{"last_value_MaximumCapacityPercent":89,"last_value_CycleCount":402,"last_value_NominalChargeCapacity":2911,"last_value_SomethingElse":7}`),
			"/JetsamEvent-2026-11-04-090000.ips":         []byte(`{"last_value_CycleCount":999}`),
		},
	}
	addr, err := d.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	t.Setenv("USBMUXD_SOCKET_ADDRESS", addr)
	return d
}

func TestPairAndInspect(t *testing.T) {
	fd := fake(t, false)
	devs, err := usbmux.ListDevices()
	if err != nil || len(devs) != 1 {
		t.Fatalf("devices %v %v", devs, err)
	}
	dev := devs[0]
	b, err := device.ReadBasic(dev)
	if err != nil || b.ProductType != "iPhone16,1" {
		t.Fatalf("basic %+v %v", b, err)
	}
	if _, err := device.Open(dev); !errors.Is(err, device.ErrNotTrusted) {
		t.Fatalf("open before pairing: %v", err)
	}
	// The trust dialog waits twice, then the user taps Trust.
	var pairing lockdown.Pairing
	for i := 0; ; i++ {
		err := device.Pair(dev, &pairing)
		if err == nil {
			break
		}
		if !errors.Is(err, lockdown.ErrPairingPending) || i > 3 {
			t.Fatalf("pair: %v", err)
		}
	}
	// Every poll offers the same record; keys are made once.
	if ids := fd.PairHostIDs; len(ids) != 3 || ids[0] != ids[1] || ids[1] != ids[2] {
		t.Errorf("pair requests carried host IDs %v, want one ID three times", ids)
	}
	l, err := device.Open(dev)
	if err != nil {
		t.Fatalf("open after pairing: %v", err)
	}
	// The overview reads lockdown and the battery gauge, and nothing that
	// only an inspection needs.
	info, err := inspect.CollectInfo(inspect.Reuse(l, func() (*lockdown.Client, error) { return device.Open(dev) }))
	if err != nil {
		t.Fatal(err)
	}
	if info.Root["SerialNumber"] != "K7Q2M4XW9T" || info.Root["ReleaseType"] != "Internal" || info.BatteryReg == nil {
		t.Errorf("info: root %v battery %v", info.Root, info.BatteryReg)
	}
	if info.Apps != nil || info.Parts != nil || info.MC != nil || info.Analytics != nil || len(info.Errors) != 0 {
		t.Errorf("info read more than the overview: %+v", info)
	}
	o := inspect.Describe(info, time.Now(), "en")
	fields := map[string]string{}
	for _, f := range o.Fields {
		fields[f.Label] = f.Value
	}
	if fields["Battery health"] != "96%" || fields["Find My iPhone"] != "Off" || fields["Activation"] != "Activated" || o.StorageTotal != 512e9 {
		t.Errorf("overview fields %v storage %d", fields, o.StorageTotal)
	}

	l, err = device.Open(dev)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := inspect.Collect(inspect.Reuse(l, func() (*lockdown.Client, error) { return device.Open(dev) }), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := inspect.Analyze(raw, time.Now(), "zh-Hans")
	got := map[string]inspect.Check{}
	for _, c := range r.Checks {
		got[c.Title] = c
	}
	// The live gauge's MaximumCapacityPercent wins over the log.
	if c := got["电池健康"]; c.Value != "96%" || c.Status != inspect.Pass {
		t.Errorf("battery %+v", c)
	}
	// The newest analytics log is used; the jetsam report is not a log.
	if b := r.Battery; b.LogHealth != 89 || b.LogCycles != 402 || !strings.Contains(b.LogFile, "log-aggregated-2026-11-03") {
		t.Errorf("analytics: health %d cycles %d file %q", b.LogHealth, b.LogCycles, b.LogFile)
	}
	// 402 cycles logged, 120 now: the battery was changed after the log.
	if c := got["电池来源"]; c.Status != inspect.Warn || !strings.Contains(c.Detail, "402") {
		t.Errorf("battery source %+v", c)
	}
	if c := got["越狱检测"]; c.Status != inspect.Fail || !strings.Contains(c.Detail, "TrollStore") {
		t.Errorf("jailbreak %+v", c)
	}
	if c := got["序列号一致性"]; c.Status != inspect.Pass {
		t.Errorf("serial consistency %+v", c)
	}
	if c := got["型号一致性"]; c.Status != inspect.Pass {
		t.Errorf("model consistency %+v", c)
	}
	if c := got["远程管理 (MDM)"]; c.Status != inspect.Warn || !strings.Contains(c.Value, "Acme") {
		t.Errorf("mdm %+v", c)
	}
	// ReleaseType is left out of the root dump and read on its own.
	if c := got["系统类型"]; c.Status != inspect.Fail {
		t.Errorf("release type %+v", c)
	}
	if _, ok := got["电池数据一致性"]; ok {
		t.Errorf("battery sources agree but were reported inconsistent")
	}
	if dev.UDID != "00008110-0001" {
		t.Errorf("udid %q, want the UDID property over SerialNumber", dev.UDID)
	}
	if r.Summary.Marketing != "iPhone 15 Pro" || r.Summary.Capacity != "512 GB" || r.AppCount != 3 {
		t.Errorf("summary %+v apps %d", r.Summary, r.AppCount)
	}
	if len(r.Errors) != 0 {
		t.Errorf("collection errors: %v", r.Errors)
	}
}

func TestListen(t *testing.T) {
	fake(t, true)
	ch, err := usbmux.Listen(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if !ev.Attached || ev.Device.UDID != "00008110-0001" {
			t.Errorf("event %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no attach event")
	}
}

// A reply that arrives late must not be taken as the answer to the next
// request: on an iPhone 16 a slow disk_usage reply shifted every reply after
// it, so starting afc2 got another service's reply and the report claimed a
// jailbreak. The client must notice, drop the connection and start over.
func TestLateReplyDoesNotShiftAnswers(t *testing.T) {
	d := fake(t, true)
	d.LateReplyDomain = "com.apple.disk_usage"
	d.EscrowLocked = false
	d.Apps = d.Apps[:2] // no jailbreak app
	d.PendingPolls = 0
	devs, _ := usbmux.ListDevices()
	l, err := device.Open(devs[0])
	if err != nil {
		t.Fatal(err)
	}
	raw, err := inspect.Collect(inspect.Reuse(l, func() (*lockdown.Client, error) { return device.Open(devs[0]) }), nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw.AFC2 == nil || *raw.AFC2 {
		t.Errorf("afc2 = %v, err %v: a shifted reply was taken as afc2's", raw.AFC2, raw.AFC2Err)
	}
	if len(raw.Apps) != 2 || raw.MC == nil {
		t.Errorf("apps %d, mc %v, errors %v", len(raw.Apps), raw.MC, raw.Errors)
	}
	if _, ok := raw.Domains["com.apple.disk_usage"]; !ok {
		t.Error("disk_usage lost")
	}
	if _, ok := raw.Domains["com.apple.fmip"]; !ok {
		t.Errorf("the domain after the late reply was not re-read: %v", raw.Errors)
	}
}

func TestProbeIORegistry(t *testing.T) {
	d := fake(t, true)
	d.EscrowLocked = false
	d.Planes = map[string]map[string]any{
		"IODeviceTree": {"IORegistryEntryName": "device-tree", "IORegistryEntryChildren": []any{
			map[string]any{"IORegistryEntryName": "chosen", "mlb-serial-number": []byte("F3X80241VTJJ3QLA\x00")},
		}},
	}
	devs, _ := usbmux.ListDevices()
	l, err := device.Open(devs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	path := filepath.Join(t.TempDir(), "ioreg.plist")
	rep, err := probe.Probe(l, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Queries) != len(probe.Queries) {
		t.Fatalf("%d results for %d queries", len(rep.Queries), len(probe.Queries))
	}
	sum := probe.Summary(rep, 100)
	for _, want := range []string{
		"plane=IODeviceTree", "Success", // a query that answered
		"Failure", // ones the device does not know
		"[plane=IODeviceTree] /device-tree/chosen/mlb-serial-number = F3X80241VTJJ3QLA",
		"class=AppleSmartBattery ", // absent from the fake, listed anyway
	} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary lacks %q:\n%s", want, sum)
		}
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		t.Errorf("dump file: %v", err)
	}
}

// iPhone 16: right after an IORegistry probe the device refused the
// diagnostics connection, and the report lost the battery gauge.
func TestDiagnosticsRefusedThenAccepted(t *testing.T) {
	d := fake(t, true)
	d.EscrowLocked = false
	d.RefuseDiagnostics = 2
	devs, _ := usbmux.ListDevices()
	l, err := device.Open(devs[0])
	if err != nil {
		t.Fatal(err)
	}
	raw, err := inspect.Collect(inspect.Reuse(l, func() (*lockdown.Client, error) { return device.Open(devs[0]) }), nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw.BatteryReg == nil {
		t.Errorf("battery gauge lost to a refused connection: %v", raw.Errors)
	}
}
