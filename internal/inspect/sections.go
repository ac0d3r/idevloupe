package inspect

import (
	"cmp"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/pkg/plistx"
)

// buildSections lays out the device information, with the battery b as
// read: what the device says of itself and, after an inspection, the
// parts' serial numbers and whether it is jailbroken.
func buildSections(a *analysis, b *Battery) []Section {
	m, lang, v, s := a.m, a.lang, a.v, a.r.Summary
	var secs []Section
	front, back := v.colors()
	// What the device table says of the model, when there is one.
	model, _ := Table.Model(s.ProductType)
	processor := joinWith(func(a, b string) string { return a + " · " + b }, model.Chip.In(lang), model.Frequency)
	screen := joinWith(func(a, b string) string { return a + " · " + b }, model.ScreenSize.In(lang), model.Resolution)
	sec := func(title string, fields ...Field) {
		var keep []Field
		for _, f := range fields {
			if strings.TrimSpace(f.Value) != "" {
				keep = append(keep, f)
			}
		}
		if len(keep) > 0 {
			secs = append(secs, Section{Title: title, Fields: keep})
		}
	}
	ecid := ""
	if n, ok := v.num("UniqueChipID"); ok {
		ecid = fmt.Sprintf("%X", uint64(n))
	}
	chip := ""
	if n, ok := v.num("ChipID"); ok {
		chip = fmt.Sprintf("0x%04X", n)
		if p := v.str("HardwarePlatform"); p != "" {
			chip += " · " + p
		}
	}
	diskTotal, diskFree := "", ""
	if n, ok := v.domainNum("com.apple.disk_usage", "TotalDiskCapacity"); ok {
		diskTotal = m.ValueGB(float64(n) / 1e9)
	}
	if n, ok := v.domainNum("com.apple.disk_usage", "AmountDataAvailable"); ok {
		diskFree = m.ValueGB(float64(n) / 1e9)
	}
	unitType := ""
	if a.modelNumber != "" {
		unitType = UnitKindOf(a.modelNumber, lang).Name
	}
	region := s.Region
	if region != a.regionInfo {
		region = m.ValueWithCode(region, a.regionInfo)
	}
	// The jailbreak check, when an inspection made it.
	jailbreak := ""
	for _, c := range a.r.Checks {
		if c.Title == m.CheckJailbreak() {
			jailbreak = c.Value
		}
	}
	telephony, telephonyOK := v.boolean("TelephonyCapability")
	// The baseband's serial is raw bytes, shown as hex as Apple's tools do.
	basebandSerial := ""
	if x, ok := v.root("BasebandSerialNumber"); ok {
		if b, ok := x.([]byte); ok {
			basebandSerial = fmt.Sprintf("%x", b)
		} else {
			basebandSerial = FormatValue(x)
		}
	}
	pw, pwOK := v.boolean("PasswordProtected")
	enc, encOK := v.domainBool("com.apple.mobile.backup", "WillEncrypt")
	storage := s.Capacity
	if diskTotal != "" {
		storage = strings.TrimSpace(m.WithNote(s.Capacity, diskTotal))
	}
	sec(m.SectionHardware(),
		Field{m.FieldDeviceName(), s.DeviceName},
		Field{m.FieldModel(), s.Marketing},
		Field{m.FieldModelIdentifier(), s.ProductType},
		Field{m.FieldBoardModel(), v.str("HardwareModel")},
		Field{m.FieldChip(), chip},
		Field{m.FieldCPUArchitecture(), v.str("CPUArchitecture")},
		Field{m.FieldProcessor(), processor},
		Field{m.FieldCPUCores(), model.Cores},
		Field{m.FieldScreen(), screen},
		Field{m.FieldReleaseDate(), model.ReleaseDate.In(lang)},
		Field{m.FieldColor(), colorText(m, lang, s.ProductType, front, true)},
		Field{m.FieldEnclosureColor(), colorText(m, lang, s.ProductType, back, false)},
		Field{m.FieldRegulatoryModel(), cmp.Or(v.platformStr("regulatory-model-number"), v.sysText("RMd#"))},
		Field{m.CheckStorage(), storage},
		Field{m.FieldAvailable(), diskFree},
	)
	sec(m.CategoryIdentity(),
		Field{m.CheckSerial(), s.Serial},
		Field{m.FieldPartNumber(), s.ModelNumber},
		Field{m.CheckUnitType(), unitType},
		Field{m.CheckRegion(), region},
		Field{m.FieldManufactured(), a.made()},
		Field{"UDID", v.str("UniqueDeviceID")},
		Field{"ECID", ecid},
		Field{"IMEI", s.IMEI},
		Field{"IMEI 2", v.str("InternationalMobileEquipmentIdentity2")},
		Field{"MEID", v.str("MobileEquipmentIdentifier")},
		Field{m.FieldBoardSerial(), v.str("MLBSerialNumber")},
		Field{m.FieldWiFiAddress(), v.str("WiFiAddress")},
		Field{m.FieldBluetoothAddress(), v.str("BluetoothAddress")},
		Field{m.FieldCellularAddress(), v.str("EthernetAddress")},
	)
	sec(m.SectionSystem(),
		Field{m.FieldIOSVersion(), s.IOS},
		Field{m.FieldBasebandVersion(), v.str("BasebandVersion")},
		Field{m.FieldIBootVersion(), v.str("FirmwareVersion")},
		Field{m.CheckActivation(), v.str("ActivationState")},
		Field{m.CheckJailbreak(), jailbreak},
		Field{m.FieldPasscode(), yesNo(m, pw, pwOK)},
		Field{m.FieldEncryptedBackups(), yesNo(m, enc, encOK)},
		Field{m.FieldLanguage(), v.domainStr("com.apple.international", "Language")},
		Field{m.FieldLocale(), v.domainStr("com.apple.international", "Locale")},
		Field{m.FieldTimeZone(), v.str("TimeZone")},
	)
	sec(m.CategoryCellular(),
		Field{m.FieldCallCapability(), yesNo(m, telephony, telephonyOK)},
		Field{m.FieldBasebandStatus(), v.str("BasebandStatus")},
		Field{m.FieldBasebandSerial(), basebandSerial},
		Field{m.FieldSIMStatus(), strings.TrimPrefix(v.str("SIMStatus"), "kCTSIMSupportSIMStatus")},
		Field{m.FieldSIMTray(), strings.TrimPrefix(v.str("SIMTrayStatus"), "kCTSIMSupportSIMTray")},
	)
	// On connecting only the storage controller is read, which alone would
	// make a parts section the inspection's checks do not stand behind.
	if !a.raw.inspected() {
		return secs
	}
	if sec, ok := partsSection(m, a.raw, b, a.productType); ok {
		secs = append(secs, sec)
	}
	return secs
}

func yesNo(m locales.Messages, b bool, ok bool) string {
	if !ok {
		return ""
	}
	if b {
		return m.ValueYes()
	}
	return m.ValueNo()
}

// buildRaw groups every value read by its source, for the raw data tab.
func buildRaw(lang string, raw *RawData) []RawGroup {
	m := locales.In(lang)
	var out []RawGroup
	group := func(name string, m map[string]any) {
		if len(m) == 0 {
			return
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		g := RawGroup{Source: name}
		for _, k := range keys {
			g.Items = append(g.Items, Field{k, FormatValue(m[k])})
		}
		out = append(out, g)
	}
	group("lockdown", raw.Root)
	ds := make([]string, 0, len(raw.Domains))
	for d := range raw.Domains {
		ds = append(ds, d)
	}
	sort.Strings(ds)
	for _, d := range ds {
		group(d, raw.Domains[d])
	}
	group("IORegistry · "+m.RawBattery(), raw.BatteryReg)
	group("IORegistry · "+m.RawPlatform(), raw.PlatformReg)
	group("MobileGestalt", raw.Gestalt)
	if len(raw.SysCfg) > 0 {
		vals := make(map[string]any, len(raw.SysCfg))
		for k, v := range raw.SysCfg {
			vals[k] = v
		}
		group("SysCfg", vals)
	}
	group("diagnostics · All", raw.DiagAll)
	partKeys := make([]string, 0, len(raw.Parts))
	for k := range raw.Parts {
		partKeys = append(partKeys, k)
	}
	sort.Strings(partKeys)
	for _, k := range partKeys {
		group("IORegistry · "+m.RawPart(k), raw.Parts[k])
	}
	if a := raw.Analytics; a != nil {
		vals := map[string]any{m.RawFile(): a.File, m.RawDate(): a.Date.Format(time.RFC3339)}
		for k, v := range a.Values {
			vals["last_value_"+k] = v
		}
		group(m.RawAnalyticsBattery(), vals)
	}
	if sc := raw.AnalyticsScan; sc != nil {
		vals := map[string]any{}
		if sc.MoveErr != "" {
			vals["(crashreportmover)"] = sc.MoveErr
		}
		for dir, names := range sc.Listing {
			vals[m.RawDirectory(dir)] = m.RawDirectoryEntries(sc.Total[dir], strings.Join(names, ", "))
		}
		if len(sc.Read) > 0 {
			vals[m.RawRead()] = strings.Join(sc.Read, ", ")
		}
		if raw.AnalyticsErr != nil {
			vals[m.RawResult()] = ErrorText(lang, raw.AnalyticsErr)
		}
		group(m.RawAnalyticsSearch(), vals)
	}
	if raw.MC != nil {
		group(m.RawMCCloud(), raw.MC.Cloud)
		group(m.RawMCProfiles(), raw.MC.RawProfiles)
	}
	return out
}

// FormatValue renders a plist value on one line.
func FormatValue(x any) string {
	switch t := x.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case []byte:
		if s, ok := plistx.String(t); ok && s != "" {
			return s
		}
		const max = 24
		if len(t) > max {
			return fmt.Sprintf("%x… (%d B)", t[:max], len(t))
		}
		return fmt.Sprintf("%x", t)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, k+": "+FormatValue(t[k]))
		}
		return truncate("{"+strings.Join(parts, ", ")+"}", 4000)
	case []any:
		var parts []string
		for _, e := range t {
			parts = append(parts, FormatValue(e))
		}
		return truncate("["+strings.Join(parts, ", ")+"]", 4000)
	case time.Time:
		return t.Format(time.RFC3339)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'g', -1, 32)
	}
	if n, ok := plistx.Int(x); ok {
		return fmt.Sprint(n)
	}
	return fmt.Sprint(x)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
