package inspect

import (
	"cmp"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/pkg/plistx"
)

// Pearl self-test codes are undocumented: 0 on a working iPhone 16, 8 on an
// iPhone X flagged by iOS. Non-zero only asks for a manual test.
func (a *analysis) faceID() {
	m := a.m
	pearl := a.raw.Parts["pearl"]
	selfTest, hasSelfTest := plistx.Int(pearl["PearlSelfTestResult"])
	// FDRValidated (iOS 17+): the module matches the factory pairing data. Seen
	// true on working units; false is inferred, no sample yet.
	fdr, hasFDR := plistx.Bool(pearl["FDRValidated"])
	const pearlKey = "ApplePearlSEPDriver PearlSelfTestResult"
	parts := m.CategoryParts()
	switch {
	case hasFDR && !fdr:
		a.add(parts, m.CheckFaceID(), m.FaceIDFDRFailed(), Warn, m.FaceIDFDRFailedDetail())
	case hasSelfTest && selfTest == 0:
		detail := pearlKey + " = 0"
		if hasFDR {
			detail = m.JoinList(detail, m.FaceIDFDRPassed())
		}
		a.add(parts, m.CheckFaceID(), m.ValuePassed(), Pass, detail)
	case hasSelfTest:
		a.add(parts, m.CheckFaceID(), m.FaceIDFailed(int(selfTest)), Info, m.FaceIDFailedDetail(pearlKey, int(selfTest)))
	case HasFaceID(a.productType):
		a.add(parts, m.CheckFaceID(), "", Unknown, m.FaceIDUnread())
	}
}

// Samples (iPhone 8 iOS 16, SE 2 iOS 27, both working): a sensor serial and
// MesaCalBlobSource = FDR; only the iPhone 8 has MesaSelfTest "OK".
func (a *analysis) touchID() {
	m, raw, productType := a.m, a.raw, a.productType
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
	// mesa-state is the sensor's power state, not its health; unused.
	serial := partSerial(sensor, "SerialNumber")
	switch {
	case hasSelfTest && selfTest == "OK":
		a.add(cat, title, m.TouchIDSelfTestPassed(), Pass, joinWith(m.JoinSentences, "MesaSelfTest = OK", cal))
	case hasSelfTest:
		a.add(cat, title, m.TouchIDSelfTestFailed(selfTest), Warn, joinWith(m.JoinSentences, m.TouchIDSelfTestFailedDetail(selfTest), cal))
	case serial != "" && calSource == "FDR":
		a.add(cat, title, m.TouchIDOnline(), Pass, m.TouchIDOnlineDetail())
	case sensor != nil || driver != nil:
		var why []string
		if serial == "" {
			why = append(why, m.TouchIDNoSerial())
		}
		if calSource != "FDR" {
			why = append(why, m.TouchIDCalibrationOther(cmp.Or(calSource, m.ValueNotReported())))
		}
		a.add(cat, title, m.TouchIDFound(), Info, joinWith(m.JoinSentences, joinWith(m.JoinList, why...), m.TouchIDUnsure()))
	case !HasTouchID(productType):
	case len(raw.Parts) > 0:
		// The other parts were read, so the sensor's absence is real.
		a.add(cat, title, m.TouchIDMissing(), Warn, m.TouchIDMissingDetail())
	default:
		a.add(cat, title, "", Unknown, m.PartsUnread())
	}
}

var cameraLabels = map[string]func(locales.Messages) string{
	"Back":                            locales.Messages.PartBackWide,
	"BackTele":                        locales.Messages.PartBackTele,
	"BackSuperWide":                   locales.Messages.PartBackUltraWide,
	"Front":                           locales.Messages.PartFront,
	"FrontIR":                         locales.Messages.PartFaceIDCamera,
	"FrontIRStructuredLightProjector": locales.Messages.PartFaceIDProjector,
}

var serialNumStringRe = regexp.MustCompile(`^(.*?)(CameraModule)?SerialNumString$`)

// Filler for a part a model lacks, as the Face ID projector's "XXXXXXXX" on an iPhone 8.
var placeholderRe = regexp.MustCompile(`^(?:[Xx]+|0+|[Ff]+)$`)

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

// syscfg is not readable over lockdown: these serials are for comparison,
// not evidence of a replacement.
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
	// The panel's record joins serials with '+' in a model-specific order: shown whole.
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
	return Section{Title: m.SectionParts(), Note: m.PartsNote(), Fields: fields}, true
}
