package inspect

import (
	"encoding/hex"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/ac0d3r/idevloupe/internal/devtable"
	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/pkg/services"
)

var now = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func sampleRaw() *RawData {
	f := false
	return &RawData{
		UDID: "00008110-001A2B3C4D5E6F70",
		Root: map[string]any{
			"DeviceName":                           "Ada 的 iPhone",
			"DeviceClass":                          "iPhone",
			"ProductType":                          "iPhone14,2",
			"HardwareModel":                        "D63AP",
			"ProductVersion":                       "18.6.2",
			"BuildVersion":                         "22G100",
			"SerialNumber":                         "H4K2L9QXWT",
			"ModelNumber":                          "MLTE3",
			"RegionInfo":                           "CH/A",
			"ProductionSOC":                        true,
			"ActivationState":                      "Activated",
			"InternationalMobileEquipmentIdentity": "356789104523178",
			"TelephonyCapability":                  true,
			"BasebandVersion":                      "4.06.02",
			"SIMStatus":                            "kCTSIMSupportSIMStatusReady",
			"UniqueChipID":                         uint64(0x1A2B3C4D5E6F70),
			"ChipID":                               uint64(0x8110),
			"DeviceEnclosureColor":                 "#f1f2ed",
			"PasswordProtected":                    true,
		},
		Domains: map[string]map[string]any{
			"com.apple.disk_usage":       {"TotalDiskCapacity": uint64(256_000_000_000), "AmountDataAvailable": uint64(100e9)},
			"com.apple.fmip":             {"IsAssociated": false},
			"com.apple.mobile.chaperone": {"DeviceIsChaperoned": false},
		},
		BatteryReg: map[string]any{
			"DesignCapacity":        uint64(3095),
			"NominalChargeCapacity": uint64(2740),
			"CycleCount":            uint64(412),
			"CurrentCapacity":       uint64(76),
			"Temperature":           uint64(3010),
			"Voltage":               uint64(3990),
			"InstantAmperage":       uint64(0xFFFFFFFFFFFFFF38), // -200 mA
			"Serial":                "F8Y1234ABCD",
		},
		Apps:  []services.App{{BundleID: "com.apple.mobilesafari", Type: "System"}},
		AFC2:  &f,
		MC:    &services.MCInfo{},
		Parts: map[string]map[string]any{"pearl": {"PearlSelfTestResult": uint64(0), "FDRValidated": true}},
	}
}

func find(t *testing.T, r *Report, title string) Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Title == title {
			return c
		}
	}
	t.Fatalf("no check %q in %v", title, r.Checks)
	return Check{}
}

func TestCleanDevice(t *testing.T) {
	r := Analyze(sampleRaw(), now, "zh-Hans")
	if r.Summary.Marketing != "iPhone 13 Pro" || r.Summary.Capacity != "256 GB" {
		t.Errorf("summary %+v", r.Summary)
	}
	for _, title := range []string{"设备型号", "机器类型", "销售地区", "量产芯片", "IMEI", "激活锁", "越狱检测", "远程管理 (MDM)", "基带"} {
		if c := find(t, r, title); c.Status != Pass {
			t.Errorf("%s: %v %q %q", title, c.Status, c.Value, c.Detail)
		}
	}
	b := r.Battery
	if b.HealthPercent < 88.4 || b.HealthPercent > 88.6 {
		t.Errorf("health %.2f, want 88.5", b.HealthPercent)
	}
	if b.AmperageMA != -200 || b.TemperatureC != 30.1 {
		t.Errorf("battery %+v", b)
	}
	if v, st := r.Verdict(); st != Pass {
		t.Errorf("verdict %q %v; checks %+v", v, st, r.Checks)
	}
	if !strings.Contains(r.Text(), "iPhone 13 Pro") {
		t.Error("text report lacks model")
	}
	if _, err := r.JSON(); err != nil {
		t.Fatal(err)
	}
}

func TestProblems(t *testing.T) {
	raw := sampleRaw()
	raw.Root["ModelNumber"] = "FLTE3"
	raw.Root["ProductionSOC"] = false
	raw.Root["InternationalMobileEquipmentIdentity"] = "356789104523171"
	delete(raw.Root, "BasebandVersion")
	raw.Domains["com.apple.fmip"]["IsAssociated"] = true
	raw.Apps = append(raw.Apps, services.App{BundleID: "com.opa334.TrollStore", Name: "TrollStore"})
	raw.MC = &services.MCInfo{Cloud: map[string]any{"OrganizationName": "Acme", "ConfigurationURL": "https://mdm.example", "IsSupervised": true}}
	raw.BatteryReg["NominalChargeCapacity"] = uint64(2300)
	raw.PlatformReg = map[string]any{"serial-number": []byte("H4K2L9QXWZ\x00")}

	r := Analyze(raw, now, "zh-Hans")
	want := map[string]Status{
		"机器类型":       Warn,
		"量产芯片":       Fail,
		"IMEI":       Fail,
		"基带":         Fail,
		"激活锁":        Warn,
		"越狱检测":       Fail,
		"远程管理 (MDM)": Fail,
		"监管模式":       Warn,
		"电池健康":       Warn,
		"序列号":        Fail,
	}
	for title, st := range want {
		if c := find(t, r, title); c.Status != st {
			t.Errorf("%s: got %v want %v (%q)", title, c.Status, st, c.Detail)
		}
	}
	if _, st := r.Verdict(); st != Fail {
		t.Errorf("verdict %v", st)
	}
}

func TestMissingDataIsUnknown(t *testing.T) {
	raw := &RawData{Root: map[string]any{"ProductType": "iPhone15,2"}, Domains: map[string]map[string]any{}, AppsErr: errTest{}, MCErr: errTest{}}
	r := Analyze(raw, now, "zh-Hans")
	for _, title := range []string{"激活锁", "越狱检测", "远程管理 (MDM)", "电池健康", "量产芯片", "序列号"} {
		if c := find(t, r, title); c.Status != Unknown {
			t.Errorf("%s: %v, want Unknown when data is missing", title, c.Status)
		}
	}
}

type errTest struct{}

func (errTest) Error() string { return "test" }

func TestDecodeSerial(t *testing.T) {
	// F = 2011 or 2021 first half, 'C' = week 10.
	d, ok := DecodeSerial("C39FC1234567", 2019, now)
	if !ok || d.Year != 2021 || d.Week != 10 {
		t.Errorf("got %+v %v", d, ok)
	}
	// 'Z' = second half of 2019, '3' = week 3 + 26.
	d, ok = DecodeSerial("F2LZ3ABCDEFG", 2019, now)
	if !ok || d.Year != 2019 || d.Week != 29 {
		t.Errorf("got %+v %v", d, ok)
	}
	if _, ok := DecodeSerial("H4K2L9QXWT", 2021, now); ok {
		t.Error("random 10-char serial decoded")
	}
}

func TestLuhn(t *testing.T) {
	if !LuhnValid("490154203237518") {
		t.Error("valid IMEI rejected")
	}
	if LuhnValid("490154203237519") {
		t.Error("invalid IMEI accepted")
	}
}

func TestUnitKind(t *testing.T) {
	for in, want := range map[string]Status{"MLTE3": Pass, "FLTE3": Warn, "NLTE3": Warn, "3A123": Fail, "": Unknown} {
		if got := UnitKindOf(in, "").Status; got != want {
			t.Errorf("%q: %v want %v", in, got, want)
		}
	}
}

func TestIpswDerivedChecks(t *testing.T) {
	raw := sampleRaw()
	raw.Root["BasebandStatus"] = "BBInfoUnavailable"
	raw.Root["NonVolatileRAM"] = map[string]any{"boot-args": []byte("-v serial=3\x00")}
	raw.Root["BrickState"] = true
	raw.Domains["com.apple.security.mac.amfi"] = map[string]any{"DeveloperModeStatus": true}
	raw.DiagAll = map[string]any{"GasGauge": map[string]any{"CycleCount": uint64(300)}}
	r := Analyze(raw, now, "zh-Hans")
	for title, st := range map[string]Status{"基带": Warn, "启动参数": Warn, "锁定状态": Warn, "开发者模式": Info, "电池数据一致性": Info} {
		if c := find(t, r, title); c.Status != st {
			t.Errorf("%s: %v want %v (%q)", title, c.Status, st, c.Detail)
		}
	}

	// Without IORegistry the gas gauge still gives the health.
	raw = sampleRaw()
	raw.BatteryReg = nil
	raw.DiagAll = map[string]any{"GasGauge": map[string]any{"CycleCount": uint64(80), "DesignCapacity": uint64(3000), "FullChargeCapacity": uint64(2700)}}
	r = Analyze(raw, now, "zh-Hans")
	if c := find(t, r, "电池健康"); c.Value != "90%" {
		t.Errorf("gas gauge health %+v", c)
	}
}

// Values of a real iPhone 12 whose battery iOS reports as not
// genuine: an over-capacity aftermarket cell with few cycles in an old unit,
// and Find My shown only through NVRAM.
func TestRealIPhone12AftermarketBattery(t *testing.T) {
	raw := &RawData{
		Root: map[string]any{
			"ProductType": "iPhone13,2", "DeviceClass": "iPhone", "SerialNumber": "DNPGL1CQ0DYL",
			"ModelNumber": "MGGU3", "RegionInfo": "CH/A", "ProductionSOC": true, "TelephonyCapability": true,
			"InternationalMobileEquipmentIdentity": "350199577093731", "BasebandVersion": "8.10.01",
			"BasebandStatus": "BBInfoAvailable", "ActivationState": "Activated",
			"BasebandSerialNumber": []byte{0xa1, 0x7d, 0xe3, 0x9f},
			"NonVolatileRAM": map[string]any{
				"boot-args": "", "fm-activation-locked": "YES", "fm-account-masked": "h•••••@qq.com",
			},
		},
		Domains: map[string]map[string]any{
			"com.apple.mobile.software_behavior": {"ChinaBrick": true},
		},
		BatteryReg: map[string]any{
			"CycleCount": uint64(14), "CurrentCapacity": uint64(79), "MaxCapacity": uint64(100),
			"Serial": "B055422LG26041BQ1",
			"BatteryData": map[string]any{
				"DesignCapacity": uint64(2798), "NominalChargeCapacity": uint64(3025),
				"AppleRawMaxCapacity": uint64(3024), "FullChargeCapacity": uint64(3024),
			},
		},
		DiagAll: map[string]any{"GasGauge": map[string]any{"CycleCount": uint64(14), "FullChargeCapacity": uint64(100), "Status": "Success"}},
		MC:      &services.MCInfo{Cloud: map[string]any{"IsSupervised": false}},
	}
	r := Analyze(raw, now, "zh-Hans")
	// Capacity above design is no evidence by itself: the health is what
	// the capacities give. The cell made after the device is.
	if c := find(t, r, "电池健康"); c.Status != Pass || c.Value != "108%" {
		t.Errorf("health: %+v", c)
	}
	src := find(t, r, "电池来源")
	if src.Status != Warn || !strings.Contains(src.Detail, "晚于整机") {
		t.Errorf("battery source: %+v", src)
	}
	if c := find(t, r, "激活锁"); c.Status != Warn || !strings.Contains(c.Detail, "qq.com") {
		t.Errorf("find my: %+v", c)
	}
	if c := find(t, r, "监管模式"); c.Status != Pass {
		t.Errorf("supervision: %+v", c)
	}
	if c := find(t, r, "地区一致性"); c.Status != Pass {
		t.Errorf("region: %+v", c)
	}
	if got := FormatValue(raw.Root["BasebandSerialNumber"]); got != "a17de39f" {
		t.Errorf("binary value shown as %q, want hex", got)
	}
}

func TestAnalyticsHealth(t *testing.T) {
	raw := sampleRaw() // gauge without MaximumCapacityPercent: 88.5% by capacity
	raw.Analytics = &services.AnalyticsBattery{File: "/log-aggregated-2026-11-04-080000.ips", Date: now.Add(-24 * time.Hour),
		Values: map[string]int64{"MaximumCapacityPercent": 86, "CycleCount": 410}}
	r := Analyze(raw, now, "zh-Hans")
	c := find(t, r, "电池健康")
	if c.Value != "86%" || !strings.Contains(c.Detail, "分析日志") || !strings.Contains(c.Detail, "88.5%") {
		t.Errorf("health %+v", c)
	}
	if c := find(t, r, "电池来源"); c.Status != Pass {
		t.Errorf("410 logged vs 412 now is normal growth: %+v", c)
	}

	raw.Analytics, raw.AnalyticsErr = nil, services.ErrNoAnalytics
	if c := find(t, Analyze(raw, now, "zh-Hans"), "电池健康"); !strings.Contains(c.Detail, "共享 iPhone 分析") {
		t.Errorf("missing-log note absent: %+v", c)
	}
}

func TestParseAnalytics(t *testing.T) {
	data := []byte(`{"last_value_CycleCount":10,"last_value_Foo":3}
{"last_value_MaximumCapacityPercent": 87,"last_value_CycleCount":12,"last_value_AppleRawMaxCapacity":2701}`)
	got := services.ParseAnalyticsBattery(data)
	if got["CycleCount"] != 12 || got["MaximumCapacityPercent"] != 87 || got["AppleRawMaxCapacity"] != 2701 {
		t.Errorf("parsed %v", got)
	}
	if _, ok := got["Foo"]; ok {
		t.Error("non-battery key kept")
	}
}

// A genuine iPhone 16 logged a peak full charge of 105.05% of
// its design capacity: a new genuine cell at that level is no warning.
func TestGenuineNewCellNotOverCapacity(t *testing.T) {
	raw := sampleRaw()
	raw.Root["ProductType"] = "iPhone17,3"
	raw.BatteryReg["DesignCapacity"] = uint64(3521)
	raw.BatteryReg["NominalChargeCapacity"] = uint64(3699)
	raw.BatteryReg["CycleCount"] = uint64(3)
	r := Analyze(raw, now, "zh-Hans")
	if c := find(t, r, "电池来源"); c.Status != Pass {
		t.Errorf("genuine new cell flagged: %+v", c)
	}
	if c := find(t, r, "电池健康"); c.Status != Pass {
		t.Errorf("health %+v", c)
	}
}

// Fixes from the code audit.
func TestAuditFixes(t *testing.T) {
	// A device tree without region-info is no mismatch.
	raw := sampleRaw()
	raw.PlatformReg = map[string]any{"model-number": []byte("MLTE3\x00")}
	r := Analyze(raw, now, "zh-Hans")
	for _, c := range r.Checks {
		if c.Title == "型号（部件号）" {
			t.Errorf("compared an incomplete part number: %+v", c)
		}
	}
	// A complete one in the device tree is compared with lockdown's.
	for region, want := range map[string]Status{"CH/A": Pass, "LL/A": Fail} {
		raw.PlatformReg = map[string]any{"model-number": []byte("MLTE3\x00"), "region-info": []byte(region + "\x00")}
		c := find(t, Analyze(raw, now, "zh-Hans"), "型号（部件号）")
		if c.Status != want {
			t.Errorf("device tree region %s: %+v, want %v", region, c, want)
		}
		// No factory record, as from iOS 17.4: the value read, no factory's.
		if c.Read.Value != "MLTE3CH/A" || c.Factory.Value != "" {
			t.Errorf("columns without a factory record: %+v", c)
		}
	}

	// The app list unread: no clean bill for jailbreaks.
	raw = sampleRaw()
	raw.Apps, raw.AppsErr = nil, errTest{}
	if c := find(t, Analyze(raw, now, "zh-Hans"), "越狱检测"); c.Status != Unknown {
		t.Errorf("jailbreak with apps unread: %+v", c)
	}

	// The gas gauge's FullChargeCapacity is a percent: no 3.6% health.
	raw = sampleRaw()
	raw.BatteryReg = nil
	raw.DiagAll = map[string]any{"GasGauge": map[string]any{"CycleCount": uint64(14), "FullChargeCapacity": uint64(100)}}
	r = Analyze(raw, now, "zh-Hans")
	if c := find(t, r, "电池健康"); c.Status != Unknown {
		t.Errorf("health from a percent: %+v", c)
	}
	if c := find(t, r, "电池来源"); c.Status != Pass {
		t.Errorf("cycles alone were weighed: %+v", c)
	}

	// Over capacity with a health iOS reports: the health stands.
	raw = sampleRaw()
	raw.BatteryReg["NominalChargeCapacity"] = uint64(3400) // 110% of 3095
	raw.BatteryReg["MaximumCapacityPercent"] = uint64(100)
	r = Analyze(raw, now, "zh-Hans")
	if c := find(t, r, "电池健康"); c.Value != "100%" || c.Status != Pass {
		t.Errorf("reported health overridden: %+v", c)
	}
	if c := find(t, r, "电池来源"); c.Status != Pass {
		t.Errorf("capacity above design taken for evidence: %+v", c)
	}

	// Nothing to weigh: unknown, not a clean bill.
	raw = sampleRaw()
	raw.BatteryReg = nil
	raw.Domains["com.apple.mobile.battery"] = map[string]any{"BatteryCurrentCapacity": uint64(50)}
	if c := find(t, Analyze(raw, now, "zh-Hans"), "电池来源"); c.Status != Unknown {
		t.Errorf("battery source without data: %+v", c)
	}
}

// Part entries as an iPhone X reports them.
func TestPartsFromRealIPhoneX(t *testing.T) {
	raw := sampleRaw()
	raw.Parts = map[string]map[string]any{
		"camera": {
			"BackCameraModuleSerialNumString":                "DN880159WHNH8MH1H",
			"BackTeleCameraModuleSerialNumString":            "DN88015M71HH8WLRP",
			"FrontCameraModuleSerialNumString":               "G8R7491GQYPHNXMAJ",
			"FrontIRCameraModuleSerialNumString":             "DNM75132BKVH0N545",
			"FrontIRStructuredLightProjectorSerialNumString": "DN875235AV1JJGXAS",
			"BackCameraSerialNumber":                         "0824fa4d616be3e8",
		},
		"pearl": {"PearlSelfTestResult": uint64(8)},
		"product": {
			"raw-panel-serial-number":  []byte("FXV73552871J6DX90+01009136794159075694800743+G9N80242JSFH8PRA9\x00"),
			"coverglass-serial-number": []byte("FXV73552871J6DX90+01009136794159075694800743\x00"),
		},
		"touch":   {"Multitouch ID": uint64(144115188075855968)},
		"storage": {"Serial Number": "0b28005122948655"},
	}
	r := Analyze(raw, now, "zh-Hans")
	if c := find(t, r, "面容 ID 自检"); c.Status != Info || !strings.Contains(c.Value, "8") || !strings.Contains(c.Detail, "录入面容") {
		t.Errorf("failed Face ID self test: %+v", c)
	}
	var sec *Section
	for i := range r.Sections {
		if r.Sections[i].Title == "部件序列号（当前值）" {
			sec = &r.Sections[i]
		}
	}
	if sec == nil {
		t.Fatal("no parts section")
	}
	got := map[string]string{}
	for _, f := range sec.Fields {
		got[f.Label] = f.Value
	}
	want := map[string]string{
		"后置广角摄像头": "DN880159WHNH8MH1H", "后置长焦摄像头": "DN88015M71HH8WLRP", "前置摄像头": "G8R7491GQYPHNXMAJ",
		"面容 ID 红外摄像头": "DNM75132BKVH0N545", "面容 ID 点阵投影器": "DN875235AV1JJGXAS",
		"屏幕盖板": "FXV73552871J6DX90", "屏幕面板记录": "FXV73552871J6DX90+01009136794159075694800743+G9N80242JSFH8PRA9", "触控模组 ID": "144115188075855968", "存储芯片": "0b28005122948655",
		"电池": "F8Y1234ABCD",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if !strings.Contains(sec.Note, "与出厂记录") || !strings.Contains(sec.Note, "读不到时") {
		t.Error("parts section lacks its caveat")
	}
}

// An iPhone 16 with working Face ID: self test 0, and
// sensors the iPhone X does not report.
func TestPartsFromRealIPhone16(t *testing.T) {
	raw := sampleRaw()
	raw.Parts = map[string]map[string]any{
		"pearl": {"PearlSelfTestResult": uint64(0)},
		"camera": {
			"BackCameraModuleSerialNumString":          "DNMHFC01BUS000048L",
			"BackSuperWideCameraModuleSerialNumString": "GCFHFS00WVC000049K",
		},
		"product": {
			"ambient-light-sensor-serial-num": "031A0FED676F",
			"coverglass-serial-number":        "GR9HEP02Y310000NDD+9000000000000000000000000",
		},
	}
	r := Analyze(raw, now, "zh-Hans")
	if c := find(t, r, "面容 ID 自检"); c.Status != Pass {
		t.Errorf("working Face ID: %+v", c)
	}
	got := map[string]string{}
	for _, sec := range r.Sections {
		for _, f := range sec.Fields {
			got[f.Label] = f.Value
		}
	}
	for k, v := range map[string]string{"后置超广角摄像头": "GCFHFS00WVC000049K", "环境光传感器": "031A0FED676F", "屏幕盖板": "GR9HEP02Y310000NDD"} {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

// Four real phones and what was known of them.
func TestFaceIDRules(t *testing.T) {
	cases := []struct {
		name  string
		pt    string
		pearl map[string]any
		want  Status
	}{
		{"iPhone 12, Face ID works", "iPhone13,2", map[string]any{"PearlSelfTestResult": uint64(0), "FDRValidated": true}, Pass},
		{"iPhone X, Face ID broken (iOS 16, no FDRValidated): test by hand", "iPhone10,3", map[string]any{"PearlSelfTestResult": uint64(8)}, Info},
		{"FDR mismatch", "iPhone17,3", map[string]any{"PearlSelfTestResult": uint64(0), "FDRValidated": false}, Warn},
		{"Face ID model, nothing read", "iPhone17,3", nil, Unknown},
	}
	for _, c := range cases {
		raw := sampleRaw()
		raw.Root["ProductType"] = c.pt
		raw.Parts = map[string]map[string]any{}
		if c.pearl != nil {
			raw.Parts["pearl"] = c.pearl
		}
		if got := find(t, Analyze(raw, now, "zh-Hans"), "面容 ID 自检"); got.Status != c.want {
			t.Errorf("%s: %+v, want %v", c.name, got, c.want)
		}
	}
	// iPhone SE (2nd generation) has Touch ID: no Face ID item.
	raw := sampleRaw()
	raw.Root["ProductType"] = "iPhone12,8"
	// The sensor reports its serial as 16 raw bytes (iPhone SE 2, iPhone 8).
	raw.Parts = map[string]map[string]any{"touchid": {"SerialNumber": []byte{0x02, 0x19, 0x60, 0x3c, 0x5f, 0x22, 0xca, 0x22, 0x60, 0x64, 0x9a, 0x0b, 0xfe, 0x01, 0x00, 0xcf}}}
	r := Analyze(raw, now, "zh-Hans")
	for _, c := range r.Checks {
		if c.Title == "面容 ID 自检" {
			t.Errorf("Touch ID model got a Face ID item: %+v", c)
		}
	}
	found := false
	for _, sec := range r.Sections {
		for _, f := range sec.Fields {
			found = found || (f.Label == "指纹传感器" && f.Value == "0219603c5f22ca2260649a0bfe0100cf")
		}
	}
	if !found {
		t.Error("Touch ID sensor serial missing")
	}
	for pt, want := range map[string]bool{"iPhone10,3": true, "iPhone10,1": false, "iPhone12,8": false, "iPhone14,6": false, "iPhone17,5": true, "iPhone11,8": true} {
		if HasFaceID(pt) != want {
			t.Errorf("HasFaceID(%s) = %v", pt, !want)
		}
	}
}

// iPhone 16: the gauge unread, the analytics log gives the cycles.
func TestCyclesFromLog(t *testing.T) {
	raw := sampleRaw()
	raw.BatteryReg = nil
	raw.Domains["com.apple.mobile.battery"] = map[string]any{"BatteryCurrentCapacity": uint64(50)}
	raw.Analytics = &services.AnalyticsBattery{File: "/Retired/Analytics-2026-11-05-074144.ips.ca.synced", Date: now,
		Values: map[string]int64{"MaximumCapacityPercent": 91, "CycleCount": 548}}
	r := Analyze(raw, now, "zh-Hans")
	if c := find(t, r, "充电循环"); c.Value != "548 次" || !strings.Contains(c.Detail, "分析日志") {
		t.Errorf("cycles: %+v", c)
	}
	if c := find(t, r, "电池来源"); c.Status != Info && c.Status != Pass {
		t.Errorf("the log compared with itself: %+v", c)
	}
}

// iPhone 8: Touch ID; its camera driver fills the Face ID projector
// it lacks with "XXXXXXXX".
func TestPartsFromRealIPhone8(t *testing.T) {
	raw := sampleRaw()
	raw.Root["ProductType"] = "iPhone10,1"
	raw.Parts = map[string]map[string]any{
		"camera": {
			"BackCameraModuleSerialNumString":                "GCF94251944HCDC49",
			"FrontCameraModuleSerialNumString":               "F0W91630FUCHJL32X",
			"FrontIRStructuredLightProjectorSerialNumString": "XXXXXXXX",
		},
		"touchid": {"SerialNumber": []byte{0x02, 0x17, 0x10, 0x6c, 0x60, 0x80, 0x85, 0x29, 0x3b, 0x5a, 0x13, 0x69, 0x06, 0x02, 0x00, 0xd5}},
	}
	r := Analyze(raw, now, "zh-Hans")
	got := map[string]string{}
	for _, sec := range r.Sections {
		for _, f := range sec.Fields {
			got[f.Label] = f.Value
		}
	}
	if _, ok := got["面容 ID 点阵投影器"]; ok {
		t.Errorf("placeholder shown as a Face ID part: %q", got["面容 ID 点阵投影器"])
	}
	if got["指纹传感器"] != "0217106c608085293b5a1369060200d5" || got["前置摄像头"] != "F0W91630FUCHJL32X" {
		t.Errorf("parts %v", got)
	}
	for _, c := range r.Checks {
		if c.Title == "面容 ID 自检" {
			t.Errorf("iPhone 8 got a Face ID item: %+v", c)
		}
	}
}

// iPhone 8: 85% health, yet iOS asks for service; the gauge's
// resistance metric is high. It is shown, not judged: three samples of
// different cells give no threshold.
func TestResistanceMetric(t *testing.T) {
	raw := sampleRaw()
	raw.BatteryReg["BatteryData"] = map[string]any{"BatteryHealthMetric": uint64(454), "ChemicalWeightedRa": uint64(454)}
	r := Analyze(raw, now, "zh-Hans")
	if r.Battery.ResistanceMetric != 454 || !strings.Contains(r.Text(), "内阻指标：454") {
		t.Errorf("metric %d", r.Battery.ResistanceMetric)
	}
	// iOS 17 on: from the analytics log.
	raw = sampleRaw()
	raw.Analytics = &services.AnalyticsBattery{Date: now, Values: map[string]int64{"BatteryHealthMetric": 116}}
	if r := Analyze(raw, now, "zh-Hans"); r.Battery.ResistanceMetric != 116 {
		t.Errorf("metric from log %d", r.Battery.ResistanceMetric)
	}
}

// Touch ID from the drivers of an iPhone 8 (iOS 16) and an iPhone SE 2
// (iOS 27), both working.
func TestTouchIDRules(t *testing.T) {
	sensor := map[string]any{"SerialNumber": []byte{0x02, 0x17}}
	cases := []struct {
		name  string
		pt    string
		parts map[string]map[string]any
		want  Status
	}{
		{"iPhone 8: self test OK", "iPhone10,1", map[string]map[string]any{
			"mesa": {"MesaSelfTest": "OK"}, "mesadriver": {"MesaCalBlobSource": "FDR"}, "touchid": sensor}, Pass},
		{"SE 2 on iOS 27: no self test, sensor as a working one", "iPhone12,8", map[string]map[string]any{
			"mesa": {"name": "aop-mesa"}, "mesadriver": {"MesaCalBlobSource": "FDR", "ScanningState": uint64(0)},
			"touchid": {"SerialNumber": []byte{0x02, 0x19}, "mesa-state": uint64(30), "SensorID": uint64(13138)}}, Pass},
		{"SE 2 read again with the sensor idle (mesa-state is a power state)", "iPhone12,8", map[string]map[string]any{
			"mesa": {"name": "aop-mesa"}, "mesadriver": {"MesaCalBlobSource": "FDR", "ScanningState": uint64(0)},
			"touchid": {"SerialNumber": []byte{0x02, 0x19}, "mesa-state": uint64(1), "SensorID": uint64(13138)}}, Pass},
		{"no self test, calibration not from FDR", "iPhone12,8", map[string]map[string]any{
			"mesadriver": {"MesaCalBlobSource": "Default"}, "touchid": {"SerialNumber": []byte{0x02, 0x19}}}, Info},
		{"no self test, sensor without a serial", "iPhone12,8", map[string]map[string]any{
			"mesadriver": {"MesaCalBlobSource": "FDR"}, "touchid": {"mesa-state": uint64(30)}}, Info},
		{"self test failed", "iPhone10,1", map[string]map[string]any{
			"mesa": {"MesaSelfTest": "Failed"}, "touchid": sensor}, Warn},
		{"sensor missing", "iPhone10,1", map[string]map[string]any{
			"camera": {"BackCameraModuleSerialNumString": "GCF94251944HCDC49"}}, Warn},
		{"nothing read", "iPhone10,1", map[string]map[string]any{}, Unknown},
	}
	for _, c := range cases {
		raw := sampleRaw()
		raw.Root["ProductType"] = c.pt
		raw.Parts = c.parts
		if got := find(t, Analyze(raw, now, "zh-Hans"), "指纹（Touch ID）"); got.Status != c.want {
			t.Errorf("%s: %+v, want %v", c.name, got, c.want)
		}
	}
	// A Face ID model gets no Touch ID item.
	raw := sampleRaw()
	for _, c := range Analyze(raw, now, "zh-Hans").Checks {
		if c.Title == "指纹（Touch ID）" {
			t.Errorf("Face ID model got a Touch ID item: %+v", c)
		}
	}
}

// The worst issues lead: failures, then warnings, a lock or a part before
// the battery.
func TestIssuesOrder(t *testing.T) {
	r := &Report{Lang: "zh-Hans", Checks: []Check{
		{Category: "电池", Title: "电池健康", Value: "76%", Status: Warn},
		{Category: "电池", Title: "充电循环", Value: "1575 次", Status: Warn},
		{Category: "部件", Title: "面容 ID 自检", Value: "未通过（代码 8）", Status: Warn},
		{Category: "基本信息", Title: "设备型号", Status: Pass},
		{Category: "安全", Title: "激活锁", Status: Unknown},
		{Category: "电池", Title: "电池来源", Value: "疑似非原装 / 已更换", Status: Fail},
	}}
	var got []string
	for _, c := range r.Issues() {
		got = append(got, c.Headline())
	}
	want := []string{"电池来源 疑似非原装 / 已更换", "面容 ID 自检 未通过（代码 8）", "电池健康 76%", "充电循环 1575 次"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("issues\n got %q\nwant %q", got, want)
	}
	if !strings.Contains(r.Text(), "主要问题：电池来源 疑似非原装 / 已更换 · 面容 ID 自检") {
		t.Errorf("text report lacks the issues:\n%s", r.Text())
	}
}

// The SIM owner's phone number, ICCID and IMSI never reach a report, also
// not inside CarrierBundleInfoArray (as on an iPhone 16).
func TestScrubSubscriber(t *testing.T) {
	root := map[string]any{
		"PhoneNumber":                            "+49 152 31415926",
		"IntegratedCircuitCardIdentity":          "89492026267777777777",
		"IntegratedCircuitCardIdentity2":         "89860000008888888888",
		"InternationalMobileSubscriberIdentity":  "262029999999999",
		"InternationalMobileSubscriberIdentity2": "460116666666666",
		"InternationalMobileEquipmentIdentity":   "356000000000000",
		"SIMStatus":                              "kCTSIMSupportSIMStatusReady",
		"CarrierBundleInfoArray": []any{map[string]any{
			"CFBundleIdentifier": "com.apple.Vodafone_de", "ICCID": "89492026267777777777",
			"IntegratedCircuitCardIdentity": "89492026267777777777", "InternationalMobileSubscriberIdentity": "262029999999999", "MCC": "262",
		}},
	}
	scrubSubscriber(root)
	raw := sampleRaw()
	maps.Copy(raw.Root, root)
	r := Analyze(raw, now, "zh-Hans")
	text := r.Text()
	js, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"31415926", "7777777777", "8888888888", "9999999999", "6666666666", "PhoneNumber", "手机号"} {
		if strings.Contains(text, secret) || strings.Contains(string(js), secret) {
			t.Errorf("report contains %q", secret)
		}
	}
	for _, keep := range []string{"356000000000000", "com.apple.Vodafone_de", "SIMStatus"} {
		if !strings.Contains(string(js), keep) {
			t.Errorf("report lost %q", keep)
		}
	}
}

// The language changes the words only: the same raw data makes the same
// checks with the same statuses in every language, and a report in
// English or German has no Chinese but what the device itself reports.
func TestLanguages(t *testing.T) {
	problems := sampleRaw()
	problems.Root["ModelNumber"] = "FLTE3"
	problems.Root["ProductionSOC"] = false
	problems.Root["ReleaseType"] = "Internal"
	problems.Root["SerialNumber"] = "F17W374TJCLF"
	problems.Root["InternationalMobileEquipmentIdentity"] = "356789104523171"
	delete(problems.Root, "BasebandVersion")
	problems.Domains["com.apple.fmip"]["IsAssociated"] = true
	problems.Apps = append(problems.Apps, services.App{BundleID: "com.opa334.TrollStore", Name: "TrollStore"})
	problems.MC = &services.MCInfo{Cloud: map[string]any{"OrganizationName": "Acme", "ConfigurationURL": "https://mdm.example"}}
	problems.BatteryReg["NominalChargeCapacity"] = uint64(3400)
	problems.BatteryReg["CycleCount"] = uint64(12)
	problems.PlatformReg = map[string]any{"serial-number": []byte("H4K2L9QXWZ\x00")}
	problems.Parts = map[string]map[string]any{"pearl": {"PearlSelfTestResult": uint64(8)}}
	problems.Errors = []*StepError{{Step: StepReadDomain, Target: "com.apple.mobile.internal", Err: errors.New("InvalidDomain")}}
	problems.AnalyticsErr = services.ErrNoAnalytics
	empty := &RawData{Root: map[string]any{}}

	for _, raw := range []*RawData{sampleRaw(), problems, empty} {
		zh := Analyze(raw, now, "zh-Hans")
		for _, lang := range []string{"en", "de"} {
			r := Analyze(raw, now, lang)
			if len(r.Checks) != len(zh.Checks) {
				t.Fatalf("%s: %d checks, %d in Chinese", lang, len(r.Checks), len(zh.Checks))
			}
			for i, c := range r.Checks {
				if c.Status != zh.Checks[i].Status {
					t.Errorf("%s: %q is %v, %q in Chinese is %v", lang, c.Title, c.Status, zh.Checks[i].Title, zh.Checks[i].Status)
				}
			}
			if len(r.Issues()) != len(zh.Issues()) {
				t.Errorf("%s: %d issues, %d in Chinese", lang, len(r.Issues()), len(zh.Issues()))
			}
			// The device's name is the device's: "Ada 的 iPhone".
			text := strings.ReplaceAll(r.Text(), r.Summary.DeviceName, "")
			if i := strings.IndexFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }); i >= 0 {
				t.Errorf("%s report has Chinese: …%s…", lang, text[max(0, i-60):min(len(text), i+60)])
			}
		}
	}
	if got := find(t, Analyze(problems, now, "de"), "Face ID-Selbsttest").Value; got != "Code 8, bitte testen" {
		t.Errorf("German Face ID value %q", got)
	}
	if got := find(t, Analyze(problems, now, "en"), "Battery health").Value; got != "110%" {
		t.Errorf("English health value %q", got)
	}
}

// The overview and the report read Find My the same way, NVRAM included
// when the fmip domain is missing.
func TestOverviewFindMyFromNVRAM(t *testing.T) {
	raw := &RawData{
		Root: map[string]any{"ProductType": "iPhone14,2", "NonVolatileRAM": map[string]any{
			"fm-activation-locked": []byte("YES"), "fm-account-masked": []byte("a•••@icloud.com"),
		}},
		Domains: map[string]map[string]any{},
	}
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	o := Describe(raw, now, "en")
	got := ""
	for _, f := range o.Fields {
		if f.Label == "Activation Lock" {
			got = f.Value
		}
	}
	var check Check
	for _, c := range Analyze(raw, now, "en").Checks {
		if c.Title == "Activation Lock" {
			check = c
		}
	}
	if check.Status != Warn || got == "" || got != check.Value {
		t.Errorf("overview %q, report %+v", got, check)
	}
}

// sysCfg builds a factory configuration of text values.
func sysCfg(vals map[string]string) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range vals {
		out[k] = []byte(v)
	}
	return out
}

// The factory record against the parts, as on an iPhone 8 whose front
// camera, panel and glass were replaced and an iPhone X whose record all
// matches. Serials keep the real ones' form, dates included, with made-up
// tails.
func TestFactoryRecord(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	raw := &RawData{
		Root: map[string]any{"ProductType": "iPhone10,1", "SerialNumber": "FFNZMYBEAAAA", "ModelNumber": "MQ722", "RegionInfo": "LL/A",
			"MLBSerialNumber": "FG394410AAAAAAAAA", "DeviceClass": "iPhone"},
		Domains:    map[string]map[string]any{},
		BatteryReg: map[string]any{"Serial": "FG99425SAAAAAAAAA", "DesignCapacity": uint64(1810), "NominalChargeCapacity": uint64(1539)},
		Gestalt:    map[string]any{},
		Parts: map[string]map[string]any{
			"camera": {"BackCameraModuleSerialNumString": "GCF94251AAAAAAAAA", "FrontCameraModuleSerialNumString": "F0W91630BBBBBBBBB"},
			"product": {"raw-panel-serial-number": "FVQ9037BBBBBBBBBB+T326M9J1+C716828C+M8C81LLK",
				"coverglass-serial-number": "FXN85033BBBBBBBBB+0101414578"},
			"touchid": {"SerialNumber": []byte{0x02, 0x17, 0x10, 0x6c, 0x60, 0x80, 0x85, 0x29, 0x3b, 0x5a, 0x13, 0x69, 0x06, 0x02, 0x00, 0xd5}},
		},
		SysCfg: sysCfg(map[string]string{
			"SrNm": "FFNZMYBEAAAA", "Mod#": "MQ722", "Regn": "LL/A", "MLB#": "FG394410AAAAAAAAA",
			"Batt": "FG99425SAAAAAAAAA\x00", "BCMS": "GCF94251AAAAAAAAA", "FCMS": "F0W94170AAAAAAAAA",
			"LCM#": "DKH94342AAAAAAAAA+S12657P0+R270819A+M9A41UBL", "MtSN": "FXR93575AAAAAAAAA+0102313980",
		}),
	}
	raw.SysCfg["NSrN"] = []byte{0x01, 0, 0, 0, 0x6c, 0x10, 0x17, 0x02, 0x29, 0x85, 0x80, 0x60, 0x69, 0x13, 0x5a, 0x3b, 0xd5, 0x00, 0x02, 0x06}
	got := map[string]Check{}
	for _, c := range Analyze(raw, now, "zh-Hans").Checks {
		got[c.Title] = c
	}
	for title, want := range map[string]Status{
		"电池与出厂记录": Pass, "后置广角摄像头与出厂记录": Pass, "指纹传感器与出厂记录": Pass,
		"前置摄像头与出厂记录": Warn, "屏幕面板与出厂记录": Warn, "屏幕盖板与出厂记录": Warn,
		"序列号": Pass, "型号（部件号）": Pass, "主板序列号": Pass,
	} {
		if c, ok := got[title]; !ok || c.Status != want {
			t.Errorf("%s: %+v, want %v", title, c, want)
		}
	}
	front := got["前置摄像头与出厂记录"]
	if front.Name() != "前置摄像头" || front.Factory.Value != "F0W94170AAAAAAAAA" || front.Factory.Date != "2019-10-13" || front.Read.Date != "2019-04-17" {
		t.Errorf("front camera columns %+v", front)
	}
	for _, w := range []string{"F0W94170AAAAAAAAA（约 2019-10-13 生产）", "F0W91630BBBBBBBBB（约 2019-04-17 生产）", "可能更换过"} {
		if !strings.Contains(front.Detail, w) {
			t.Errorf("front camera detail lacks %q: %s", w, front.Detail)
		}
	}
	// A match says what it does not prove.
	if d := got["电池与出厂记录"].Detail; !strings.Contains(d, "一致不能证明未更换") {
		t.Errorf("battery match detail: %s", d)
	}
	// The serial's row holds its sources' agreement and the factory's serial.
	if c := got["序列号"]; !strings.Contains(c.Detail, "SysCfg=FFNZMYBEAAAA") || c.Factory.Value != "FFNZMYBEAAAA" || c.Read.Value != "FFNZMYBEAAAA" {
		t.Errorf("serial leaves out the factory record: %+v", c)
	}
	// The panel compares its own serial, the first of its record.
	if d := got["屏幕面板与出厂记录"].Detail; strings.Contains(d, "S12657P0") || !strings.Contains(d, "DKH94342AAAAAAAAA") {
		t.Errorf("panel detail: %s", d)
	}
	// A board serial that is not the factory's fails its consistency.
	raw.Root["MLBSerialNumber"] = "FG3OTHERBOARD0000"
	for _, c := range Analyze(raw, now, "zh-Hans").Checks {
		if c.Title == "主板序列号" && c.Status != Fail {
			t.Errorf("swapped board: %+v", c)
		}
	}

	// iPhone X: no panel or Touch ID record, all the others match.
	x := &RawData{
		Root:    map[string]any{"ProductType": "iPhone10,3"},
		Domains: map[string]map[string]any{}, Gestalt: map[string]any{},
		Parts: map[string]map[string]any{
			"camera":  {"FrontCameraModuleSerialNumString": "G8R7491GAAAAAAAAA"},
			"product": {"coverglass-serial-number": "FXV73552AAAAAAAAA+01009136"},
		},
		SysCfg: sysCfg(map[string]string{"FCMS": "G8R7491GAAAAAAAAA", "MtSN": "FXV73552AAAAAAAAA+01009136"}),
	}
	gotX := map[string]Check{}
	for _, c := range Analyze(x, now, "en").Checks {
		gotX[c.Title] = c
	}
	// A screen whose serial matches is only worth knowing: the original's
	// serial is often written onto a new screen, as on this iPhone X.
	if gotX["Front camera vs. factory record"].Status != Pass || gotX["Cover glass vs. factory record"].Status != Info {
		t.Errorf("iPhone X: %+v", gotX)
	}
	if _, ok := gotX["Display panel vs. factory record"]; ok {
		t.Error("a panel check without a panel record")
	}
	// The battery is recorded but its gauge not read.
	x.SysCfg["Batt"] = []byte("F8Y75174AAAAAAAAA")
	for _, c := range Analyze(x, now, "en").Checks {
		if c.Title == "Battery vs. factory record" && c.Status != Unknown {
			t.Errorf("battery unread: %+v", c)
		}
	}
}

// Without a record: nothing to say on an iOS that gives no MobileGestalt,
// Unknown when MobileGestalt answered without it or it did not parse.
func TestFactoryRecordUnread(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		raw  RawData
		want bool
	}{
		"deprecated": {RawData{GestaltErr: services.ErrGestaltDeprecated}, false},
		"no record":  {RawData{Gestalt: map[string]any{}}, true},
		"bad record": {RawData{Gestalt: map[string]any{}, SysCfgErr: errors.New("syscfg: not a syscfg block")}, true},
		"other":      {RawData{GestaltErr: errors.New("timeout")}, true},
		"not asked":  {RawData{}, false},
	} {
		raw := tc.raw
		raw.Root, raw.Domains = map[string]any{"ProductType": "iPhone14,2"}, map[string]map[string]any{}
		found := false
		for _, c := range Analyze(&raw, now, "en").Checks {
			if c.Title == "Factory record of parts" {
				found = c.Status == Unknown
			}
		}
		if found != tc.want {
			t.Errorf("%s: Unknown check %v, want %v", name, found, tc.want)
		}
	}
}

// Dates as 3uTools gives them for an iPhone 8's parts.
func TestPartDate(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	for serial, want := range map[string]string{
		"F0W94170AU6HJL371": "2019-10-13", "F0W91630FUCHJL32X": "2019-04-17",
		"FG99425SCDAHXYDA2": "2019-10-18", "GCF94251944HCDC49": "2019-10-18",
		"DKH943426BXHNK0A7": "2019-10-24", "FVQ9037B1XZHNJLA4": "2019-01-20",
		"FG394410MMLJ0Y1AD":                "2019-10-28",
		"0217106c608085293b5a1369060200d5": "", // the Touch ID sensor's hex
		"F0W95990AAAAAAAAA":                "", // week 59
		"F0W64570AAAAAAAAA":                "", // 2026 week 45 day 7: after now
		"SHORT":                            "",
	} {
		d, ok := PartDate(serial, now)
		got := ""
		if ok {
			got = d.Format("2006-01-02")
		}
		if got != want {
			t.Errorf("PartDate(%s) = %q, want %q", serial, got, want)
		}
	}
}

// The network addresses and the colors, as the iPhone X and the iPhone 8
// report them, and the regulatory model from the device tree or the record.
func TestAddressesAndColors(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	mac := func(s string) []byte {
		b, err := hex.DecodeString(strings.ReplaceAll(s, ":", ""))
		if err != nil {
			t.Fatal(err)
		}
		return append(b, make([]byte, 10)...)
	}
	color := func(n byte) []byte { return []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, n, 0, 0, 0} }
	raw := &RawData{
		Root: map[string]any{"ProductType": "iPhone10,3", "DeviceColor": "1",
			"WiFiAddress": "d8:8f:76:00:00:68", "BluetoothAddress": "d8:8f:76:00:00:1e", "EthernetAddress": "d8:8f:76:00:00:49"},
		Domains:     map[string]map[string]any{},
		PlatformReg: map[string]any{"regulatory-model-number": []byte("A1865\x00")},
		SysCfg: map[string][]byte{"WMac": mac("d8:8f:76:00:00:68"), "BMac": mac("d8:8f:76:00:00:1e"), "EMac": mac("d8:8f:76:00:00:49"),
			"CLCG": color(1), "CLHS": color(1), "RMd#": []byte("A1865")},
	}
	r := Analyze(raw, now, "zh-Hans")
	find := func(r *Report, title string) Check {
		for _, c := range r.Checks {
			if c.Title == title {
				return c
			}
		}
		return Check{}
	}
	if c := find(r, "蜂窝地址"); c.Status != Pass || c.Factory.Value != "d8:8f:76:00:00:49" || c.Read.Value != "d8:8f:76:00:00:49" || !c.Read.ID {
		t.Errorf("addresses %+v", c)
	}
	fields := map[string]string{}
	for _, sec := range r.Sections {
		for _, f := range sec.Fields {
			fields[f.Label] = f.Value
		}
	}
	if fields["正面颜色"] != "黑色（代码 1）" || fields["背面颜色"] != "深空灰色（代码 1）" || fields["监管型号"] != "A1865" || fields["蜂窝地址"] != "d8:8f:76:00:00:49" {
		t.Errorf("fields %v", fields)
	}
	if r.Summary.ColorName != "深空灰色" {
		t.Errorf("summary color %q", r.Summary.ColorName)
	}

	// Another Wi-Fi address than the factory's.
	raw.Root["WiFiAddress"] = "d8:8f:76:00:00:99"
	if c := find(Analyze(raw, now, "zh-Hans"), "Wi-Fi 地址"); c.Status != Warn || c.Factory.Value != "d8:8f:76:00:00:68" || !strings.Contains(c.Detail, "出厂 d8:8f:76:00:00:68，当前 d8:8f:76:00:00:99") {
		t.Errorf("a changed address %+v", c)
	}

	// iPhone 8: space gray is code 8 there; a code no sample settled stays a code.
	raw.Root["ProductType"] = "iPhone10,1"
	delete(raw.Root, "DeviceColor")
	raw.SysCfg["CLHS"] = color(8)
	fields = map[string]string{}
	for _, sec := range Analyze(raw, now, "en").Sections {
		for _, f := range sec.Fields {
			fields[f.Label] = f.Value
		}
	}
	if fields["Front color"] != "Black (code 1)" || fields["Back color"] != "Space Gray (code 8)" {
		t.Errorf("iPhone 8 colors %v", fields)
	}
	raw.SysCfg["CLHS"] = color(3)
	for _, sec := range Analyze(raw, now, "en").Sections {
		for _, f := range sec.Fields {
			if f.Label == "Back color" && f.Value != "code 3" {
				t.Errorf("unsettled code: %q", f.Value)
			}
		}
	}
}

// A part made after the device was fitted later, which needs no factory
// record: an iPhone 12 (iOS 27, no SysCfg) made in week 43 of 2021 with a
// battery its serial dates to October 2025. Serials keep the real ones'
// form, dates included, with made-up tails.
func TestPartDates(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	raw := &RawData{
		Root:       map[string]any{"ProductType": "iPhone13,2", "SerialNumber": "DNPGL1CQAAAA", "MLBSerialNumber": "F3Y143400AAAAAAA"},
		Domains:    map[string]map[string]any{},
		BatteryReg: map[string]any{"Serial": "B055422LAAAAAAAAA", "DesignCapacity": uint64(2798), "NominalChargeCapacity": uint64(2790), "CycleCount": uint64(300)},
		GestaltErr: services.ErrGestaltDeprecated,
		Parts: map[string]map[string]any{
			"camera":  {"BackCameraModuleSerialNumString": "F6D14120AAAAAAAAA", "FrontIRStructuredLightProjectorSerialNumString": "F8P13511AAAAAAAAA"},
			"product": {"coverglass-serial-number": "FXR11350AAAAAAAAA+0B0772", "raw-panel-serial-number": "G9P1236BAAAAAAAAA+A100+FXR11350AAAAAAAAA"},
		},
	}
	checks := func() map[string]Check {
		got := map[string]Check{}
		for _, c := range Analyze(raw, now, "zh-Hans").Checks {
			got[c.Title] = c
		}
		return got
	}
	got := checks()
	// Only the battery is newer, weighed as the battery's origin; the other
	// parts are older than the device.
	if c := got["电池来源"]; c.Status != Warn || !strings.Contains(c.Detail, "约 2025-10-14 生产，晚于整机（约 2021-10-31）") {
		t.Errorf("battery origin %+v", c)
	}
	if c := got["部件生产日期"]; c.Status != Pass || !strings.Contains(c.Detail, "4 个部件均早于整机（约 2021-10-31）") {
		t.Errorf("part dates %+v", c)
	}

	// A new screen: its panel made in 2024.
	raw.Parts["product"]["raw-panel-serial-number"] = "G9P4101BAAAAAAAAA+A100"
	if c := checks()["部件生产日期"]; c.Status != Warn || !strings.Contains(c.Detail, "屏幕面板：G9P4101BAAAAAAAAA（约 2024-03-04 生产）") ||
		!strings.Contains(c.Value, "1 个部件") {
		t.Errorf("a newer panel %+v", c)
	}
	// No margin: a part dated after the device's week is newer.
	raw.Parts["product"]["raw-panel-serial-number"] = "G9P1446BAAAAAAAAA"
	if c := checks()["部件生产日期"]; c.Status != Warn {
		t.Errorf("a part after the device's week %+v", c)
	}
	// One made in the device's week is not.
	raw.Parts["product"]["raw-panel-serial-number"] = "G9P1436BAAAAAAAAA"
	if c := checks()["部件生产日期"]; c.Status != Pass {
		t.Errorf("a part of the device's week %+v", c)
	}
	// With a factory record, the comparison with it stands for the parts
	// that are not newer.
	raw.SysCfg = map[string][]byte{"SrNm": []byte("DNPGL1CQAAAA")}
	if _, ok := checks()["部件生产日期"]; ok {
		t.Error("a part date check beside the factory record")
	}
	// A random serial and a board serial that tells no date: no device date.
	raw.SysCfg = nil
	raw.Root["SerialNumber"], raw.Root["MLBSerialNumber"] = "H4K2L9QXWT", ""
	if _, ok := checks()["部件生产日期"]; ok {
		t.Error("a part date check without the device's date")
	}
}

// With 3uTools' device table, the report names colors, the chip, the
// screen, the release date and the battery's maker; an unknown prefix
// names none.
func TestDeviceTable(t *testing.T) {
	tb, err := devtable.Parse([]byte(`{"Images":[{"SupportTypes":[{"ProductType":"iPhone13,2"}],"imgs":[
		{"BackColor":"9","BackColorText":"蓝色","BackColorText_en":"Blue","FrontColor":"1","FrontColorText":"黑色","FrontColorText_en":"Black"}]}],
		"devices":[{"ProductType":"iPhone13,2","ReleaseDate":"2020年10月13日","ReleaseDate_en":"10/13/2020","ScreenSize":"6.1 英寸","ScreenSize_en":"6.1 inches"}],
		"cpus":[{"productType":"iPhone13,2","displayName":"A14 六核","displayName_en":"A14 Hexa","rate":"3.1GHz","resolution":"2532 x 1170"}],
		"batfacotry":[{"cn":"常熟新普","en":"SIMPLO","prefix":"FG"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	Table = tb
	t.Cleanup(func() { Table = nil })
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	raw := &RawData{
		Root:       map[string]any{"ProductType": "iPhone13,2", "DeviceColor": "1", "DeviceEnclosureColor": "9"},
		Domains:    map[string]map[string]any{},
		BatteryReg: map[string]any{"Serial": "FG99425SAAAAAAAAA"},
	}
	r := Analyze(raw, now, "en")
	fields := map[string]string{}
	for _, sec := range r.Sections {
		for _, f := range sec.Fields {
			fields[f.Label] = f.Value
		}
	}
	for label, want := range map[string]string{
		"Back color": "Blue (code 9)", "Front color": "Black (code 1)", "Processor": "A14 Hexa · 3.1GHz",
		"Screen": "6.1 inches · 2532 x 1170", "Released": "10/13/2020",
	} {
		if fields[label] != want {
			t.Errorf("%s = %q, want %q", label, fields[label], want)
		}
	}
	if r.Summary.ColorName != "Blue" || r.Battery.Maker != "SIMPLO" {
		t.Errorf("color %q maker %q", r.Summary.ColorName, r.Battery.Maker)
	}
	if !strings.Contains(r.Text(), "Battery maker: SIMPLO") {
		t.Error("the text report lacks the battery maker")
	}
	raw.BatteryReg["Serial"] = "B055422LAAAAAAAAA"
	if r := Analyze(raw, now, "en"); r.Battery.Maker != "" || strings.Contains(r.Text(), "Battery maker") {
		t.Errorf("an unknown maker: %q", r.Battery.Maker)
	}
}

// TestDetailsFields checks the device details have what a buyer compares
// with other tools, and the inspection's findings once it has run.
func TestDetailsFields(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	raw := sampleRaw()
	raw.Root["BasebandStatus"] = "BBInfoAvailable"
	raw.Root["BasebandSerialNumber"] = []byte{0xa6, 0x7d, 0xab, 0x99}
	raw.Root["SIMTrayStatus"] = "kCTSIMSupportSIMTrayInsertedSIM"
	fields := func(secs []Section) map[string]string {
		out := map[string]string{}
		for _, s := range secs {
			for _, f := range s.Fields {
				out[f.Label] = f.Value
			}
		}
		return out
	}
	want := map[string]string{
		"Unit type":              "Retail unit",
		"Calls":                  "Yes",
		"Baseband status":        "BBInfoAvailable",
		"Baseband serial number": "a67dab99",
		"SIM tray":               "InsertedSIM",
		"Jailbreak":              "None found",
	}
	got := fields(Describe(raw, now, "en").Sections)
	for label, v := range want {
		if got[label] != v {
			t.Errorf("%s = %q, want %q", label, got[label], v)
		}
	}
	if got["Sales region"] == "" || !strings.Contains(got["Sales region"], "CH/A") {
		t.Errorf("Sales region = %q", got["Sales region"])
	}
	// The report's details and the overview's, from the same data, agree.
	if r := fields(Analyze(raw, now, "en").Sections); len(r) != len(got) {
		t.Errorf("report has %d fields, overview %d", len(r), len(got))
	}

	// Read on connecting: no apps, no AFC2, so no jailbreak verdict yet.
	raw.Apps, raw.AFC2 = nil, nil
	if v, ok := fields(Describe(raw, now, "en").Sections)["Jailbreak"]; ok {
		t.Errorf("jailbreak shown before an inspection: %q", v)
	}
}

// TestBatteryDetails checks the readings the battery details show, and the
// rating, which follows the battery check's thresholds.
// Cycles are judged by the count Apple rates the model for: 500 before
// the iPhone 15, 1000 from it; none for a model not in the table.
func TestRatedCycles(t *testing.T) {
	for _, c := range []struct {
		product string
		cycles  uint64
		want    Status
	}{{"iPhone14,2", 500, Pass}, {"iPhone14,2", 501, Info}, {"iPhone16,1", 900, Pass}, {"iPhone16,1", 1001, Info}, {"iPhone99,1", 3000, Pass}} {
		raw := sampleRaw()
		raw.Root["ProductType"] = c.product
		raw.BatteryReg["CycleCount"] = c.cycles
		if got := find(t, Analyze(raw, now, "zh-Hans"), "充电循环"); got.Status != c.want {
			t.Errorf("%s, %d cycles: %+v, want %v", c.product, c.cycles, got, c.want)
		}
	}
}

func TestBatteryDetails(t *testing.T) {
	raw := sampleRaw()
	raw.BatteryReg["AppleRawCurrentCapacity"] = uint64(2350)
	raw.BatteryReg["BootVoltage"] = uint64(4240)
	raw.BatteryReg["AtWarnLevel"] = false
	raw.BatteryReg["AtCriticalLevel"] = true
	b := parseBattery(raw)
	if !b.HasCurrentMAh || b.CurrentMAh != 2350 || b.BootVoltageMV != 4240 {
		t.Errorf("current %d (%v), boot %d", b.CurrentMAh, b.HasCurrentMAh, b.BootVoltageMV)
	}
	if !b.HasWarnLevel || b.AtWarnLevel || !b.HasCriticalLevel || !b.AtCriticalLevel {
		t.Errorf("warn %v/%v, critical %v/%v", b.AtWarnLevel, b.HasWarnLevel, b.AtCriticalLevel, b.HasCriticalLevel)
	}
	for _, c := range []struct {
		health int64
		rating string
		st     Status
	}{{100, "Good", Pass}, {80, "Good", Pass}, {79, "Worn", Warn}, {0, "", Unknown}} {
		b := &Battery{SystemHealth: c.health}
		if r, st := b.Rating("en"); r != c.rating || st != c.st {
			t.Errorf("health %d: %q %v, want %q %v", c.health, r, st, c.rating, c.st)
		}
	}
}

// TestStorage reads the storage as an iPhone X (iOS 16.7) reports it, its
// controller node trimmed to the keys the details show.
func TestStorage(t *testing.T) {
	raw := sampleRaw()
	raw.Domains["com.apple.disk_usage"] = map[string]any{
		"TotalDiskCapacity": uint64(64e9), "AmountDataAvailable": uint64(52365996032), "TotalSystemCapacity": uint64(4943917056),
	}
	raw.Domains["com.apple.disk_usage.factory"] = map[string]any{"PhotoUsage": uint64(14e6), "CameraUsage": uint64(0)}
	raw.Apps = []services.App{
		{BundleID: "com.example.a", Type: "User", DiskUsage: 600e6},
		{BundleID: "com.example.b", Type: "User", DiskUsage: 400e6},
		{BundleID: "com.apple.mobilesafari", Type: "System", DiskUsage: 9e9},
	}
	raw.Parts = map[string]map[string]any{"storage": {
		"Model Number": "APPLE SSD AP0064L", "Serial Number": "0b28005122948655", "Firmware Revision": "1296.140",
		"IOMaximumSegmentByteCountRead": uint64(4096), "IOMaximumSwapWrite": uint64(1073741824),
		"Controller Characteristics": map[string]any{
			"vendor-name": "Samsung          ", "cell-type": uint64(3), "chip-id": "S4E",
			"nand-marketing-name": "tlc_3d_v4_2p_256                ", "msp-version": "13.174.0        ",
			"Encryption Type": "AES-XTS", "Preferred IO Size": uint64(1048576),
		},
	}}
	s := parseStorage(locales.In("en"), raw)
	if s == nil {
		t.Fatal("no storage")
	}
	if !s.HasApps || s.Apps != 1e9 || s.Photos != 14e6 || s.System != 4943917056 {
		t.Errorf("apps %d (%v), photos %d, system %d", s.Apps, s.HasApps, s.Photos, s.System)
	}
	if want := int64(64e9 - 52365996032 - 4943917056 - 1e9 - 14e6); s.Other() != want {
		t.Errorf("other %d, want %d", s.Other(), want)
	}
	got := map[string]string{}
	for _, f := range append(s.Info, s.Limits...) {
		got[f.Label] = f.Value
	}
	for k, v := range map[string]string{
		"Vendor": "Samsung", "Storage type": "TLC", "Storage model": "APPLE SSD AP0064L", "Controller chip": "S4E",
		"Firmware version": "1296.140", "NAND flash name": "tlc_3d_v4_2p_256", "MSP version": "13.174.0",
		"Encryption": "AES-XTS", "Max Read Bytes": "4096", "Max Write Swap": "1073741824", "Preferred IO Size": "1048576",
	} {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	// Before an inspection the apps are unknown and count as other.
	raw.Apps = nil
	if s := parseStorage(locales.In("en"), raw); s.HasApps || s.Other() != int64(64e9-52365996032-4943917056-14e6) {
		t.Errorf("before inspection: apps known %v, other %d", s.HasApps, s.Other())
	}
}

// TestICloud reads iCloud as an iPhone 12 (iOS 27.2) reports it with
// Contacts turned off in Settings, and as an iPhone X (iOS 16.7) does,
// which says nothing of it.
func TestICloud(t *testing.T) {
	icloud := []any{"iCloud"}
	raw := sampleRaw()
	raw.Domains[dataSyncDomain] = map[string]any{
		"Contacts":  map[string]any{"AccountNames": []any{}, "Sources": []any{}},
		"Calendars": map[string]any{"AccountNames": icloud, "Sources": icloud},
		"Bookmarks": map[string]any{"AccountNames": []any{"iCloud", "ada@example.com"}, "Sources": []any{"iCloud", "ada@example.com"}},
		"Notes":     map[string]any{"AccountNames": icloud, "Sources": icloud},
	}
	raw.Domains["com.apple.mobile.backup"] = map[string]any{
		"CloudBackupEnabled": true, "LastCloudBackupDate": uint64(815684883), "Version": "2.0",
	}
	raw.scrub()
	if got := FormatValue(raw.Domains[dataSyncDomain]["Bookmarks"]); strings.Contains(got, "example.com") {
		t.Errorf("an account name stayed: %s", got)
	}
	ic := parseICloud(locales.In("en"), raw)
	if ic == nil {
		t.Fatal("no iCloud")
	}
	want := map[string]bool{"Contacts": false, "Calendars": true, "Safari": true, "Notes": true}
	for _, c := range ic.Classes {
		if on, ok := want[c.Name]; !ok || !c.Known || c.On != on {
			t.Errorf("%s: on %v known %v", c.Name, c.On, c.Known)
		}
	}
	if !ic.HasBackup || !ic.Backup || !ic.LastBackup.Equal(time.Date(2026, 11, 6, 19, 8, 3, 0, time.UTC)) {
		t.Errorf("backup %v (%v), last %v", ic.Backup, ic.HasBackup, ic.LastBackup)
	}

	raw.Domains[dataSyncDomain] = map[string]any{}
	raw.Domains["com.apple.mobile.backup"] = map[string]any{"Version": "2.0"}
	if ic := parseICloud(locales.In("en"), raw); ic != nil {
		t.Errorf("iCloud from an empty domain: %+v", ic)
	}
}
