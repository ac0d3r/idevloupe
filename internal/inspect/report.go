// Package inspect builds inspection reports. A check passes only on positive
// evidence: when its data could not be read, it is Unknown, never Pass.
package inspect

import (
	"slices"
	"sort"
	"time"

	"github.com/ac0d3r/idevloupe/internal/locales"
)

type Status int

const (
	Unknown Status = iota // the data could not be read
	Pass
	Info
	Warn
	Fail
)

func (s Status) String() string {
	switch s {
	case Pass:
		return "pass"
	case Info:
		return "info"
	case Warn:
		return "warn"
	case Fail:
		return "fail"
	}
	return "unknown"
}

func (s Status) Label(lang string) string {
	m := locales.In(lang)
	switch s {
	case Pass:
		return m.StatusPass()
	case Info:
		return m.StatusInfo()
	case Warn:
		return m.StatusWarn()
	case Fail:
		return m.StatusFail()
	}
	return m.StatusUnknown()
}

type Check struct {
	Category      string
	Title         string
	Value         string
	Status        Status
	Detail        string
	Factory, Read Reading
	Short         string
}

func (c Check) Name() string {
	if c.Short != "" {
		return c.Short
	}
	return c.Title
}

type Reading struct {
	Value string
	Date  string
	// ID marks a value that identifies the unit, hidden in shared screenshots.
	ID bool
}

type Field struct {
	Label string
	Value string
}

type Section struct {
	Title  string
	Note   string
	Fields []Field
}

type Battery struct {
	Present                        bool
	DesignCapacity                 int64 // mAh
	MaxCapacity                    int64 // mAh, full charge capacity now
	HealthPercent                  float64
	SystemHealth                   int64 // the percent iOS shows; 0 if not reported
	CycleCount                     int64
	CurrentPercent                 int64
	TemperatureC                   float64
	VoltageMV                      int64
	AmperageMA                     int64
	IsCharging                     bool
	ExternalPower                  bool
	Serial                         string
	Maker                          string
	Made                           time.Time
	CurrentMAh                     int64
	HasCurrentMAh                  bool
	BootVoltageMV                  int64
	AtWarnLevel, AtCriticalLevel   bool
	HasWarnLevel, HasCriticalLevel bool
	HasCycleCount                  bool
	HasTemperature                 bool
	HasDesign                      bool
	HasMax                         bool
	HasCurrentLevel                bool
	GaugeCycleCount                int64
	HasGaugeCycles                 bool

	LogHealth     int64
	LogCycles     int64
	HasLogCycles  bool
	CyclesFromLog bool
	// BatteryHealthMetric = the gauge's ChemicalWeightedRa, from the gauge
	// (iOS 16) or the analytics log (iOS 17 on); 0 when unknown.
	ResistanceMetric int64
	LogDate          time.Time
	LogFile          string
	LogNote          string
}

// Health prefers iOS's maximum capacity (gauge, then log), else computes it
// from the capacities.
func (b *Battery) Health() float64 {
	switch {
	case b.SystemHealth > 0:
		return float64(b.SystemHealth)
	case b.LogHealth > 0:
		return float64(b.LogHealth)
	}
	return b.HealthPercent
}

type Summary struct {
	DeviceName  string
	Marketing   string
	ProductType string
	Capacity    string
	Color       string
	ColorName   string
	ColorHex    string
	IOS         string
	Region      string
	ModelNumber string
	Serial      string
	IMEI        string
}

type Counts struct {
	Pass, Info, Warn, Fail, Unknown int
}

type Report struct {
	Lang     string
	Time     time.Time
	UDID     string
	Summary  Summary
	Checks   []Check
	Sections []Section
	Battery  *Battery
	Apps     []AppEntry
	AppCount int
	Profiles []ProfileEntry
	Manual   []string
	Errors   []string
	Raw      []RawGroup
}

type AppEntry struct {
	BundleID, Name, Version, Type, Reason string
}

type ProfileEntry struct {
	Identifier, Name, Organization string
	MDM                            bool
}

type RawGroup struct {
	Source string
	Items  []Field
}

var idKeys = map[string]bool{
	"SerialNumber": true, "MLBSerialNumber": true, "UniqueChipID": true, "UniqueDeviceID": true,
	"InternationalMobileEquipmentIdentity": true, "InternationalMobileEquipmentIdentity2": true,
	"WiFiAddress": true, "BluetoothAddress": true, "EthernetAddress": true,
}

func (r *Report) Identifiers() []string {
	var out []string
	add := func(v string) {
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	add(r.UDID)
	add(r.Summary.Serial)
	add(r.Summary.IMEI)
	for _, g := range r.Raw {
		if g.Source != "lockdown" {
			continue
		}
		for _, f := range g.Items {
			if idKeys[f.Label] {
				add(f.Value)
			}
		}
	}
	for _, c := range r.Checks {
		for _, rd := range []Reading{c.Factory, c.Read} {
			if rd.ID {
				add(rd.Value)
			}
		}
	}
	return out
}

func (r *Report) Counts() Counts {
	var c Counts
	for _, ch := range r.Checks {
		switch ch.Status {
		case Pass:
			c.Pass++
		case Info:
			c.Info++
		case Warn:
			c.Warn++
		case Fail:
			c.Fail++
		default:
			c.Unknown++
		}
	}
	return c
}

// Ordered by what a buyer must know first.
var categoryOrder = []func(locales.Messages) string{
	locales.Messages.CategorySecurity,
	locales.Messages.CategoryParts,
	locales.Messages.CategoryBasic,
	locales.Messages.CategoryIdentity,
	locales.Messages.CategoryCellular,
	locales.Messages.CategoryBattery,
}

type CategoryStatus struct {
	Name   string
	Status Status
}

// Unknown ranks between Info and Warn.
var severity = map[Status]int{Pass: 0, Info: 1, Unknown: 2, Warn: 3, Fail: 4}

func Categories(lang string) []string {
	m := locales.In(lang)
	out := make([]string, len(categoryOrder))
	for i, name := range categoryOrder {
		out[i] = name(m)
	}
	return out
}

func (r *Report) CategoryStatuses() []CategoryStatus {
	worst := map[string]Status{}
	for _, c := range r.Checks {
		if w, ok := worst[c.Category]; !ok || severity[c.Status] > severity[w] {
			worst[c.Category] = c.Status
		}
	}
	var out []CategoryStatus
	for _, name := range Categories(r.Lang) {
		st, ok := worst[name]
		if !ok {
			st = Unknown
		}
		out = append(out, CategoryStatus{name, st})
	}
	return out
}

func (r *Report) Issues() []Check {
	m := locales.In(r.Lang)
	ranks := map[string]int{}
	for i, name := range categoryOrder {
		ranks[name(m)] = i
	}
	rank := func(c Check) int {
		if n, ok := ranks[c.Category]; ok {
			return n
		}
		return len(ranks)
	}
	var out []Check
	for _, st := range []Status{Fail, Warn} {
		var level []Check
		for _, c := range r.Checks {
			if c.Status == st {
				level = append(level, c)
			}
		}
		sort.SliceStable(level, func(i, j int) bool { return rank(level[i]) < rank(level[j]) })
		out = append(out, level...)
	}
	return out
}

func (c Check) Headline() string {
	if c.Value == "" {
		return c.Title
	}
	return c.Title + " " + c.Value
}

func (r *Report) Verdict() (string, Status) {
	c := r.Counts()
	m := locales.In(r.Lang)
	switch {
	case c.Fail > 0:
		return m.VerdictFail(), Fail
	case c.Warn > 0:
		return m.VerdictWarn(), Warn
	case c.Unknown > 0:
		return m.VerdictUnknown(), Info
	}
	return m.VerdictPass(), Pass
}
