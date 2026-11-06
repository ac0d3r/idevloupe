package gui

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/pkg/lockdown"
	"github.com/ac0d3r/idevloupe/pkg/services"
	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

func sampleRaw() *inspect.RawData {
	f := false
	raw := &inspect.RawData{
		UDID: "00008110-001A2B3C4D5E6F70",
		Root: map[string]any{
			"DeviceName": "Ada 的 iPhone", "DeviceClass": "iPhone", "ProductType": "iPhone14,2",
			"HardwareModel": "D63AP", "ProductVersion": "18.6.2", "BuildVersion": "22G100",
			"SerialNumber": "H4K2L9QXWT", "ModelNumber": "MLTE3", "RegionInfo": "CH/A",
			"ProductionSOC": true, "ActivationState": "Activated",
			"InternationalMobileEquipmentIdentity": "356789104523178", "TelephonyCapability": true,
			"BasebandVersion": "4.06.02", "SIMStatus": "kCTSIMSupportSIMStatusReady",
			"UniqueChipID": uint64(0x1A2B3C4D5E6F70), "ChipID": uint64(0x8110), "HardwarePlatform": "t8110",
			"DeviceEnclosureColor": "#f1f2ed", "PasswordProtected": true, "CPUArchitecture": "arm64e",
			"WiFiAddress": "a4:cf:99:12:34:56", "BluetoothAddress": "a4:cf:99:12:34:57", "FirmwareVersion": "iBoot-11881.140.96",
		},
		Domains: map[string]map[string]any{
			"com.apple.disk_usage":       {"TotalDiskCapacity": uint64(256_000_000_000), "AmountDataAvailable": uint64(100e9)},
			"com.apple.fmip":             {"IsAssociated": true},
			"com.apple.mobile.chaperone": {"DeviceIsChaperoned": false},
			"com.apple.international":    {"Language": "zh-Hans", "Locale": "zh_CN"},
		},
		BatteryReg: map[string]any{
			"DesignCapacity": uint64(3095), "NominalChargeCapacity": uint64(2610), "CycleCount": uint64(612),
			"CurrentCapacity": uint64(76), "Temperature": uint64(3010), "Voltage": uint64(3990),
			"InstantAmperage": uint64(0xFFFFFFFFFFFFFF38), "Serial": "F8Y1234ABCD",
		},
		Apps:   []services.App{{BundleID: "com.apple.mobilesafari", Type: "System"}},
		AFC2:   &f,
		MC:     &services.MCInfo{},
		Errors: []*inspect.StepError{{Step: inspect.StepReadDomain, Target: "com.apple.mobile.software_behavior", Err: errors.New("lockdown GetValue: InvalidDomain")}},
	}
	return raw
}

// iphoneXRaw is a real iPhone X whose screen was
// replaced, Face ID broken, battery original but worn.
func iphoneXRaw() *inspect.RawData {
	f := false
	raw := &inspect.RawData{
		Root: map[string]any{
			"DeviceName": "whoami的iPhone", "DeviceClass": "iPhone", "ProductType": "iPhone10,3",
			"ProductVersion": "16.7.16", "BuildVersion": "20H392", "SerialNumber": "F17W374TJCLF",
			"ModelNumber": "MQA52", "RegionInfo": "CH/A", "ProductionSOC": true, "ActivationState": "Activated",
			"InternationalMobileEquipmentIdentity": "353051099553303", "TelephonyCapability": true,
			"BasebandVersion": "9.00.02", "BasebandStatus": "BBInfoAvailable", "DeviceColor": "1",
		},
		Domains: map[string]map[string]any{
			"com.apple.disk_usage": {"TotalDiskCapacity": uint64(64e9)},
		},
		BatteryReg: map[string]any{
			"DesignCapacity": uint64(2701), "NominalChargeCapacity": uint64(2058), "CycleCount": uint64(1575),
			"CurrentCapacity": uint64(89), "Temperature": uint64(2079), "Serial": "F8Y75174H07HXRY9F",
			"BatteryData": map[string]any{"BatteryHealthMetric": uint64(282)},
		},
		Parts: map[string]map[string]any{
			"pearl": {"PearlSelfTestResult": uint64(8)},
			"camera": {
				"BackCameraModuleSerialNumString":                "DN880159WHNH8MH1H",
				"FrontCameraModuleSerialNumString":               "G8R7491GQYPHNXMAJ",
				"FrontIRCameraModuleSerialNumString":             "DNM75132BKVH0N545",
				"FrontIRStructuredLightProjectorSerialNumString": "DN875235AV1JJGXAS",
			},
			"product": {
				"coverglass-serial-number": "FXV73552871J6DX90+01009136794159075694800743",
				"raw-panel-serial-number":  "FXV73552871J6DX90+01009136794159075694800743+G9N80242JSFH8PRA9+A1000000000000000390",
			},
		},
		Apps: []services.App{{BundleID: "com.apple.mobilesafari", Type: "System"}},
		AFC2: &f,
		MC:   &services.MCInfo{},
	}
	return raw
}

// The checks of Face ID, the parts and the battery reach the window.
func TestPartsAndBatteryInUI(t *testing.T) {
	a := testApp()
	d := &devState{dev: usbmux.Device{DeviceID: 3, UDID: "x", ConnectionType: "USB"}, phase: phaseDone, raw: iphoneXRaw(), time: testTime}
	a.devices, a.selected = []*devState{d}, "x"
	tabs := []struct {
		tab  int
		name string
		want []string
	}{
		{0, "x-report", []string{"面容 ID 自检 未通过（代码 8）", "电池健康 76%", "充电循环 1575 次", "需人工核对", "原彩显示"}},
		{1, "x-battery", []string{"内阻指标", "282", "1575 次"}},
		{2, "x-info", []string{"部件序列号（当前值）", "面容 ID 点阵投影器", "DN875235AV1JJGXAS", "屏幕盖板", "FXV73552871J6DX90"}},
	}
	for _, tc := range tabs {
		a.tab = tc.tab
		tt := shot(t, a, tc.name, false)
		texts := strings.Join(tt.Texts(), "\n")
		for _, w := range tc.want {
			if !strings.Contains(texts, w) {
				t.Errorf("tab %d lacks %q", tc.tab, w)
			}
		}
		if tc.tab == 0 {
			// Scroll to the parts and the manual checks, for the screenshot.
			tt.Wheel(700, 500, 0, -3000)
			tt.Frame()
			if dir := os.Getenv("SHOT_DIR"); dir != "" {
				f, err := os.Create(filepath.Join(dir, "x-report-bottom.png"))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, tt.Image()); err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
		}
	}
}

// iPhone 8: Touch ID, self test OK.
func TestTouchIDInUI(t *testing.T) {
	f := false
	raw := &inspect.RawData{
		Root: map[string]any{"DeviceName": "iPhone 8", "DeviceClass": "iPhone", "ProductType": "iPhone10,1",
			"ProductVersion": "16.5.1", "BuildVersion": "20F75", "SerialNumber": "FFNZMYBEJC6C"},
		Domains: map[string]map[string]any{},
		BatteryReg: map[string]any{"DesignCapacity": uint64(1810), "NominalChargeCapacity": uint64(1539), "CycleCount": uint64(322),
			"BatteryData": map[string]any{"BatteryHealthMetric": uint64(454)}},
		Parts: map[string]map[string]any{
			"mesa":       {"MesaSelfTest": "OK"},
			"mesadriver": {"MesaCalBlobSource": "FDR"},
			"touchid":    {"SerialNumber": []byte{0x02, 0x17, 0x10, 0x6c, 0x60, 0x80, 0x85, 0x29, 0x3b, 0x5a, 0x13, 0x69, 0x06, 0x02, 0x00, 0xd5}},
		},
		AFC2: &f, MC: &services.MCInfo{},
	}
	a := testApp()
	d := &devState{dev: usbmux.Device{DeviceID: 5, UDID: "8", ConnectionType: "USB"}, phase: phaseDone,
		raw: raw, time: testTime}
	d.basic.DeviceName = "iPhone 8"
	a.devices, a.selected = []*devState{d}, "8"
	if got := d.subtitle("zh-Hans"); got != "iPhone 8" {
		t.Errorf("subtitle %q: no separator without a capacity", got)
	}
	for tab, want := range map[int][]string{
		0: {"指纹（Touch ID）", "自检通过", "MesaSelfTest = OK", "序列号 FFNZMYBEJC6C"},
		2: {"指纹传感器", "0217106c608085293b5a1369060200d5"},
	} {
		a.tab = tab
		tt := shot(t, a, fmt.Sprintf("8-tab%d", tab), false)
		texts := strings.Join(tt.Texts(), "\n")
		for _, w := range want {
			if !strings.Contains(texts, w) {
				t.Errorf("tab %d lacks %q", tab, w)
			}
		}
		if strings.Contains(texts, "面容 ID") {
			t.Errorf("tab %d shows Face ID on a Touch ID model", tab)
		}
	}
}

// testTime is when the tests' inspections ran.
var testTime = time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)

// shot draws the window in Chinese, which the tests' texts are in, and
// saves it to SHOT_DIR when set.
func shot(t *testing.T, a *App, name string, dark bool) *ui.Tester {
	return shotIn(t, a, name, dark, "zh-Hans")
}

func shotIn(t *testing.T, a *App, name string, dark bool, lang string) *ui.Tester {
	tt := ui.NewTester(a.View, 1180, 780)
	tt.SetLocale(lang)
	tt.SetDark(dark)
	tt.SetScale(2)
	tt.Frame()
	if dir := os.Getenv("SHOT_DIR"); dir != "" {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	return tt
}

func testApp() *App {
	a := New()
	a.update = func(fn func()) { fn() }
	return a
}

func TestViews(t *testing.T) {
	a := testApp()
	tt := shot(t, a, "empty", false)
	if !tt.HasText("连接你的 iPhone") {
		t.Errorf("empty state texts %q", tt.Texts())
	}

	dev := usbmux.Device{DeviceID: 1, UDID: "00008110-001A2B3C4D5E6F70", ConnectionType: "USB"}
	d := &devState{dev: dev, phase: phaseDone, raw: sampleRaw(), time: testTime}
	d.basic.DeviceName = "Ada 的 iPhone"
	pair := &devState{dev: usbmux.Device{DeviceID: 2, UDID: "x2", ConnectionType: "USB"}, phase: phasePairing, step: errorStep(lockdown.ErrPairingPending)}
	pair.basic.ProductType = "iPhone17,1"
	a.devices = []*devState{d, pair}
	a.selected = d.dev.UDID
	for i, name := range []string{"report", "battery", "info", "raw"} {
		a.tab = i
		if name == "raw" {
			a.rawOpen["lockdown"] = true
		}
		tt = shot(t, a, name, false)
	}
	a.tab = 0
	shot(t, a, "report-dark", true)
	if !tt.HasText("原始数据") {
		t.Error("tabs missing")
	}
	a.selected = "x2"
	shot(t, a, "pairing", false)
}

// On connecting, the window shows what the device says about itself and
// judges nothing until asked to inspect it.
func TestOverview(t *testing.T) {
	a := testApp()
	d := &devState{dev: usbmux.Device{DeviceID: 1, UDID: "u", ConnectionType: "USB"}, phase: phaseReady, info: sampleRaw(), infoTime: testTime}
	a.devices, a.selected = []*devState{d}, "u"
	a.detailsOpen = true
	tt := shot(t, a, "overview", false)
	texts := strings.Join(tt.Texts(), "\n")
	for _, w := range []string{"iPhone 13 Pro", "验机", "已激活", "查找我的 iPhone", "已开启（激活锁）", "H4K2L9QXWT", "电池健康", "84%", "612 次", "已用 156.0 GB，共 256.0 GB", "设备详情", "硬件"} {
		if !strings.Contains(texts, w) {
			t.Errorf("overview lacks %q in\n%s", w, texts)
		}
	}
	for _, w := range []string{"通过", "警告", "需要关注", "验机报告"} {
		if strings.Contains(texts, w) {
			t.Errorf("overview judges: %q", w)
		}
	}
	shot(t, a, "overview-dark", true)
	for _, lang := range []string{"en", "de"} {
		shotIn(t, a, "overview-"+lang, false, lang)
	}
	// Inspecting starts from the button. The device is not real: what the
	// worker finds is dropped.
	t.Setenv("USBMUXD_SOCKET_ADDRESS", "tcp://127.0.0.1:1")
	a.update = func(func()) {}
	tt = shot(t, a, "overview", false)
	if err := tt.Click("验机"); err != nil {
		t.Fatal(err)
	}
	if d.phase != phaseInspecting || d.info == nil {
		t.Errorf("phase %v, info %v after clicking Inspect", d.phase, d.info != nil)
	}
}

// The window says everything in its language, the report included, made
// again from the raw data when the language changes.
func TestLanguagesInUI(t *testing.T) {
	a := testApp()
	d := &devState{dev: usbmux.Device{DeviceID: 3, UDID: "x", ConnectionType: "USB"}, phase: phaseDone, raw: iphoneXRaw(), time: testTime}
	a.devices, a.selected = []*devState{d}, "x"
	for lang, want := range map[string][]string{
		"en": {"Needs attention", "Face ID self test", "Failed (code 8)", "Battery health", "Export report", "Connected devices", "English"},
		"de": {"Zu beachten", "Face ID-Selbsttest", "Fehlgeschlagen (Code 8)", "Batteriezustand", "Bericht exportieren", "Verbundene Geräte", "Deutsch"},
	} {
		for tab := range 4 {
			a.tab = tab
			tt := shotIn(t, a, fmt.Sprintf("%s-tab%d", lang, tab), false, lang)
			texts := strings.Join(tt.Texts(), "\n")
			if tab == 0 {
				for _, w := range want {
					if !strings.Contains(texts, w) {
						t.Errorf("%s: no %q in\n%s", lang, w, texts)
					}
				}
			}
			for _, r := range strings.ReplaceAll(texts, "whoami的iPhone", "") {
				if r >= 0x4e00 && r <= 0x9fff {
					t.Errorf("%s tab %d shows Chinese: %q", lang, tab, string(r))
					break
				}
			}
		}
		if d.report.Lang != lang {
			t.Errorf("report in %q, window in %q", d.report.Lang, lang)
		}
	}
	a.tab = 0
	shotIn(t, a, "en-empty", false, "en")
	a.devices = nil
	shotIn(t, a, "de-empty", false, "de")
}

// The language menu sets the app's language.
func TestLanguagePicker(t *testing.T) {
	a := testApp()
	var got string
	a.setLocale = func(tag string) { got = tag }
	tt := ui.NewTester(a.View, 1180, 780)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("简体中文") {
		t.Fatalf("no language menu in %q", tt.Texts())
	}
	for _, label := range []string{"简体中文", "Deutsch"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if got != "de" {
		t.Errorf("chose %q, want de", got)
	}
}
