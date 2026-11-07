package inspect

import (
	"math"
	"time"

	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/pkg/plistx"
)

// battery reads the battery, adds what the analytics log knows of it,
// and checks it.
func (a *analysis) battery() {
	r := a.r
	r.Battery = parseBattery(a.raw)
	if b := r.Battery; b != nil {
		b.identify(a.lang, a.now)
		if analytics := a.raw.Analytics; analytics != nil {
			b.LogFile, b.LogDate = analytics.File, analytics.Date
			if n, ok := analytics.Values["MaximumCapacityPercent"]; ok && n > 0 && n <= 150 {
				b.LogHealth = n
			}
			b.LogCycles, b.HasLogCycles = analytics.Values["CycleCount"]
			if n, ok := analytics.Values["BatteryHealthMetric"]; ok && b.ResistanceMetric == 0 {
				b.ResistanceMetric = n
			}
			if !b.HasCycleCount && b.HasLogCycles {
				// The gauge was not read: the log's count stands in.
				b.CycleCount, b.HasCycleCount, b.CyclesFromLog = b.LogCycles, true, true
			}
		} else if a.raw.AnalyticsErr != nil {
			b.LogNote = ErrorText(a.lang, a.raw.AnalyticsErr)
		}
	}
	a.batteryChecks()
}

// parseBattery reads the gas gauge: the IORegistry's battery entry, else
// the "All" diagnostics' gas gauge, else the charge level lockdown gives.
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
	b.CurrentMAh, b.HasCurrentMAh = num("AppleRawCurrentCapacity")
	b.BootVoltageMV, _ = num("BootVoltage")
	b.AtWarnLevel, b.HasWarnLevel = plistx.Bool(reg["AtWarnLevel"])
	b.AtCriticalLevel, b.HasCriticalLevel = plistx.Bool(reg["AtCriticalLevel"])
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

// identify fills in, from the serial, the cell's maker in the language
// lang and when it was made.
func (b *Battery) identify(lang string, now time.Time) {
	b.Maker, _ = Table.BatteryMaker(b.Serial, lang)
	b.Made, _ = PartDate(b.Serial, now)
}

// Rating grades the health against the 80% Apple designs the battery to
// keep through its rated cycles and below which it offers a service: good
// at 80% and above, worn below. "" and Unknown when the health is unread.
func (b *Battery) Rating(lang string) (string, Status) {
	m := locales.In(lang)
	switch h := b.Health(); {
	case h <= 0:
		return "", Unknown
	case h >= serviceHealth:
		return m.RatingGood(), Pass
	default:
		return m.RatingWorn(), Warn
	}
}

// serviceHealth is the maximum capacity, in percent, Apple designs an
// iPhone battery to keep through its rated cycles; below it iOS advises a
// service (support.apple.com/HT208387).
const serviceHealth = 80

// ratedCycles is the number of full charge cycles Apple rates an iPhone's
// battery to keep serviceHealth through: 1000 from the iPhone 15 models of
// 2023 on, 500 before, as Apple's battery service page says. 0 for a
// model not in the table.
func ratedCycles(model Model, known bool) int64 {
	switch {
	case !known:
		return 0
	case model.Year >= 2023:
		return 1000
	}
	return 500
}

// batteryChecks checks the battery's health, cycles, origin and
// temperature.
func (a *analysis) batteryChecks() {
	m, now := a.m, a.now
	b := a.r.Battery
	cat, title := m.CategoryBattery(), m.CheckBatteryHealth()
	if b == nil {
		a.add(cat, title, "", Unknown, m.BatteryUnread())
		return
	}
	health := b.Health()
	source := m.HealthFromCapacity(int(b.MaxCapacity), int(b.DesignCapacity))
	calc := ""
	if b.HealthPercent > 0 {
		calc = m.HealthComputed(b.HealthPercent, int(b.MaxCapacity), int(b.DesignCapacity))
	}
	switch {
	case b.SystemHealth > 0:
		source = joinWith(m.JoinClauses, m.HealthFromGauge(), calc)
	case b.LogHealth > 0:
		source = joinWith(m.JoinClauses, m.HealthFromLog(b.LogDate.Format("2006-01-02")), calc)
		if age := now.Sub(b.LogDate); age > 14*24*time.Hour {
			source = m.JoinSentences(source, m.HealthLogOld(int(age.Hours()/24)))
		}
	case b.LogNote != "":
		source = m.WithNote(source, b.LogNote)
	}
	pct := int(math.Round(health))
	switch {
	case health <= 0:
		a.add(cat, title, "", Unknown, m.CapacityNotRead())
	case health >= serviceHealth:
		a.add(cat, title, m.ValuePercent(pct), Pass, source)
	default:
		a.add(cat, title, m.ValuePercent(pct), Warn, m.JoinSentences(source, m.HealthBelow80()))
	}
	// Two readings of one gauge's count: any difference is reported.
	if b.HasCycleCount && b.HasGaugeCycles && b.CycleCount != b.GaugeCycleCount {
		a.add(cat, m.CheckBatteryConsistency(), m.ValueInconsistent(), Info, m.BatteryCyclesDisagree(int(b.CycleCount), int(b.GaugeCycleCount)))
	}
	if b.HasCycleCount {
		from := ""
		if b.CyclesFromLog {
			from = m.CyclesFromLog(b.LogDate.Format("2006-01-02"))
		}
		val := m.ValueCycles(int(b.CycleCount))
		// Past the cycles Apple rates the battery for, it is known only to
		// have outlived its rating; the health says how it wore.
		if rated := ratedCycles(a.model, a.known); rated > 0 && b.CycleCount > rated {
			a.add(cat, m.CheckCycles(), val, Info, joinWith(m.JoinSentences, m.CyclesPastRated(int(rated)), from))
		} else {
			a.add(cat, m.CheckCycles(), val, Pass, from)
		}
	} else {
		a.add(cat, m.CheckCycles(), "", Unknown, "")
	}
	a.batterySource(b)
}

// batterySource looks for evidence that the battery was replaced: a
// cycle count lower than one iOS logged before, cycles only growing, or a
// cell made after the device. Whether a battery is genuine is iOS's
// verdict in Parts and Service History, which lockdown does not give, so
// none is made here.
func (a *analysis) batterySource(b *Battery) {
	m := a.m
	var ev []string
	if b.HasLogCycles && b.HasCycleCount && !b.CyclesFromLog && b.LogCycles > b.CycleCount {
		ev = append(ev, m.BatteryLogCyclesHigher(b.LogDate.Format("2006-01-02"), int(b.LogCycles), int(b.CycleCount)))
	}
	if newer, _ := a.partsNewer(map[string]string{"battery": b.Serial}); len(newer) > 0 {
		device, _ := a.deviceMade()
		ev = append(ev, m.BatteryNewer(newer[0].made.Format("2006-01-02"), device.Format("2006-01-02")))
	}
	cat, title := m.CategoryBattery(), m.CheckBatteryOrigin()
	switch {
	case len(ev) > 0:
		a.add(cat, title, m.BatteryReplaced(), Warn, joinWith(m.JoinClauses, ev...))
	case !b.HasCycleCount && b.Serial == "":
		a.add(cat, title, "", Unknown, m.BatteryOriginUnread())
	default:
		// No evidence either way is not proof of a genuine battery.
		a.add(cat, title, m.BatteryNoSigns(), Pass, m.BatteryNoSignsDetail())
	}
}
