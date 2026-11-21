package inspect

import (
	"sort"
	"strings"
	"time"
)

func isoDate(year, week, day int) time.Time {
	// ISO week 1 is the week with January 4th in it.
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	monday := jan4.AddDate(0, 0, -((int(jan4.Weekday()) + 6) % 7))
	return monday.AddDate(0, 0, (week-1)*7+day-1)
}

func (a *analysis) deviceMade() (time.Time, bool) {
	if d := a.mfg; d != nil && !d.Ambiguous {
		return isoDate(d.Year, d.Week, 7), true
	}
	return time.Time{}, false
}

type newerPart struct {
	name, serial string
	made         time.Time
}

func (a *analysis) partsNewer(serials map[string]string) (newer []newerPart, dated int) {
	device, ok := a.deviceMade()
	if !ok {
		return nil, 0
	}
	for name, serial := range serials {
		made, ok := PartDate(serial, a.now)
		if !ok {
			continue
		}
		dated++
		if made.After(device) {
			newer = append(newer, newerPart{name, serial, made})
		}
	}
	sort.Slice(newer, func(i, j int) bool { return newer[i].name < newer[j].name })
	return newer, dated
}

// A part fitted at the factory cannot be newer than the device. Sample: an
// iPhone 12 made in October 2021 with a battery dated October 2025.
func (a *analysis) partDates() {
	m, raw := a.m, a.raw
	device, ok := a.deviceMade()
	if !ok {
		return
	}
	serials := map[string]string{}
	if cam := raw.Parts["camera"]; cam != nil {
		for k := range cam {
			sub := serialNumStringRe.FindStringSubmatch(k)
			if sub == nil {
				continue
			}
			name := k
			if label, ok := cameraLabels[sub[1]]; ok {
				name = label(m)
			}
			serials[name] = partSerial(cam, k)
		}
	}
	first := func(s string) string {
		f, _, _ := strings.Cut(s, "+")
		return f
	}
	product := raw.Parts["product"]
	serials[m.PartCoverGlass()] = first(partSerial(product, "coverglass-serial-number"))
	panel := partSerial(product, "raw-panel-serial-number")
	if panel == "" {
		panel = partSerial(raw.Parts["display"], "Panel_ID")
	}
	serials[m.PartDisplayPanel()] = first(panel)
	newer, dated := a.partsNewer(serials)
	cat, title := m.CategoryParts(), m.CheckPartDates()
	made := device.Format("2006-01-02")
	switch {
	case len(newer) == 0 && raw.SysCfg != nil:
		// The factory record compared the parts, which says more.
	case len(newer) > 0:
		var items []string
		for _, p := range newer {
			items = append(items, m.LabelValue(p.name, m.SerialWithDate(p.serial, p.made.Format("2006-01-02"))))
		}
		a.add(cat, title, m.PartsNewer(len(newer)), Warn,
			m.JoinSentences(m.JoinSentences(m.DeviceMadeOn(made), joinWith(m.JoinList, items...)), m.PartsNewerDetail()))
	case dated > 0:
		a.add(cat, title, m.PartsNotNewer(), Pass, m.PartsNotNewerDetail(dated, made))
	}
}
