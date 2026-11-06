package inspect

import (
	"math"
	"time"

	"github.com/ac0d3r/idevloupe/pkg/plistx"
)

// battery reads the battery, adds what the analytics log knows of it,
// and checks it.
func (a *analysis) battery() {
	r := a.r
	r.Battery = parseBattery(a.raw)
	if b := r.Battery; b != nil {
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
	// A cell charging well above its design capacity is suspicious whatever
	// the health shown. Only a health computed from that capacity is itself
	// meaningless then; one iOS reports stands.
	overCap := b.HealthPercent > overCapacityPercent
	computed := b.SystemHealth == 0 && b.LogHealth == 0
	pct := int(math.Round(health))
	switch {
	case health <= 0:
		a.add(cat, title, "", Unknown, m.CapacityNotRead())
	case overCap && computed:
		a.add(cat, title, m.HealthAboveDesign(pct), Warn, m.JoinSentences(source, m.HealthAboveDesignDetail()))
	case health >= 85:
		a.add(cat, title, m.ValuePercent(pct), Pass, source)
	case health >= 80:
		a.add(cat, title, m.ValuePercent(pct), Info, m.JoinSentences(source, m.HealthNear80()))
	default:
		a.add(cat, title, m.ValuePercent(pct), Warn, m.JoinSentences(source, m.HealthBelow80()))
	}
	if b.HasCycleCount && b.HasGaugeCycles && abs64(b.CycleCount-b.GaugeCycleCount) > 2 {
		a.add(cat, m.CheckBatteryConsistency(), m.ValueInconsistent(), Warn, m.BatteryCyclesDisagree(int(b.CycleCount), int(b.GaugeCycleCount)))
	}
	if b.HasCycleCount {
		from := ""
		if b.CyclesFromLog {
			from = m.CyclesFromLog(b.LogDate.Format("2006-01-02"))
		}
		val := m.ValueCycles(int(b.CycleCount))
		switch {
		case b.CycleCount < 500:
			a.add(cat, m.CheckCycles(), val, Pass, from)
		case b.CycleCount < 1000:
			a.add(cat, m.CheckCycles(), val, Info, joinWith(m.JoinSentences, m.CyclesMany(), from))
		default:
			a.add(cat, m.CheckCycles(), val, Warn, joinWith(m.JoinSentences, m.CyclesVeryMany(), from))
		}
	} else {
		a.add(cat, m.CheckCycles(), "", Unknown, "")
	}
	a.batterySource(b, overCap)
	if b.HasTemperature {
		st, d := Pass, ""
		if b.TemperatureC >= 45 {
			st, d = Warn, m.BatteryWarm()
		}
		a.add(cat, m.CheckBatteryTemperature(), m.ValueCelsius(b.TemperatureC), st, d)
	}
}

// overCapacityPercent is how far above its design capacity a battery may
// charge before it is suspicious. Set from samples: a genuine iPhone 16 cell
// logged a peak full charge (MaximumFCC) of 105.05% of design, an
// aftermarket iPhone 12 cell charges to 108.4%. Revisit as samples grow.
const overCapacityPercent = 107

// batterySource weighs the signs of a replaced or non-genuine battery.
// iOS's own "unknown part" verdict is not readable over lockdown, so this
// relies on what the gas gauge reports.
func (a *analysis) batterySource(b *Battery, overCap bool) {
	m, mfg, now := a.m, a.mfg, a.now
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
		a.add(cat, title, m.BatteryNonGenuine(), Fail, detail)
	case len(strong) == 1:
		a.add(cat, title, m.BatteryReplaced(), Warn, detail)
	case !weighed:
		a.add(cat, title, "", Unknown, m.BatteryOriginUnread())
	default:
		// No evidence either way is not proof of a genuine battery.
		a.add(cat, title, m.BatteryNoSigns(), Pass, m.BatteryNoSignsDetail())
	}
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
