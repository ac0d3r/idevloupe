// Package inspect collects what a device reports about itself and turns it
// into an inspection report of checks.
//
// Accuracy rule: a check passes only on positive evidence. When the data a
// check needs could not be read, the check is Unknown, never Pass.
package inspect

import (
	"slices"
	"sort"
	"time"

	"github.com/ac0d3r/idevloupe/internal/locales"
)

// Status is the outcome of a check.
type Status int

const (
	Unknown Status = iota // the data could not be read
	Pass                  // normal
	Info                  // worth knowing, not a problem
	Warn                  // needs attention
	Fail                  // abnormal
)

// String is the status's code, the same in every language: "pass",
// "info", "warn", "fail" or "unknown".
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

// Label names the status in the language lang.
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

// Check is one item of the report.
type Check struct {
	Category string
	Title    string
	Value    string
	Status   Status
	Detail   string
	// Factory and Read, for a check that compares the unit with its
	// factory record, are the value recorded and the value read now.
	Factory, Read Reading
	// Short is the title where columns name the comparison, as the part
	// alone for "Front camera vs factory record"; "" when it is Title.
	Short string
}

// Name is the check's name in a table whose columns say what it compares.
func (c Check) Name() string {
	if c.Short != "" {
		return c.Short
	}
	return c.Title
}

// Reading is one value a comparison check sets side by side.
type Reading struct {
	Value string
	// Date is when the part was made, as its serial tells; "" if it does not.
	Date string
	// ID is whether the value names the unit, as its serial or a network
	// address does, to be hidden from a shared screenshot.
	ID bool
}

// Field is a labeled value of the device information.
type Field struct {
	Label string
	Value string
}

// Section is a group of fields.
type Section struct {
	Title string
	// Note, if any, says how to read the fields.
	Note   string
	Fields []Field
}

// Battery is the battery's state as the gas gauge reports it.
type Battery struct {
	Present        bool
	DesignCapacity int64   // mAh
	MaxCapacity    int64   // mAh, full charge capacity now
	HealthPercent  float64 // MaxCapacity / DesignCapacity
	SystemHealth   int64   // the percent iOS shows, when reported; 0 if not
	CycleCount     int64
	CurrentPercent int64
	TemperatureC   float64
	VoltageMV      int64
	AmperageMA     int64
	IsCharging     bool
	ExternalPower  bool
	Serial         string
	Maker          string    // the battery's maker by its serial, from the device table
	Made           time.Time // when the cell was made, by its serial; zero if it does not tell
	// CurrentMAh is the charge held now (AppleRawCurrentCapacity).
	CurrentMAh    int64
	HasCurrentMAh bool
	// BootVoltageMV is the voltage when the device last started; 0 if unread.
	BootVoltageMV int64
	// AtWarnLevel and AtCriticalLevel are the gauge's low-charge flags.
	AtWarnLevel, AtCriticalLevel   bool
	HasWarnLevel, HasCriticalLevel bool
	HasCycleCount                  bool
	HasTemperature                 bool
	HasDesign                      bool
	HasMax                         bool
	HasCurrentLevel                bool
	// GaugeCycleCount is the "All" diagnostics' gas gauge cycle count, a
	// second source to compare with.
	GaugeCycleCount int64
	HasGaugeCycles  bool

	// From iOS's analytics logs: the maximum capacity Settings shows, and
	// the cycle count when the log was written.
	LogHealth    int64
	LogCycles    int64
	HasLogCycles bool
	// CyclesFromLog means CycleCount is the log's, the gauge unread.
	CyclesFromLog bool
	// ResistanceMetric is BatteryHealthMetric, which equals the gauge's
	// ChemicalWeightedRa: the cell's weighted resistance, from the gauge
	// (iOS 16) or the analytics log (iOS 17 on). 0 when unknown.
	ResistanceMetric int64
	LogDate          time.Time
	LogFile          string
	LogNote          string // why there is no log value, when there is none
}

// Health is the health the report goes by: the maximum capacity iOS
// reports (the gas gauge's, else the analytics log's), else the one
// computed from the capacities.
func (b *Battery) Health() float64 {
	switch {
	case b.SystemHealth > 0:
		return float64(b.SystemHealth)
	case b.LogHealth > 0:
		return float64(b.LogHealth)
	}
	return b.HealthPercent
}

// Summary is the header of the report.
type Summary struct {
	DeviceName  string
	Marketing   string
	ProductType string
	Capacity    string
	Color       string // the back's color code, else the front's: "1", "#rrggbb"
	ColorName   string // the back's color named, when a sample settled its code
	ColorHex    string // "#rrggbb" when the color is given as one
	IOS         string
	Region      string
	ModelNumber string // part number with region, "MQ8X3CH/A"
	Serial      string
	IMEI        string
}

// Counts tallies checks by status.
type Counts struct {
	Pass, Info, Warn, Fail, Unknown int
}

// Report is the result of an inspection, in one language: Analyze makes
// it again from the raw data for another.
type Report struct {
	// Lang is the language the report is written in, a tag of
	// locales.Catalog's languages ("" for its source language).
	Lang     string
	Time     time.Time
	UDID     string
	Summary  Summary
	Checks   []Check
	Sections []Section
	Battery  *Battery
	Apps     []AppEntry // installed apps flagged by the inspection
	AppCount int
	Profiles []ProfileEntry
	// Manual lists what software cannot verify and must be checked by hand.
	Manual []string
	// Errors are the steps of the collection that failed, in the report's
	// language.
	Errors []string
	// Raw holds every value read, by source, for transparency.
	Raw []RawGroup
}

// AppEntry is an installed app named in the report.
type AppEntry struct {
	BundleID, Name, Version, Type, Reason string
}

// ProfileEntry is an installed configuration profile.
type ProfileEntry struct {
	Identifier, Name, Organization string
	MDM                            bool
}

// RawGroup is the raw values of one source.
type RawGroup struct {
	Source string
	Items  []Field
}

// idKeys are the lockdown values that name the unit.
var idKeys = map[string]bool{
	"SerialNumber": true, "MLBSerialNumber": true, "UniqueChipID": true, "UniqueDeviceID": true,
	"InternationalMobileEquipmentIdentity": true, "InternationalMobileEquipmentIdentity2": true,
	"WiFiAddress": true, "BluetoothAddress": true, "EthernetAddress": true,
}

// Identifiers are the values that name the unit: its serials, IMEIs,
// ECID, UDID and network addresses, as lockdown and the factory record
// give them. A shared screenshot hides them.
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

// Counts tallies the report's checks.
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

// categoryOrder orders the categories by what a buyer must know first: a
// lock or management, then parts, then the unit's identity, its radio and
// last its battery, which wears on every phone.
var categoryOrder = []func(locales.Messages) string{
	locales.Messages.CategorySecurity,
	locales.Messages.CategoryParts,
	locales.Messages.CategoryBasic,
	locales.Messages.CategoryIdentity,
	locales.Messages.CategoryCellular,
	locales.Messages.CategoryBattery,
}

// CategoryStatus is a category of the report and its worst status.
type CategoryStatus struct {
	Name   string
	Status Status
}

// severity orders the statuses from the best: what could not be read is
// worse than worth knowing, better than a warning.
var severity = map[Status]int{Pass: 0, Info: 1, Unknown: 2, Warn: 3, Fail: 4}

// Categories names the report's categories in lang, in categoryOrder.
func Categories(lang string) []string {
	m := locales.In(lang)
	out := make([]string, len(categoryOrder))
	for i, name := range categoryOrder {
		out[i] = name(m)
	}
	return out
}

// CategoryStatuses returns each category of Categories with the worst
// status of its checks; a category without checks is Unknown.
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

// Issues returns the failed and warned checks, worst first: failures before
// warnings, and within each by categoryOrder, then in report order.
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

// Headline names a check in a few words, as "Face ID self test Failed
// (code 8)".
func (c Check) Headline() string {
	if c.Value == "" {
		return c.Title
	}
	return c.Title + " " + c.Value
}

// Verdict is a one-line conclusion.
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
