package inspect

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"idevloupe/internal/locales"
	"idevloupe/internal/lockdown"
	"idevloupe/internal/plistx"
	"idevloupe/internal/services"
	"idevloupe/internal/usbmux"
)

// view gives typed access to the raw data.
type view struct {
	raw *RawData
}

func (v view) root(key string) (any, bool) {
	x, ok := v.raw.Root[key]
	return x, ok
}

func (v view) str(key string) string {
	x, _ := v.root(key)
	s, _ := plistx.String(x)
	return strings.TrimSpace(s)
}

func (v view) boolean(key string) (bool, bool) {
	x, ok := v.root(key)
	if !ok {
		return false, false
	}
	return plistx.Bool(x)
}

func (v view) num(key string) (int64, bool) {
	x, ok := v.root(key)
	if !ok {
		return 0, false
	}
	return plistx.Int(x)
}

// nvram returns the NonVolatileRAM dictionary of the root domain.
func (v view) nvram() map[string]any {
	x, ok := v.root("NonVolatileRAM")
	if !ok {
		return nil
	}
	m, _ := plistx.Dict(x)
	return m
}

func (v view) domain(d, key string) (any, bool) {
	m, ok := v.raw.Domains[d]
	if !ok {
		return nil, false
	}
	x, ok := m[key]
	return x, ok
}

func (v view) domainBool(d, key string) (bool, bool) {
	x, ok := v.domain(d, key)
	if !ok {
		return false, false
	}
	return plistx.Bool(x)
}

func (v view) domainNum(d, key string) (int64, bool) {
	x, ok := v.domain(d, key)
	if !ok {
		return 0, false
	}
	return plistx.Int(x)
}

func (v view) domainStr(d, key string) string {
	x, _ := v.domain(d, key)
	s, _ := plistx.String(x)
	return s
}

func (v view) gestaltStr(key string) string {
	if v.raw.Gestalt == nil {
		return ""
	}
	s, _ := plistx.String(v.raw.Gestalt[key])
	return strings.TrimSpace(s)
}

func (v view) platformStr(keys ...string) string {
	if v.raw.PlatformReg == nil {
		return ""
	}
	for _, k := range keys {
		if s, ok := plistx.String(v.raw.PlatformReg[k]); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

var betaBuild = regexp.MustCompile(`^\d+[A-Z]\d+[a-z]$`)

// Analyze turns raw data into a report in the language lang, a tag of
// locales.Catalog's languages. It is pure, so tests feed it recorded data,
// and the window makes the report again when the language changes.
func Analyze(raw *RawData, now time.Time, lang string) *Report {
	v := view{raw}
	m := locales.In(lang)
	r := &Report{Lang: lang, Time: now, UDID: raw.UDID}
	for _, e := range raw.Errors {
		r.Errors = append(r.Errors, ErrorText(lang, e))
	}
	add := func(cat, title, value string, st Status, detail string) {
		r.Checks = append(r.Checks, Check{Category: cat, Title: title, Value: value, Status: st, Detail: detail})
	}

	productType := v.str("ProductType")
	model, known := LookupModel(productType, lang)
	marketing := model.Name
	if !known {
		marketing = v.gestaltStr("ArtworkDeviceProductDescription")
	}
	if marketing == "" {
		marketing = productType
	}
	modelNumber := v.str("ModelNumber")
	regionInfo := v.str("RegionInfo")
	serial := v.str("SerialNumber")
	imei := v.str("InternationalMobileEquipmentIdentity")
	imei2 := v.str("InternationalMobileEquipmentIdentity2")
	telephony, hasTelephonyKey := v.boolean("TelephonyCapability")
	if !hasTelephonyKey {
		telephony = imei != "" || v.str("DeviceClass") == "iPhone"
	}

	// ---- Summary
	s := Summary{
		DeviceName:  v.str("DeviceName"),
		Marketing:   marketing,
		ProductType: productType,
		IOS:         strings.TrimSpace(v.str("ProductVersion") + " (" + v.str("BuildVersion") + ")"),
		ModelNumber: modelNumber + regionInfo,
		Serial:      serial,
		IMEI:        imei,
	}
	if s.IOS == "()" {
		s.IOS = ""
	}
	if n, ok := v.domainNum("com.apple.disk_usage", "TotalDiskCapacity"); ok && n > 0 {
		s.Capacity = NominalCapacity(n)
	}
	s.Color = v.str("DeviceEnclosureColor")
	if s.Color == "" {
		s.Color = v.str("DeviceColor")
	}
	if strings.HasPrefix(s.Color, "#") && len(s.Color) == 7 {
		s.ColorHex = s.Color
	}
	if name, ok := RegionName(regionInfo, lang); ok {
		s.Region = name
	} else {
		s.Region = regionInfo
	}
	r.Summary = s

	// ---- Basics
	basic := m.CategoryBasic()
	switch {
	case productType == "":
		add(basic, m.CheckModel(), "", Unknown, m.KeyNotRead("ProductType"))
	case known:
		add(basic, m.CheckModel(), m.ValueWithCode(marketing, productType), Pass, "")
	default:
		add(basic, m.CheckModel(), m.ValueWithCode(marketing, productType), Info, m.ModelNotInTable())
	}

	kind := UnitKindOf(modelNumber, lang)
	kv := kind.Name
	if modelNumber != "" {
		kv = kind.Name + " · " + modelNumber + regionInfo
	}
	add(basic, m.CheckUnitType(), kv, kind.Status, kind.Detail)

	if regionInfo == "" {
		add(basic, m.CheckRegion(), "", Unknown, m.KeyNotRead("RegionInfo"))
	} else if name, ok := RegionName(regionInfo, lang); ok {
		add(basic, m.CheckRegion(), m.ValueWithCode(name, regionInfo), Pass, "")
	} else {
		add(basic, m.CheckRegion(), regionInfo, Info, m.RegionNotInTable())
	}

	// software_behavior's ChinaBrick is set on units made for mainland
	// China (WLAN wording, dual physical SIM), independently of RegionInfo.
	if cb, ok := v.domainBool("com.apple.mobile.software_behavior", "ChinaBrick"); ok && regionInfo != "" {
		isCH := strings.HasPrefix(strings.ToUpper(regionInfo), "CH/")
		if cb == isCH {
			add(basic, m.CheckRegionConsistency(), m.ValueConsistent(), Pass, m.RegionChinaBrick(regionInfo, strconv.FormatBool(cb)))
		} else {
			add(basic, m.CheckRegionConsistency(), m.ValueInconsistent(), Warn, m.RegionChinaBrickMismatch(regionInfo, strconv.FormatBool(cb)))
		}
	}
	if b, ok := v.boolean("ProductionSOC"); ok {
		if b {
			add(basic, m.CheckProductionChip(), m.ProductionChipYes(), Pass, "ProductionSOC = true")
		} else {
			add(basic, m.CheckProductionChip(), m.ProductionChipNo(), Fail, m.ProductionChipNoDetail())
		}
	} else {
		add(basic, m.CheckProductionChip(), "", Unknown, m.KeyNotRead("ProductionSOC"))
	}
	if b, ok := v.domainBool("com.apple.mobile.internal", "IsInternal"); ok {
		if b {
			add(basic, m.CheckInternal(), m.InternalApple(), Fail, "com.apple.mobile.internal IsInternal = true")
		} else {
			add(basic, m.CheckInternal(), m.ValueNo(), Pass, "")
		}
	}

	ver, build := v.str("ProductVersion"), v.str("BuildVersion")
	switch {
	case ver == "":
		add(basic, m.CheckSystemVersion(), "", Unknown, "")
	case betaBuild.MatchString(build):
		add(basic, m.CheckSystemVersion(), fmt.Sprintf("iOS %s (%s)", ver, build), Info, m.BetaBuildDetail())
	default:
		add(basic, m.CheckSystemVersion(), fmt.Sprintf("iOS %s (%s)", ver, build), Pass, "")
	}
	// Retail builds carry no ReleaseType; internal and seed builds do.
	switch rt := v.str("ReleaseType"); rt {
	case "":
	case "Internal", "NonUI":
		add(basic, m.CheckReleaseType(), rt, Fail, m.ReleaseInternalDetail(rt))
	case "Beta":
		add(basic, m.CheckReleaseType(), rt, Info, m.ReleaseBetaDetail())
	default:
		add(basic, m.CheckReleaseType(), rt, Warn, m.ReleaseOtherDetail(rt))
	}

	if n, ok := v.domainNum("com.apple.disk_usage", "TotalDiskCapacity"); ok && n > 0 {
		add(basic, m.CheckStorage(), m.StorageValue(NominalCapacity(n), float64(n)/1e9), Pass, "")
	} else {
		add(basic, m.CheckStorage(), "", Unknown, m.KeyNotRead("TotalDiskCapacity"))
	}

	// ---- Serial number and identifiers
	ident := m.CategoryIdentity()
	releaseYear := 0
	if known {
		releaseYear = model.Year
	}
	var mfg *SerialDate
	switch {
	case serial == "":
		add(ident, m.CheckSerial(), "", Unknown, m.KeyNotRead("SerialNumber"))
	case len(serial) == 12:
		if d, ok := DecodeSerial(serial, releaseYear, now); ok {
			mfg = &d
			add(ident, m.CheckSerial(), serial, Pass, m.SerialLegacy12(d.Text(lang)))
		} else {
			add(ident, m.CheckSerial(), serial, Warn, m.SerialLegacy12Bad())
		}
	case len(serial) == 10:
		add(ident, m.CheckSerial(), serial, Pass, m.SerialRandom())
	case len(serial) == 11:
		add(ident, m.CheckSerial(), serial, Pass, m.SerialLegacy11())
	default:
		add(ident, m.CheckSerial(), serial, Warn, m.SerialOddLength(len(serial)))
	}

	// Cross-check values iOS reports through independent paths. Lockdown
	// and MobileGestalt both read the factory syscfg, the device tree holds
	// what iBoot passed to the kernel.
	type src struct{ name, val string }
	consistency := func(title string, sources []src) {
		var have []src
		for _, s := range sources {
			if s.val != "" {
				have = append(have, s)
			}
		}
		if len(have) < 2 {
			return
		}
		same := true
		for _, s := range have[1:] {
			if !strings.EqualFold(s.val, have[0].val) {
				same = false
			}
		}
		var parts []string
		for _, s := range have {
			parts = append(parts, s.name+"="+s.val)
		}
		values := joinWith(m.JoinList, parts...)
		if same {
			add(ident, title, m.ValueConsistent(), Pass, values)
		} else {
			add(ident, title, m.ValueInconsistent(), Fail, m.JoinSentences(values, m.SourcesDisagree()))
		}
	}
	deviceTree := m.SourceDeviceTree()
	consistency(m.CheckSerialConsistency(), []src{
		{"lockdown", serial},
		{deviceTree, v.platformStr("IOPlatformSerialNumber", "serial-number")},
		{"MobileGestalt", v.gestaltStr("SerialNumber")},
	})
	// A part number is model and region; a source missing either has no
	// complete value to compare.
	partNumber := func(model, region string) string {
		if model == "" || region == "" {
			return ""
		}
		return model + region
	}
	consistency(m.CheckModelConsistency(), []src{
		{"lockdown", partNumber(modelNumber, regionInfo)},
		{deviceTree, partNumber(v.platformStr("model-number"), v.platformStr("region-info"))},
		{"MobileGestalt", partNumber(v.gestaltStr("ModelNumber"), v.gestaltStr("RegionInfo"))},
	})
	if mlb := v.str("MLBSerialNumber"); mlb != "" {
		consistency(m.CheckBoardSerialConsistency(), []src{
			{"lockdown", mlb},
			{deviceTree, v.platformStr("mlb-serial-number")},
		})
	}

	if telephony {
		checkIMEI := func(title, val string) {
			switch {
			case val == "":
				add(ident, title, "", Warn, m.ImeiMissing())
			case len(val) != 15:
				add(ident, title, val, Fail, m.ImeiLength(len(val)))
			case !LuhnValid(val):
				add(ident, title, val, Fail, m.ImeiLuhnFailed())
			default:
				add(ident, title, val, Pass, m.ImeiLuhnPassed())
			}
		}
		checkIMEI("IMEI", imei)
		if imei2 != "" {
			checkIMEI("IMEI 2", imei2)
		}
	}

	// ---- Security and locks
	sec := m.CategorySecurity()
	switch st := v.str("ActivationState"); st {
	case "":
		add(sec, m.CheckActivation(), "", Unknown, "")
	case "Activated":
		add(sec, m.CheckActivation(), m.ActivationActivated(), Pass, "")
	case "Unactivated":
		add(sec, m.CheckActivation(), m.ActivationUnactivated(), Info, m.ActivationUnactivatedDetail())
	case "FactoryActivated":
		add(sec, m.CheckActivation(), m.ActivationFactory(), Warn, m.ActivationFactoryDetail())
	default:
		add(sec, m.CheckActivation(), st, Warn, m.ActivationOtherDetail())
	}

	fmOn, fmOK := v.domainBool("com.apple.fmip", "IsAssociated")
	fmDetail := ""
	if !fmOK {
		// The Find My daemon mirrors its state into NVRAM, which lockdown
		// returns with the root values.
		if nv := v.nvram(); nv != nil {
			if st, ok := plistx.String(nv["fm-activation-locked"]); ok && st != "" {
				fmOn, fmOK = strings.EqualFold(st, "YES"), true
				fmDetail = "NVRAM fm-activation-locked = " + st
				if acct, ok := plistx.String(nv["fm-account-masked"]); ok && acct != "" {
					fmDetail = m.JoinList(fmDetail, m.FindMyAccount(acct))
				}
			}
		}
	}
	if fmOK {
		if fmOn {
			add(sec, m.CheckFindMy(), m.FindMyOn(), Warn, joinWith(m.JoinSentences, fmDetail, m.FindMyOnDetail()))
		} else {
			add(sec, m.CheckFindMy(), m.ValueOff(), Pass, fmDetail)
		}
	} else {
		add(sec, m.CheckFindMy(), "", Unknown, m.FindMyUnknown())
	}

	supervised, hasSup := v.domainBool("com.apple.mobile.chaperone", "DeviceIsChaperoned")
	if raw.MC != nil && raw.MC.Cloud != nil {
		if b, ok := raw.MC.Cloud["IsSupervised"].(bool); ok && (b || !hasSup) {
			supervised, hasSup = b, true
		}
	}
	if hasSup {
		if supervised {
			add(sec, m.CheckSupervision(), m.Supervised(), Warn, m.SupervisedDetail())
		} else {
			add(sec, m.CheckSupervision(), m.NotSupervised(), Pass, "")
		}
	} else {
		add(sec, m.CheckSupervision(), "", Unknown, "")
	}

	if raw.MC == nil {
		add(sec, m.CheckMDM(), "", Unknown, m.ProfilesUnread())
	} else {
		cloud := raw.MC.Cloud
		org := ""
		dep := false
		if cloud != nil {
			org, _ = cloud["OrganizationName"].(string)
			if u, _ := cloud["ConfigurationURL"].(string); u != "" {
				dep = true
			}
			if b, _ := cloud["IsMDMUnremovable"].(bool); b {
				dep = true
			}
			if b, _ := cloud["IsMandatory"].(bool); b {
				dep = true
			}
			if org != "" {
				dep = true
			}
		}
		var mdm []ProfileEntry
		for _, p := range raw.MC.Profiles {
			pe := ProfileEntry{Identifier: p.Identifier, Name: p.DisplayName, Organization: p.Organization, MDM: p.IsMDM}
			r.Profiles = append(r.Profiles, pe)
			// MDM enrollment profiles are installed so that the user
			// cannot remove them; IsMDMProfile is not always reported.
			if p.IsMDM || p.RemovalDisallowed {
				mdm = append(mdm, pe)
			}
		}
		switch {
		case dep:
			val := m.MdmEnrolled()
			if org != "" {
				val += " · " + org
			}
			add(sec, m.CheckMDM(), val, Fail, m.MdmEnrolledDetail())
		case len(mdm) > 0:
			name := mdm[0].Name
			if mdm[0].Organization != "" {
				name += " · " + mdm[0].Organization
			}
			add(sec, m.CheckMDM(), m.MdmProfile(name), Warn, m.MdmProfileDetail())
		default:
			add(sec, m.CheckMDM(), m.ValueNotFound(), Pass, "")
		}
		if len(r.Profiles) > 0 {
			var names []string
			for _, p := range r.Profiles {
				n := p.Name
				if n == "" {
					n = p.Identifier
				}
				names = append(names, n)
			}
			add(sec, m.CheckProfiles(), m.ProfileCount(len(r.Profiles)), Info, joinWith(m.JoinEnumeration, names...))
		} else {
			add(sec, m.CheckProfiles(), m.ValueNone(), Pass, "")
		}
	}

	// Jailbreak
	r.AppCount = len(raw.Apps)
	var jb []string
	for _, a := range raw.Apps {
		if name, ok := jailbreakApps[a.BundleID]; ok {
			r.Apps = append(r.Apps, AppEntry{a.BundleID, a.Name, a.Version, a.Type, m.JailbreakApp(name)})
			jb = append(jb, name)
		}
	}
	afc2 := raw.AFC2 != nil && *raw.AFC2
	appsRead, afc2Read := raw.AppsErr == nil, raw.AFC2 != nil
	switch {
	case afc2 || len(jb) > 0:
		var ev []string
		if afc2 {
			ev = append(ev, m.JailbreakAFC2())
		}
		if len(jb) > 0 {
			ev = append(ev, m.JailbreakApps(joinWith(m.JoinEnumeration, jb...)))
		}
		add(sec, m.CheckJailbreak(), m.JailbreakFound(), Fail, joinWith(m.JoinClauses, ev...))
	case appsRead && afc2Read:
		add(sec, m.CheckJailbreak(), m.ValueNotFound(), Pass, m.JailbreakNone())
	case !appsRead && !afc2Read:
		add(sec, m.CheckJailbreak(), "", Unknown, m.JailbreakUnread())
	case !appsRead:
		add(sec, m.CheckJailbreak(), m.ValueNotFoundIncomplete(), Unknown, m.JailbreakNoApps())
	default:
		add(sec, m.CheckJailbreak(), m.ValueNotFoundIncomplete(), Unknown, m.JailbreakNoAFC2())
	}

	if b, ok := v.domainBool("com.apple.security.mac.amfi", "DeveloperModeStatus"); ok && b {
		add(sec, m.CheckDeveloperMode(), m.ValueOn(), Info, m.DeveloperModeDetail())
	}
	if b, ok := v.boolean("BrickState"); ok && b {
		add(sec, m.CheckBrickState(), "BrickState", Warn, m.BrickStateDetail())
	}
	if nv := v.nvram(); nv != nil {
		if args, ok := plistx.String(nv["boot-args"]); ok && strings.TrimSpace(args) != "" {
			add(sec, m.CheckBootArgs(), args, Warn, m.BootArgsDetail())
		}
	}

	// ---- Cellular
	if telephony {
		cell := m.CategoryCellular()
		bb := v.str("BasebandVersion")
		bbStatus := v.str("BasebandStatus")
		if bb != "" && bbStatus != "" && bbStatus != "BBInfoAvailable" {
			add(cell, m.CheckBaseband(), bb, Warn, m.BasebandNotReady(bbStatus))
		} else if bb != "" {
			add(cell, m.CheckBaseband(), bb, Pass, "")
		} else {
			add(cell, m.CheckBaseband(), m.ValueNotRead(), Fail, m.BasebandMissing())
		}
		switch st := v.str("SIMStatus"); st {
		case "":
		case "kCTSIMSupportSIMStatusReady":
			add(cell, m.CheckSIM(), m.SimReady(), Pass, "")
		case "kCTSIMSupportSIMStatusNotInserted":
			add(cell, m.CheckSIM(), m.SimNotInserted(), Info, "")
		case "kCTSIMSupportSIMStatusPINLocked":
			add(cell, m.CheckSIM(), m.SimPINLocked(), Info, "")
		default:
			add(cell, m.CheckSIM(), strings.TrimPrefix(st, "kCTSIMSupportSIMStatus"), Info, st)
		}
	}

	// ---- Battery
	r.Battery = parseBattery(raw)
	if b := r.Battery; b != nil {
		if a := raw.Analytics; a != nil {
			b.LogFile, b.LogDate = a.File, a.Date
			if n, ok := a.Values["MaximumCapacityPercent"]; ok && n > 0 && n <= 150 {
				b.LogHealth = n
			}
			b.LogCycles, b.HasLogCycles = a.Values["CycleCount"]
			if n, ok := a.Values["BatteryHealthMetric"]; ok && b.ResistanceMetric == 0 {
				b.ResistanceMetric = n
			}
			if !b.HasCycleCount && b.HasLogCycles {
				// The gauge was not read: the log's count stands in.
				b.CycleCount, b.HasCycleCount, b.CyclesFromLog = b.LogCycles, true, true
			}
		} else if raw.AnalyticsErr != nil {
			b.LogNote = ErrorText(lang, raw.AnalyticsErr)
		}
	}
	addBatteryChecks(m, r, add, mfg, now)

	// ---- Parts
	// Face ID's self test, as the Pearl driver reports it. Its codes are
	// not documented; samples: 0 on an iPhone 16 with working Face ID, 8 on
	// an iPhone X whose Face ID iOS flags in Parts and Service History.
	pearl := raw.Parts["pearl"]
	selfTest, hasSelfTest := plistx.Int(pearl["PearlSelfTestResult"])
	// FDRValidated (iOS 17 and later) tells whether the module matches the
	// factory pairing data (FDR). Seen true on working units; false is read
	// from its meaning, no sample has it yet.
	fdr, hasFDR := plistx.Bool(pearl["FDRValidated"])
	const pearlKey = "ApplePearlSEPDriver PearlSelfTestResult"
	parts := m.CategoryParts()
	switch {
	case hasFDR && !fdr:
		add(parts, m.CheckFaceID(), m.FaceIDFDRFailed(), Warn, m.FaceIDFDRFailedDetail())
	case hasSelfTest && selfTest == 0:
		detail := pearlKey + " = 0"
		if hasFDR {
			detail = m.JoinList(detail, m.FaceIDFDRPassed())
		}
		add(parts, m.CheckFaceID(), m.ValuePassed(), Pass, detail)
	case hasSelfTest:
		add(parts, m.CheckFaceID(), m.FaceIDFailed(int(selfTest)), Warn, m.FaceIDFailedDetail(pearlKey, int(selfTest)))
	case HasFaceID(productType):
		add(parts, m.CheckFaceID(), "", Unknown, m.FaceIDUnread())
	}

	addTouchIDCheck(m, raw, productType, add)

	// ---- Other
	if b, ok := v.domainBool("com.apple.PurpleBuddy", "SetupDone"); ok && !b {
		add(m.CategoryOther(), m.CheckSetup(), m.SetupUnfinished(), Info, m.SetupUnfinishedDetail())
	}

	r.Sections = buildSections(m, v, s)
	if sec, ok := partsSection(m, raw, r.Battery, productType); ok {
		r.Sections = append(r.Sections, sec)
	}
	r.Raw = buildRaw(lang, raw)
	r.Manual = manualChecks(m, productType)
	return r
}

func parseBattery(raw *RawData) *Battery {
	reg := raw.BatteryReg
	b := &Battery{}
	gauge, _ := plistx.Path(raw.DiagAll, "GasGauge")
	gg, _ := plistx.Dict(gauge)
	if reg == nil && gg != nil {
		// The "All" diagnostics' gas gauge, as ipsw reads it.
		b.Present = true
		b.DesignCapacity, b.HasDesign = plistx.Int(gg["DesignCapacity"])
		// iOS reports FullChargeCapacity here as a percent (100 on a real
		// iPhone 12 whose cell holds 3025 mAh): only mAh values are capacity.
		if n, ok := plistx.Int(gg["FullChargeCapacity"]); ok && n > 100 {
			b.MaxCapacity, b.HasMax = n, true
		}
		b.CycleCount, b.HasCycleCount = plistx.Int(gg["CycleCount"])
		if b.HasDesign && b.HasMax && b.DesignCapacity > 0 {
			b.HealthPercent = float64(b.MaxCapacity) * 100 / float64(b.DesignCapacity)
		}
		return b
	}
	if reg == nil {
		// The lockdown battery domain still gives the charge level.
		if n, ok := (view{raw}).domainNum("com.apple.mobile.battery", "BatteryCurrentCapacity"); ok {
			b.Present, b.CurrentPercent, b.HasCurrentLevel = true, n, true
			b.IsCharging, _ = (view{raw}).domainBool("com.apple.mobile.battery", "BatteryIsCharging")
			return b
		}
		return nil
	}
	b.Present = true
	data, _ := plistx.Dict(reg["BatteryData"])
	num := func(keys ...string) (int64, bool) {
		for _, k := range keys {
			if n, ok := plistx.Int(reg[k]); ok {
				return n, true
			}
			if data != nil {
				if n, ok := plistx.Int(data[k]); ok {
					return n, true
				}
			}
		}
		return 0, false
	}
	b.DesignCapacity, b.HasDesign = num("DesignCapacity")
	if n, ok := num("NominalChargeCapacity"); ok && n > 0 {
		b.MaxCapacity, b.HasMax = n, true
	} else if n, ok := num("AppleRawMaxCapacity"); ok && n > 0 {
		b.MaxCapacity, b.HasMax = n, true
	} else if n, ok := num("MaxCapacity"); ok && n > 100 {
		// Before iOS 10, MaxCapacity was in mAh; later it is a percent.
		b.MaxCapacity, b.HasMax = n, true
	}
	if b.HasDesign && b.HasMax && b.DesignCapacity > 0 {
		b.HealthPercent = float64(b.MaxCapacity) * 100 / float64(b.DesignCapacity)
	}
	if n, ok := num("MaximumCapacityPercent"); ok && n > 0 && n <= 150 {
		b.SystemHealth = n
	}
	b.CycleCount, b.HasCycleCount = num("CycleCount")
	b.ResistanceMetric, _ = num("BatteryHealthMetric")
	if n, ok := num("CurrentCapacity"); ok && n >= 0 && n <= 100 {
		b.CurrentPercent, b.HasCurrentLevel = n, true
	}
	if n, ok := num("Temperature"); ok {
		b.TemperatureC, b.HasTemperature = float64(n)/100, true
	}
	b.VoltageMV, _ = num("Voltage", "AppleRawBatteryVoltage")
	b.AmperageMA, _ = num("InstantAmperage", "Amperage")
	b.IsCharging, _ = plistx.Bool(reg["IsCharging"])
	b.ExternalPower, _ = plistx.Bool(reg["ExternalConnected"])
	b.Serial, _ = plistx.String(reg["Serial"])
	if b.Serial == "" && data != nil {
		b.Serial, _ = plistx.String(data["Serial"])
	}
	if gg != nil {
		if n, ok := plistx.Int(gg["CycleCount"]); ok {
			b.GaugeCycleCount, b.HasGaugeCycles = n, true
		}
	}
	return b
}

func addBatteryChecks(m locales.Messages, r *Report, add func(cat, title, value string, st Status, detail string), mfg *SerialDate, now time.Time) {
	b := r.Battery
	cat, title := m.CategoryBattery(), m.CheckBatteryHealth()
	if b == nil {
		add(cat, title, "", Unknown, m.BatteryUnread())
		return
	}
	health := b.HealthPercent
	source := m.HealthFromCapacity(int(b.MaxCapacity), int(b.DesignCapacity))
	calc := ""
	if b.HealthPercent > 0 {
		calc = m.HealthComputed(b.HealthPercent, int(b.MaxCapacity), int(b.DesignCapacity))
	}
	switch {
	case b.SystemHealth > 0:
		health = float64(b.SystemHealth)
		source = joinWith(m.JoinClauses, m.HealthFromGauge(), calc)
	case b.LogHealth > 0:
		health = float64(b.LogHealth)
		source = joinWith(m.JoinClauses, m.HealthFromLog(b.LogDate.Format("2006-01-02")), calc)
		if age := now.Sub(b.LogDate); age > 14*24*time.Hour {
			source = m.JoinSentences(source, m.HealthLogOld(int(age.Hours()/24)))
		}
	case b.LogNote != "":
		source = m.WithNote(source, b.LogNote)
	}
	// A cell charging well above its design capacity is suspicious whatever
	// the health shown. Only a health computed from that capacity is itself
	// meaningless then; one iOS reports stands.
	overCap := b.HealthPercent > overCapacityPercent
	computed := b.SystemHealth == 0 && b.LogHealth == 0
	pct := int(math.Round(health))
	switch {
	case health <= 0:
		add(cat, title, "", Unknown, m.CapacityNotRead())
	case overCap && computed:
		add(cat, title, m.HealthAboveDesign(pct), Warn, m.JoinSentences(source, m.HealthAboveDesignDetail()))
	case health >= 85:
		add(cat, title, m.ValuePercent(pct), Pass, source)
	case health >= 80:
		add(cat, title, m.ValuePercent(pct), Info, m.JoinSentences(source, m.HealthNear80()))
	default:
		add(cat, title, m.ValuePercent(pct), Warn, m.JoinSentences(source, m.HealthBelow80()))
	}
	if b.HasCycleCount && b.HasGaugeCycles && abs64(b.CycleCount-b.GaugeCycleCount) > 2 {
		add(cat, m.CheckBatteryConsistency(), m.ValueInconsistent(), Warn, m.BatteryCyclesDisagree(int(b.CycleCount), int(b.GaugeCycleCount)))
	}
	if b.HasCycleCount {
		from := ""
		if b.CyclesFromLog {
			from = m.CyclesFromLog(b.LogDate.Format("2006-01-02"))
		}
		val := m.ValueCycles(int(b.CycleCount))
		switch {
		case b.CycleCount < 500:
			add(cat, m.CheckCycles(), val, Pass, from)
		case b.CycleCount < 1000:
			add(cat, m.CheckCycles(), val, Info, joinWith(m.JoinSentences, m.CyclesMany(), from))
		default:
			add(cat, m.CheckCycles(), val, Warn, joinWith(m.JoinSentences, m.CyclesVeryMany(), from))
		}
	} else {
		add(cat, m.CheckCycles(), "", Unknown, "")
	}
	addBatterySource(m, add, b, overCap, mfg, now)
	if b.HasTemperature {
		st, d := Pass, ""
		if b.TemperatureC >= 45 {
			st, d = Warn, m.BatteryWarm()
		}
		add(cat, m.CheckBatteryTemperature(), m.ValueCelsius(b.TemperatureC), st, d)
	}
}

// manualChecks lists what software cannot verify, naming the model's own
// biometric sensor.
func manualChecks(m locales.Messages, productType string) []string {
	bio := m.BioFaceOrTouch()
	switch {
	case HasFaceID(productType):
		bio = m.BioFace()
	case HasTouchID(productType):
		bio = m.BioTouch()
	}
	return []string{
		m.ManualPartsHistory(bio),
		m.ManualBatteryHealth(),
		m.ManualTrueTone(),
		m.ManualCoverage(),
		m.ManualCarrierLock(),
		m.ManualFunctions(bio),
		m.ManualExterior(),
	}
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

func buildSections(m locales.Messages, v view, s Summary) []Section {
	var secs []Section
	sec := func(title string, fields ...Field) {
		var keep []Field
		for _, f := range fields {
			if strings.TrimSpace(f.Value) != "" {
				keep = append(keep, f)
			}
		}
		if len(keep) > 0 {
			secs = append(secs, Section{title, keep})
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
		Field{m.FieldColor(), v.str("DeviceColor")},
		Field{m.FieldEnclosureColor(), v.str("DeviceEnclosureColor")},
		Field{m.CheckStorage(), storage},
		Field{m.FieldAvailable(), diskFree},
	)
	sec(m.CategoryIdentity(),
		Field{m.CheckSerial(), s.Serial},
		Field{m.FieldPartNumber(), s.ModelNumber},
		Field{"UDID", v.str("UniqueDeviceID")},
		Field{"ECID", ecid},
		Field{"IMEI", s.IMEI},
		Field{"IMEI 2", v.str("InternationalMobileEquipmentIdentity2")},
		Field{"MEID", v.str("MobileEquipmentIdentifier")},
		Field{m.FieldBoardSerial(), v.str("MLBSerialNumber")},
		Field{m.FieldWiFiAddress(), v.str("WiFiAddress")},
		Field{m.FieldBluetoothAddress(), v.str("BluetoothAddress")},
	)
	sec(m.SectionSystem(),
		Field{m.FieldIOSVersion(), s.IOS},
		Field{m.FieldBasebandVersion(), v.str("BasebandVersion")},
		Field{m.FieldIBootVersion(), v.str("FirmwareVersion")},
		Field{m.CheckActivation(), v.str("ActivationState")},
		Field{m.FieldPasscode(), yesNo(m, pw, pwOK)},
		Field{m.FieldEncryptedBackups(), yesNo(m, enc, encOK)},
		Field{m.FieldLanguage(), v.domainStr("com.apple.international", "Language")},
		Field{m.FieldLocale(), v.domainStr("com.apple.international", "Locale")},
		Field{m.FieldTimeZone(), v.str("TimeZone")},
	)
	sec(m.CategoryCellular(),
		Field{m.FieldSIMStatus(), strings.TrimPrefix(v.str("SIMStatus"), "kCTSIMSupportSIMStatus")},
	)
	return secs
}

// addTouchIDCheck judges Touch ID from what its drivers report. Samples:
// an iPhone 8 (iOS 16) and an iPhone SE 2 (iOS 27), both working, report
// a serial number from the sensor and its calibration from the factory
// pairing data (MesaCalBlobSource = FDR); only the iPhone 8's device tree
// node has MesaSelfTest, "OK". Without a self test, a sensor reporting the
// same passes.
func addTouchIDCheck(m locales.Messages, raw *RawData, productType string, add func(cat, title, value string, st Status, detail string)) {
	mesa, driver, sensor := raw.Parts["mesa"], raw.Parts["mesadriver"], raw.Parts["touchid"]
	selfTest, hasSelfTest := plistx.String(mesa["MesaSelfTest"])
	calSource, _ := plistx.String(driver["MesaCalBlobSource"])
	cal := ""
	if calSource == "FDR" {
		cal = m.TouchIDCalibrationFDR()
	} else if calSource != "" {
		cal = "MesaCalBlobSource = " + calSource
	}
	cat, title := m.CategoryParts(), m.CheckTouchID()
	// mesa-state is the sensor's power state (its IOReport channel is
	// "Mesa Power State"; 30, 1 and 6 on three reads of one working SE 2),
	// so it says nothing about the sensor's health and is not used.
	serial := partSerial(sensor, "SerialNumber")
	switch {
	case hasSelfTest && selfTest == "OK":
		add(cat, title, m.TouchIDSelfTestPassed(), Pass, joinWith(m.JoinSentences, "MesaSelfTest = OK", cal))
	case hasSelfTest:
		add(cat, title, m.TouchIDSelfTestFailed(selfTest), Warn, joinWith(m.JoinSentences, m.TouchIDSelfTestFailedDetail(selfTest), cal))
	case serial != "" && calSource == "FDR":
		add(cat, title, m.TouchIDOnline(), Pass, m.TouchIDOnlineDetail())
	case sensor != nil || driver != nil:
		var why []string
		if serial == "" {
			why = append(why, m.TouchIDNoSerial())
		}
		if calSource != "FDR" {
			why = append(why, m.TouchIDCalibrationOther(cmp.Or(calSource, m.ValueNotReported())))
		}
		add(cat, title, m.TouchIDFound(), Info, joinWith(m.JoinSentences, joinWith(m.JoinList, why...), m.TouchIDUnsure()))
	case !HasTouchID(productType):
	case len(raw.Parts) > 0:
		// The other parts were read, so the sensor's absence is real.
		add(cat, title, m.TouchIDMissing(), Warn, m.TouchIDMissingDetail())
	default:
		add(cat, title, "", Unknown, m.PartsUnread())
	}
}

// cameraLabels name the camera modules by their IORegistry key prefix.
var cameraLabels = map[string]func(locales.Messages) string{
	"Back":                            locales.Messages.PartBackWide,
	"BackTele":                        locales.Messages.PartBackTele,
	"BackSuperWide":                   locales.Messages.PartBackUltraWide,
	"Front":                           locales.Messages.PartFront,
	"FrontIR":                         locales.Messages.PartFaceIDCamera,
	"FrontIRStructuredLightProjector": locales.Messages.PartFaceIDProjector,
}

var serialNumStringRe = regexp.MustCompile(`^(.*?)(CameraModule)?SerialNumString$`)

// partsSection lists the serial numbers the parts report now. The factory
// record of them (syscfg) is not readable over lockdown, so they are
// information for comparing with a repair record or the parts' labels, not
// evidence of a replacement.
// placeholderRe matches the filler drivers report for a part a model
// lacks, as the Face ID projector's "XXXXXXXX" on an iPhone 8.
var placeholderRe = regexp.MustCompile(`^(?:[Xx]+|0+|[Ff]+)$`)

// partSerial reads a part's serial number, which drivers give as text or
// as raw bytes (the Touch ID sensor's 16 bytes), shown then in hex.
// Placeholders read as none.
func partSerial(m map[string]any, key string) string {
	var s string
	switch v := m[key].(type) {
	case string:
		s = v
	case []byte:
		if t, ok := plistx.String(v); ok {
			s = t
		} else {
			s = fmt.Sprintf("%x", v)
		}
	}
	s = strings.TrimSpace(s)
	if placeholderRe.MatchString(s) {
		return ""
	}
	return s
}

func partsSection(m locales.Messages, raw *RawData, b *Battery, productType string) (Section, bool) {
	var fields []Field
	faceID := HasFaceID(productType)
	if cam := raw.Parts["camera"]; cam != nil {
		var keys []string
		for k := range cam {
			if serialNumStringRe.MatchString(k) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			s := partSerial(cam, k)
			prefix := serialNumStringRe.FindStringSubmatch(k)[1]
			// Face ID's modules belong only to models that have it.
			if s == "" || (!faceID && strings.HasPrefix(prefix, "FrontIR")) {
				continue
			}
			label := k
			if name, ok := cameraLabels[prefix]; ok {
				label = name(m)
			}
			fields = append(fields, Field{label, s})
		}
	}
	product := raw.Parts["product"]
	str := partSerial
	if cg := str(product, "coverglass-serial-number"); cg != "" {
		first, _, _ := strings.Cut(cg, "+")
		fields = append(fields, Field{m.PartCoverGlass(), first})
	}
	// The panel's record joins several serials with '+', in an order that
	// differs between models: shown whole, not taken apart.
	panel := str(product, "raw-panel-serial-number")
	if panel == "" {
		panel = str(raw.Parts["display"], "Panel_ID")
	}
	if panel != "" {
		fields = append(fields, Field{m.PartPanel(), panel})
	}
	for _, f := range []struct{ key, label string }{
		{"ambient-light-sensor-serial-num", m.PartAmbientLight()},
		{"backglass-compass-serial-number", m.PartBackGlass()},
		{"nova-serial-num", "nova-serial-num"},
	} {
		if s := str(product, f.key); s != "" {
			fields = append(fields, Field{f.label, s})
		}
	}
	if t := raw.Parts["touch"]; t != nil {
		if n, ok := plistx.Int(t["Multitouch ID"]); ok {
			fields = append(fields, Field{m.PartTouch(), fmt.Sprint(uint64(n))})
		}
	}
	if s := str(raw.Parts["touchid"], "SerialNumber"); s != "" {
		fields = append(fields, Field{m.PartFingerprint(), s})
	}
	if s := partSerial(raw.Parts["storage"], "Serial Number"); s != "" {
		fields = append(fields, Field{m.PartStorage(), s})
	}
	if b != nil && b.Serial != "" {
		fields = append(fields, Field{m.CategoryBattery(), b.Serial})
	}
	if len(fields) == 0 {
		return Section{}, false
	}
	note := Field{m.FieldNote(), m.PartsNote()}
	return Section{m.SectionParts(), append([]Field{note}, fields...)}, true
}

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

// overCapacityPercent is how far above its design capacity a battery may
// charge before it is suspicious. Set from samples: a genuine iPhone 16 cell
// logged a peak full charge (MaximumFCC) of 105.05% of design, an
// aftermarket iPhone 12 cell charges to 108.4%. Revisit as samples grow.
const overCapacityPercent = 107

// addBatterySource weighs the signs of a replaced or non-genuine battery.
// iOS's own "unknown part" verdict is not readable over lockdown, so this
// relies on what the gas gauge reports.
func addBatterySource(m locales.Messages, add func(cat, title, value string, st Status, detail string), b *Battery, overCap bool, mfg *SerialDate, now time.Time) {
	var strong []string
	if overCap {
		strong = append(strong, m.BatteryOverCapacity(int(b.MaxCapacity), int(math.Round(b.HealthPercent)), int(b.DesignCapacity)))
	}
	// Cycle counts only grow: an older log that counted more cycles than
	// the battery now has means the battery was changed since.
	if b.HasLogCycles && b.HasCycleCount && !b.CyclesFromLog && b.LogCycles > b.CycleCount+2 {
		strong = append(strong, m.BatteryLogCyclesHigher(b.LogDate.Format("2006-01-02"), int(b.LogCycles), int(b.CycleCount)))
	}
	if mfg != nil && b.HasCycleCount {
		made := time.Date(mfg.Year, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, (mfg.Week-1)*7)
		if age := now.Sub(made); age > 548*24*time.Hour && b.CycleCount < 50 {
			strong = append(strong, m.BatteryTooFewCycles(age.Hours()/24/365, int(b.CycleCount)))
		}
	}
	// The gauge's DesignCapacity is no sign by itself: a genuine iPhone 16
	// reported 3521 mAh against the 3561 mAh published for it.
	detail := joinWith(m.JoinClauses, strong...)
	// Without capacities or a cycle count there was nothing to weigh.
	weighed := (b.HasDesign && b.HasMax) || b.HasCycleCount
	cat, title := m.CategoryBattery(), m.CheckBatteryOrigin()
	switch {
	case len(strong) >= 2:
		add(cat, title, m.BatteryNonGenuine(), Fail, detail)
	case len(strong) == 1:
		add(cat, title, m.BatteryReplaced(), Warn, detail)
	case !weighed:
		add(cat, title, "", Unknown, m.BatteryOriginUnread())
	default:
		// No evidence either way is not proof of a genuine battery.
		add(cat, title, m.BatteryNoSigns(), Pass, m.BatteryNoSignsDetail())
	}
}

// joinWith joins the parts that are not empty with join, a message such as
// JoinSentences that puts the language's punctuation between two texts.
func joinWith(join func(first, next string) string, parts ...string) string {
	out := ""
	for _, p := range parts {
		switch {
		case p == "":
		case out == "":
			out = p
		default:
			out = join(out, p)
		}
	}
	return out
}

// ErrorText says err in the language lang: the steps of the collection and
// the errors a user can act on in their words, other errors as they are.
func ErrorText(lang string, err error) string {
	m := locales.In(lang)
	var step *StepError
	switch {
	case errors.As(err, &step):
		return m.StepFailed(step.Step.Text(lang, step.Target), ErrorText(lang, step.Err))
	case errors.Is(err, usbmux.ErrNoDaemon):
		return m.WithNote(m.ErrorNoUsbmuxd(), err.Error())
	case errors.Is(err, lockdown.ErrPairingPending):
		return m.ErrorPairingPending()
	case errors.Is(err, lockdown.ErrPasswordLocked):
		return m.ErrorPasswordLocked()
	case errors.Is(err, lockdown.ErrUserDenied):
		return m.ErrorUserDenied()
	case errors.Is(err, services.ErrNoAnalytics):
		return m.NoAnalytics()
	case errors.Is(err, services.ErrNoBatteryInAnalytics):
		return m.NoBatteryInAnalytics()
	}
	return err.Error()
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
