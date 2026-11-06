package inspect

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/pkg/plistx"
)

// Analyze turns raw data into a report in the language lang, a tag of
// locales.Catalog's languages. It is pure, so tests feed it recorded data,
// and the window makes the report again when the language changes.
func Analyze(raw *RawData, now time.Time, lang string) *Report {
	a := newAnalysis(raw, now, lang)
	a.summary()
	a.basics()
	a.identity()
	a.security()
	a.cellular()
	a.battery()
	a.faceID()
	a.touchID()
	a.other()

	r := a.r
	r.Sections = buildSections(a.m, a.v, r.Summary)
	if sec, ok := partsSection(a.m, raw, r.Battery, a.productType); ok {
		r.Sections = append(r.Sections, sec)
	}
	r.Raw = buildRaw(lang, raw)
	r.Manual = manualChecks(a.m, a.productType)
	return r
}

// analysis is one run of Analyze: the data, the language, the report it
// makes, and the values that several checks share.
type analysis struct {
	raw  *RawData
	v    view
	m    locales.Messages
	lang string
	now  time.Time
	r    *Report

	productType string
	model       Model
	known       bool // productType is in the model table
	modelNumber string
	regionInfo  string
	telephony   bool
	// mfg is the week of manufacture a legacy serial number tells, if any.
	mfg *SerialDate
}

func newAnalysis(raw *RawData, now time.Time, lang string) *analysis {
	v := view{raw}
	a := &analysis{raw: raw, v: v, m: locales.In(lang), lang: lang, now: now,
		r: &Report{Lang: lang, Time: now, UDID: raw.UDID}}
	for _, e := range raw.Errors {
		a.r.Errors = append(a.r.Errors, ErrorText(lang, e))
	}
	a.productType = v.str("ProductType")
	a.model, a.known = LookupModel(a.productType, lang)
	a.modelNumber = v.str("ModelNumber")
	a.regionInfo = v.str("RegionInfo")
	telephony, ok := v.boolean("TelephonyCapability")
	if !ok {
		telephony = v.str("InternationalMobileEquipmentIdentity") != "" || v.str("DeviceClass") == "iPhone"
	}
	a.telephony = telephony
	return a
}

// add records a check.
func (a *analysis) add(cat, title, value string, st Status, detail string) {
	a.r.Checks = append(a.r.Checks, Check{Category: cat, Title: title, Value: value, Status: st, Detail: detail})
}

// summary fills the report's header.
func (a *analysis) summary() {
	v, r := a.v, a.r
	marketing := a.model.Name
	if !a.known {
		marketing = v.gestaltStr("ArtworkDeviceProductDescription")
	}
	if marketing == "" {
		marketing = a.productType
	}
	s := Summary{
		DeviceName:  v.str("DeviceName"),
		Marketing:   marketing,
		ProductType: a.productType,
		IOS:         strings.TrimSpace(v.str("ProductVersion") + " (" + v.str("BuildVersion") + ")"),
		ModelNumber: a.modelNumber + a.regionInfo,
		Serial:      v.str("SerialNumber"),
		IMEI:        v.str("InternationalMobileEquipmentIdentity"),
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
	if name, ok := RegionName(a.regionInfo, a.lang); ok {
		s.Region = name
	} else {
		s.Region = a.regionInfo
	}
	r.Summary = s
}

var betaBuild = regexp.MustCompile(`^\d+[A-Z]\d+[a-z]$`)

// basics checks what the unit is: model, kind, region, chip, system.
func (a *analysis) basics() {
	m, v := a.m, a.v
	marketing := a.r.Summary.Marketing
	basic := m.CategoryBasic()
	switch {
	case a.productType == "":
		a.add(basic, m.CheckModel(), "", Unknown, m.KeyNotRead("ProductType"))
	case a.known:
		a.add(basic, m.CheckModel(), m.ValueWithCode(marketing, a.productType), Pass, "")
	default:
		a.add(basic, m.CheckModel(), m.ValueWithCode(marketing, a.productType), Info, m.ModelNotInTable())
	}

	kind := UnitKindOf(a.modelNumber, a.lang)
	kv := kind.Name
	if a.modelNumber != "" {
		kv = kind.Name + " · " + a.modelNumber + a.regionInfo
	}
	a.add(basic, m.CheckUnitType(), kv, kind.Status, kind.Detail)

	if a.regionInfo == "" {
		a.add(basic, m.CheckRegion(), "", Unknown, m.KeyNotRead("RegionInfo"))
	} else if name, ok := RegionName(a.regionInfo, a.lang); ok {
		a.add(basic, m.CheckRegion(), m.ValueWithCode(name, a.regionInfo), Pass, "")
	} else {
		a.add(basic, m.CheckRegion(), a.regionInfo, Info, m.RegionNotInTable())
	}

	// software_behavior's ChinaBrick is set on units made for mainland
	// China (WLAN wording, dual physical SIM), independently of RegionInfo.
	if cb, ok := v.domainBool("com.apple.mobile.software_behavior", "ChinaBrick"); ok && a.regionInfo != "" {
		isCH := strings.HasPrefix(strings.ToUpper(a.regionInfo), "CH/")
		if cb == isCH {
			a.add(basic, m.CheckRegionConsistency(), m.ValueConsistent(), Pass, m.RegionChinaBrick(a.regionInfo, strconv.FormatBool(cb)))
		} else {
			a.add(basic, m.CheckRegionConsistency(), m.ValueInconsistent(), Warn, m.RegionChinaBrickMismatch(a.regionInfo, strconv.FormatBool(cb)))
		}
	}
	if b, ok := v.boolean("ProductionSOC"); ok {
		if b {
			a.add(basic, m.CheckProductionChip(), m.ProductionChipYes(), Pass, "ProductionSOC = true")
		} else {
			a.add(basic, m.CheckProductionChip(), m.ProductionChipNo(), Fail, m.ProductionChipNoDetail())
		}
	} else {
		a.add(basic, m.CheckProductionChip(), "", Unknown, m.KeyNotRead("ProductionSOC"))
	}
	if b, ok := v.domainBool("com.apple.mobile.internal", "IsInternal"); ok {
		if b {
			a.add(basic, m.CheckInternal(), m.InternalApple(), Fail, "com.apple.mobile.internal IsInternal = true")
		} else {
			a.add(basic, m.CheckInternal(), m.ValueNo(), Pass, "")
		}
	}

	ver, build := v.str("ProductVersion"), v.str("BuildVersion")
	switch {
	case ver == "":
		a.add(basic, m.CheckSystemVersion(), "", Unknown, "")
	case betaBuild.MatchString(build):
		a.add(basic, m.CheckSystemVersion(), fmt.Sprintf("iOS %s (%s)", ver, build), Info, m.BetaBuildDetail())
	default:
		a.add(basic, m.CheckSystemVersion(), fmt.Sprintf("iOS %s (%s)", ver, build), Pass, "")
	}
	// Retail builds carry no ReleaseType; internal and seed builds do.
	switch rt := v.str("ReleaseType"); rt {
	case "":
	case "Internal", "NonUI":
		a.add(basic, m.CheckReleaseType(), rt, Fail, m.ReleaseInternalDetail(rt))
	case "Beta":
		a.add(basic, m.CheckReleaseType(), rt, Info, m.ReleaseBetaDetail())
	default:
		a.add(basic, m.CheckReleaseType(), rt, Warn, m.ReleaseOtherDetail(rt))
	}

	if n, ok := v.domainNum("com.apple.disk_usage", "TotalDiskCapacity"); ok && n > 0 {
		a.add(basic, m.CheckStorage(), m.StorageValue(NominalCapacity(n), float64(n)/1e9), Pass, "")
	} else {
		a.add(basic, m.CheckStorage(), "", Unknown, m.KeyNotRead("TotalDiskCapacity"))
	}
}

// identity checks the serial number, the IMEI and that the sources of
// the identifiers agree.
func (a *analysis) identity() {
	m, v := a.m, a.v
	serial := a.r.Summary.Serial
	imei := a.r.Summary.IMEI
	ident := m.CategoryIdentity()
	releaseYear := 0
	if a.known {
		releaseYear = a.model.Year
	}
	switch {
	case serial == "":
		a.add(ident, m.CheckSerial(), "", Unknown, m.KeyNotRead("SerialNumber"))
	case len(serial) == 12:
		if d, ok := DecodeSerial(serial, releaseYear, a.now); ok {
			a.mfg = &d
			a.add(ident, m.CheckSerial(), serial, Pass, m.SerialLegacy12(d.Text(a.lang)))
		} else {
			a.add(ident, m.CheckSerial(), serial, Warn, m.SerialLegacy12Bad())
		}
	case len(serial) == 10:
		a.add(ident, m.CheckSerial(), serial, Pass, m.SerialRandom())
	case len(serial) == 11:
		a.add(ident, m.CheckSerial(), serial, Pass, m.SerialLegacy11())
	default:
		a.add(ident, m.CheckSerial(), serial, Warn, m.SerialOddLength(len(serial)))
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
			a.add(ident, title, m.ValueConsistent(), Pass, values)
		} else {
			a.add(ident, title, m.ValueInconsistent(), Fail, m.JoinSentences(values, m.SourcesDisagree()))
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
	partNumber := func(number, region string) string {
		if number == "" || region == "" {
			return ""
		}
		return number + region
	}
	consistency(m.CheckModelConsistency(), []src{
		{"lockdown", partNumber(a.modelNumber, a.regionInfo)},
		{deviceTree, partNumber(v.platformStr("model-number"), v.platformStr("region-info"))},
		{"MobileGestalt", partNumber(v.gestaltStr("ModelNumber"), v.gestaltStr("RegionInfo"))},
	})
	if mlb := v.str("MLBSerialNumber"); mlb != "" {
		consistency(m.CheckBoardSerialConsistency(), []src{
			{"lockdown", mlb},
			{deviceTree, v.platformStr("mlb-serial-number")},
		})
	}

	if a.telephony {
		checkIMEI := func(title, val string) {
			switch {
			case val == "":
				a.add(ident, title, "", Warn, m.ImeiMissing())
			case len(val) != 15:
				a.add(ident, title, val, Fail, m.ImeiLength(len(val)))
			case !LuhnValid(val):
				a.add(ident, title, val, Fail, m.ImeiLuhnFailed())
			default:
				a.add(ident, title, val, Pass, m.ImeiLuhnPassed())
			}
		}
		checkIMEI("IMEI", imei)
		if imei2 := v.str("InternationalMobileEquipmentIdentity2"); imei2 != "" {
			checkIMEI("IMEI 2", imei2)
		}
	}
}

// security checks the locks and the management that bind the unit to an
// account or an organization, and the signs of a modified system.
func (a *analysis) security() {
	a.activation()
	a.findMy()
	a.supervision()
	a.management()
	a.jailbreak()
	a.systemFlags()
}

// activation checks the activation state.
func (a *analysis) activation() {
	m, v := a.m, a.v
	sec := m.CategorySecurity()
	switch st := v.str("ActivationState"); st {
	case "":
		a.add(sec, m.CheckActivation(), "", Unknown, "")
	case "Activated":
		a.add(sec, m.CheckActivation(), m.ActivationActivated(), Pass, "")
	case "Unactivated":
		a.add(sec, m.CheckActivation(), m.ActivationUnactivated(), Info, m.ActivationUnactivatedDetail())
	case "FactoryActivated":
		a.add(sec, m.CheckActivation(), m.ActivationFactory(), Warn, m.ActivationFactoryDetail())
	default:
		a.add(sec, m.CheckActivation(), st, Warn, m.ActivationOtherDetail())
	}
}

// findMy checks Activation Lock.
func (a *analysis) findMy() {
	m, v := a.m, a.v
	sec := m.CategorySecurity()
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
			a.add(sec, m.CheckFindMy(), m.FindMyOn(), Warn, joinWith(m.JoinSentences, fmDetail, m.FindMyOnDetail()))
		} else {
			a.add(sec, m.CheckFindMy(), m.ValueOff(), Pass, fmDetail)
		}
	} else {
		a.add(sec, m.CheckFindMy(), "", Unknown, m.FindMyUnknown())
	}
}

// supervision checks whether the unit is supervised.
func (a *analysis) supervision() {
	m, v := a.m, a.v
	sec := m.CategorySecurity()
	supervised, hasSup := v.domainBool("com.apple.mobile.chaperone", "DeviceIsChaperoned")
	if a.raw.MC != nil && a.raw.MC.Cloud != nil {
		if b, ok := a.raw.MC.Cloud["IsSupervised"].(bool); ok && (b || !hasSup) {
			supervised, hasSup = b, true
		}
	}
	if hasSup {
		if supervised {
			a.add(sec, m.CheckSupervision(), m.Supervised(), Warn, m.SupervisedDetail())
		} else {
			a.add(sec, m.CheckSupervision(), m.NotSupervised(), Pass, "")
		}
	} else {
		a.add(sec, m.CheckSupervision(), "", Unknown, "")
	}
}

// management checks enrollment in Apple Business Manager, MDM and the
// configuration profiles.
func (a *analysis) management() {
	m, r := a.m, a.r
	sec := m.CategorySecurity()
	mc := a.raw.MC
	if mc == nil {
		a.add(sec, m.CheckMDM(), "", Unknown, m.ProfilesUnread())
		return
	}
	cloud := mc.Cloud
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
	for _, p := range mc.Profiles {
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
		a.add(sec, m.CheckMDM(), val, Fail, m.MdmEnrolledDetail())
	case len(mdm) > 0:
		name := mdm[0].Name
		if mdm[0].Organization != "" {
			name += " · " + mdm[0].Organization
		}
		a.add(sec, m.CheckMDM(), m.MdmProfile(name), Warn, m.MdmProfileDetail())
	default:
		a.add(sec, m.CheckMDM(), m.ValueNotFound(), Pass, "")
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
		a.add(sec, m.CheckProfiles(), m.ProfileCount(len(r.Profiles)), Info, joinWith(m.JoinEnumeration, names...))
	} else {
		a.add(sec, m.CheckProfiles(), m.ValueNone(), Pass, "")
	}
}

// jailbreak looks for jailbreak apps and the afc2 service.
func (a *analysis) jailbreak() {
	m, r := a.m, a.r
	sec := m.CategorySecurity()
	r.AppCount = len(a.raw.Apps)
	var jb []string
	for _, app := range a.raw.Apps {
		if name, ok := jailbreakApps[app.BundleID]; ok {
			r.Apps = append(r.Apps, AppEntry{app.BundleID, app.Name, app.Version, app.Type, m.JailbreakApp(name)})
			jb = append(jb, name)
		}
	}
	afc2 := a.raw.AFC2 != nil && *a.raw.AFC2
	appsRead, afc2Read := a.raw.AppsErr == nil, a.raw.AFC2 != nil
	switch {
	case afc2 || len(jb) > 0:
		var ev []string
		if afc2 {
			ev = append(ev, m.JailbreakAFC2())
		}
		if len(jb) > 0 {
			ev = append(ev, m.JailbreakApps(joinWith(m.JoinEnumeration, jb...)))
		}
		a.add(sec, m.CheckJailbreak(), m.JailbreakFound(), Fail, joinWith(m.JoinClauses, ev...))
	case appsRead && afc2Read:
		a.add(sec, m.CheckJailbreak(), m.ValueNotFound(), Pass, m.JailbreakNone())
	case !appsRead && !afc2Read:
		a.add(sec, m.CheckJailbreak(), "", Unknown, m.JailbreakUnread())
	case !appsRead:
		a.add(sec, m.CheckJailbreak(), m.ValueNotFoundIncomplete(), Unknown, m.JailbreakNoApps())
	default:
		a.add(sec, m.CheckJailbreak(), m.ValueNotFoundIncomplete(), Unknown, m.JailbreakNoAFC2())
	}
}

// systemFlags reports settings a retail unit in use does not have.
func (a *analysis) systemFlags() {
	m, v := a.m, a.v
	sec := m.CategorySecurity()
	if b, ok := v.domainBool("com.apple.security.mac.amfi", "DeveloperModeStatus"); ok && b {
		a.add(sec, m.CheckDeveloperMode(), m.ValueOn(), Info, m.DeveloperModeDetail())
	}
	if b, ok := v.boolean("BrickState"); ok && b {
		a.add(sec, m.CheckBrickState(), "BrickState", Warn, m.BrickStateDetail())
	}
	if nv := v.nvram(); nv != nil {
		if args, ok := plistx.String(nv["boot-args"]); ok && strings.TrimSpace(args) != "" {
			a.add(sec, m.CheckBootArgs(), args, Warn, m.BootArgsDetail())
		}
	}
}

// cellular checks the baseband and the SIM of a unit that has them.
func (a *analysis) cellular() {
	if !a.telephony {
		return
	}
	m, v := a.m, a.v
	cell := m.CategoryCellular()
	bb := v.str("BasebandVersion")
	bbStatus := v.str("BasebandStatus")
	if bb != "" && bbStatus != "" && bbStatus != "BBInfoAvailable" {
		a.add(cell, m.CheckBaseband(), bb, Warn, m.BasebandNotReady(bbStatus))
	} else if bb != "" {
		a.add(cell, m.CheckBaseband(), bb, Pass, "")
	} else {
		a.add(cell, m.CheckBaseband(), m.ValueNotRead(), Fail, m.BasebandMissing())
	}
	switch st := v.str("SIMStatus"); st {
	case "":
	case "kCTSIMSupportSIMStatusReady":
		a.add(cell, m.CheckSIM(), m.SimReady(), Pass, "")
	case "kCTSIMSupportSIMStatusNotInserted":
		a.add(cell, m.CheckSIM(), m.SimNotInserted(), Info, "")
	case "kCTSIMSupportSIMStatusPINLocked":
		a.add(cell, m.CheckSIM(), m.SimPINLocked(), Info, "")
	default:
		a.add(cell, m.CheckSIM(), strings.TrimPrefix(st, "kCTSIMSupportSIMStatus"), Info, st)
	}
}

// other reports what fits no category: an unfinished setup.
func (a *analysis) other() {
	m, v := a.m, a.v
	if b, ok := v.domainBool("com.apple.PurpleBuddy", "SetupDone"); ok && !b {
		a.add(m.CategoryOther(), m.CheckSetup(), m.SetupUnfinished(), Info, m.SetupUnfinishedDetail())
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
