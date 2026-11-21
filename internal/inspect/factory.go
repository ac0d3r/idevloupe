package inspect

import (
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/ac0d3r/idevloupe/pkg/services"
)

func (a *analysis) addresses() {
	m, v := a.m, a.v
	cat := m.CategoryIdentity()
	for _, p := range []struct{ label, now, factory string }{
		{m.FieldWiFiAddress(), v.str("WiFiAddress"), v.sysMAC("WMac")},
		{m.FieldBluetoothAddress(), v.str("BluetoothAddress"), v.sysMAC("BMac")},
		{m.FieldCellularAddress(), v.str("EthernetAddress"), v.sysMAC("EMac")},
	} {
		if p.now == "" || p.factory == "" {
			continue
		}
		ch := Check{Category: cat, Title: p.label, Value: m.ValueConsistent(), Status: Pass,
			Factory: Reading{Value: p.factory, ID: true}, Read: Reading{Value: p.now, ID: true}}
		if !strings.EqualFold(p.now, p.factory) {
			ch.Value, ch.Status = m.ValueInconsistent(), Warn
			ch.Detail = m.JoinSentences(m.FactoryValues(p.factory, p.now), m.AddressMismatchDetail())
		}
		a.r.Checks = append(a.r.Checks, ch)
	}
}

// Samples: an iPhone 8 (iOS 16) whose front camera, panel and glass differ
// from the record; an iPhone X (iOS 16) matching despite a replaced screen.
// A match is consistency, not proof of an original part; a mismatch is a
// replacement, not proof of a non-genuine one.
func (a *analysis) factory() {
	m, raw := a.m, a.raw
	cat := m.CategoryParts()
	if raw.SysCfg == nil {
		// iOS 17.4+ gives no MobileGestalt, so no record; Unknown is only for a
		// record that should have been read and was not.
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
		// The screen's record joins serials with '+', the first its own; a match is
		// weak, as the original serial is often written onto a new screen.
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
	// The Touch ID record is a 4-byte header, then the 16-byte serial with each
	// 32-bit word byte-swapped: 6c101702 29858060 … is 0217106c 60808529 ….
	if rec := raw.SysCfg["NSrN"]; len(rec) == 20 {
		sn := make([]byte, 16)
		for i := 0; i < 16; i += 4 {
			w := rec[4+i : 8+i]
			sn[i], sn[i+1], sn[i+2], sn[i+3] = w[3], w[2], w[1], w[0]
		}
		a.factoryCheck(cat, m.PartFingerprint(), hex.EncodeToString(sn), partSerial(raw.Parts["touchid"], "SerialNumber"), false, false)
	}
}

func (a *analysis) factoryCheck(cat, part, factory, current string, dated, screen bool) {
	m := a.m
	title := m.CheckPartFactory(part)
	reading := func(serial string) Reading {
		rd := Reading{Value: serial}
		if dated {
			if d, ok := PartDate(serial, a.now); ok {
				rd.Date = d.Format("2006-01-02")
			}
		}
		return rd
	}
	say := func(rd Reading) string {
		if rd.Date == "" {
			return rd.Value
		}
		return m.SerialWithDate(rd.Value, rd.Date)
	}
	f, r := reading(factory), reading(current)
	ch := Check{Category: cat, Title: title, Short: part, Factory: f, Read: r}
	switch {
	case current == "":
		ch.Status, ch.Detail = Unknown, m.FactoryCurrentUnread(say(f))
	case strings.EqualFold(factory, current) && screen:
		ch.Value, ch.Status, ch.Detail = m.ValueConsistent(), Info, m.JoinSentences(m.FactorySame(say(r)), m.FactoryScreenMatchDetail())
	case strings.EqualFold(factory, current):
		ch.Value, ch.Status, ch.Detail = m.ValueConsistent(), Pass, m.JoinSentences(m.FactorySame(say(r)), m.FactoryMatchDetail())
	default:
		ch.Value, ch.Status, ch.Detail = m.ValueInconsistent(), Warn, m.JoinSentences(m.FactoryValues(say(f), say(r)), m.FactoryMismatchDetail())
	}
	a.r.Checks = append(a.r.Checks, ch)
}

// PartDate: after a 3-character supplier code, the year's last digit, the
// ISO week and the weekday (Monday 1); F0W94170… is 2019-10-13. The decade
// is the latest not in the future at now.
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
