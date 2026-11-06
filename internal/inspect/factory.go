package inspect

import (
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/ac0d3r/idevloupe/pkg/services"
)

// addresses compares the network addresses lockdown gives with those of
// the factory record: a difference means another logic board or radio, or
// an address written over.
func (a *analysis) addresses() {
	m, v := a.m, a.v
	var same, differ []string
	for _, p := range []struct{ label, now, factory string }{
		{m.FieldWiFiAddress(), v.str("WiFiAddress"), v.sysMAC("WMac")},
		{m.FieldBluetoothAddress(), v.str("BluetoothAddress"), v.sysMAC("BMac")},
		{m.FieldCellularAddress(), v.str("EthernetAddress"), v.sysMAC("EMac")},
	} {
		switch {
		case p.now == "" || p.factory == "":
		case strings.EqualFold(p.now, p.factory):
			same = append(same, m.LabelValue(p.label, p.now))
		default:
			differ = append(differ, m.LabelValue(p.label, m.FactoryValues(p.factory, p.now)))
		}
	}
	cat, title := m.CategoryIdentity(), m.CheckAddressesFactory()
	switch {
	case len(differ) > 0:
		a.add(cat, title, m.ValueInconsistent(), Warn, m.JoinSentences(joinWith(m.JoinList, differ...), m.AddressMismatchDetail()))
	case len(same) > 0:
		a.add(cat, title, m.ValueConsistent(), Pass, joinWith(m.JoinList, same...))
	}
}

// factory compares the serial numbers of the parts the factory
// configuration records with those the parts report now. Samples: an
// iPhone 8 (iOS 16) whose front camera, display panel and touch glass differ
// from the record, as 3uTools also reports for the camera and the panel; an
// iPhone X (iOS 16) whose record all matches although its screen was
// replaced, the original's serial most likely written onto the new one. So a
// match is consistency, not proof that a part is original, and a mismatch
// is a replacement, not proof that the new part is not genuine.
func (a *analysis) factory() {
	m, raw := a.m, a.raw
	cat := m.CategoryParts()
	if raw.SysCfg == nil {
		// iOS 17.4 and later give no MobileGestalt: the record is not to be
		// had, which the manual checks' parts history covers. Unknown is for
		// a record that should have been read and was not.
		switch {
		case raw.SysCfgErr != nil:
			a.add(cat, m.CheckFactoryRecord(), "", Unknown, m.WithNote(m.FactoryUnread(), raw.SysCfgErr.Error()))
		case errors.Is(raw.GestaltErr, services.ErrGestaltDeprecated):
		case raw.GestaltErr != nil:
			a.add(cat, m.CheckFactoryRecord(), "", Unknown, m.WithNote(m.FactoryUnread(), raw.GestaltErr.Error()))
		case raw.Gestalt != nil:
			a.add(cat, m.CheckFactoryRecord(), "", Unknown, m.FactoryUnread())
		}
		return
	}
	cam, product := raw.Parts["camera"], raw.Parts["product"]
	panel := partSerial(product, "raw-panel-serial-number")
	if panel == "" {
		panel = partSerial(raw.Parts["display"], "Panel_ID")
	}
	battery := ""
	if b := a.r.Battery; b != nil {
		battery = b.Serial
	}
	first := func(s string) string {
		f, _, _ := strings.Cut(s, "+")
		return f
	}
	for _, p := range []struct {
		name, tag, current string
		// screen: the record is the panel's or the glass's, several serials
		// joined with '+' of which the first is the part's own, and a match
		// is weak, the original serial being often written onto a new screen
		// (the iPhone X sample).
		screen bool
	}{
		{m.PartBattery(), "Batt", battery, false},
		{m.PartBackWide(), "BCMS", partSerial(cam, "BackCameraModuleSerialNumString"), false},
		{m.PartFront(), "FCMS", partSerial(cam, "FrontCameraModuleSerialNumString"), false},
		{m.PartDisplayPanel(), "LCM#", panel, true},
		{m.PartCoverGlass(), "MtSN", partSerial(product, "coverglass-serial-number"), true},
	} {
		factory := a.v.sysText(p.tag)
		if factory == "" {
			continue // this model's factory does not record the part
		}
		current := p.current
		if p.screen {
			factory, current = first(factory), first(current)
		}
		a.factoryCheck(cat, p.name, factory, current, true, p.screen)
	}
	// The Touch ID sensor's record is 4 bytes of header, then the sensor's
	// 16-byte serial number with each 32-bit word byte-swapped: an iPhone 8's
	// 6c101702 29858060 … is the sensor's 0217106c 60808529 ….
	if rec := raw.SysCfg["NSrN"]; len(rec) == 20 {
		sn := make([]byte, 16)
		for i := 0; i < 16; i += 4 {
			w := rec[4+i : 8+i]
			sn[i], sn[i+1], sn[i+2], sn[i+3] = w[3], w[2], w[1], w[0]
		}
		a.factoryCheck(cat, m.PartFingerprint(), hex.EncodeToString(sn), partSerial(raw.Parts["touchid"], "SerialNumber"), false, false)
	}
}

// factoryCheck adds the comparison of one part's factory serial with its
// current one. dated tells serials that carry a date of manufacture; screen,
// a part whose match is only worth knowing, not a pass.
func (a *analysis) factoryCheck(cat, part, factory, current string, dated, screen bool) {
	m := a.m
	title := m.CheckPartFactory(part)
	say := func(serial string) string {
		if !dated {
			return serial
		}
		if d, ok := PartDate(serial, a.now); ok {
			return m.SerialWithDate(serial, d.Format("2006-01-02"))
		}
		return serial
	}
	switch {
	case current == "":
		a.add(cat, title, "", Unknown, m.FactoryCurrentUnread(say(factory)))
	case strings.EqualFold(factory, current) && screen:
		a.add(cat, title, m.ValueConsistent(), Info, m.JoinSentences(m.FactorySame(say(current)), m.FactoryScreenMatchDetail()))
	case strings.EqualFold(factory, current):
		a.add(cat, title, m.ValueConsistent(), Pass, m.JoinSentences(m.FactorySame(say(current)), m.FactoryMatchDetail()))
	default:
		a.add(cat, title, m.ValueInconsistent(), Warn, m.JoinSentences(m.FactoryValues(say(factory), say(current)), m.FactoryMismatchDetail()))
	}
}

// PartDate reads the date of manufacture a part's serial number carries
// after its three-character supplier code: the year's last digit, the ISO
// week and the weekday, Monday 1. An iPhone 8's front camera F0W94170AU…
// was made on 2019-10-13 (2019, week 41, day 7), as 3uTools also dates it,
// and so its other parts. The decade is the latest that is not in the future
// at now. Serials of other forms, such as the Touch ID sensor's hex, are not
// dated.
func PartDate(serial string, now time.Time) (time.Time, bool) {
	if len(serial) < 12 || serial[0] < 'A' || serial[0] > 'Z' {
		return time.Time{}, false
	}
	digit := func(c byte) (int, bool) { return int(c - '0'), c >= '0' && c <= '9' }
	y, ok1 := digit(serial[3])
	w1, ok2 := digit(serial[4])
	w2, ok3 := digit(serial[5])
	d, ok4 := digit(serial[6])
	week := w1*10 + w2
	if !ok1 || !ok2 || !ok3 || !ok4 || week < 1 || week > 53 || d < 1 || d > 7 {
		return time.Time{}, false
	}
	date := isoDate(now.Year()-(now.Year()-y)%10, week, d)
	if date.After(now) {
		return time.Time{}, false
	}
	return date, true
}
