package gui

import (
	"math"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/internal/locales"
)

// batteryModal shows all the gas gauge tells: the charge now, the health,
// and the readings, label left and value right.
func (a *App) batteryModal(c *ui.Context, o *inspect.Overview) {
	b := o.Battery
	if b == nil {
		return
	}
	m := locales.In(o.Lang)
	row := func(label, value string) { kvRow(c, label, value) }
	volts := func(mv int64) string {
		if mv <= 0 {
			return ""
		}
		return m.ValueVolts(float64(mv) / 1000)
	}
	ui.Modal(c, &a.batteryOpen, func() {
		ui.Column(c).Width(720).Gap(12).Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, m.BatteryDetailsTitle()).FontSize(15).Bold()
				ui.Spacer(c)
				if ui.Button(c, m.ActionClose()).Clicked() {
					a.batteryOpen = false
				}
			})
			ui.Row(c).Gap(12).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c).Width(310).Gap(12).Children(func() {
					card(c.Key("charge")).Padding(16, 18).Gap(8).Children(func() {
						batteryGlyph(c, b, 84)
						ui.Column(c).Margin(8, 0, 0, 0).Children(func() {
							chargeRows(c, m, b, true)
						})
					})
					card(c.Key("health")).Grow(1).Padding(12, 18).Children(func() {
						healthRows(c, o.Lang, b)
					})
				})
				card(c.Key("readings")).Grow(1).MinWidth(0).Padding(10, 18).Selectable().Children(func() {
					made := ""
					if !b.Made.IsZero() {
						made = b.Made.Format("2006-01-02")
					}
					yes := func(v, ok bool) string {
						if !ok {
							return ""
						}
						if v {
							return m.ValueYes()
						}
						return m.ValueNo()
					}
					row(m.FieldBatteryMaker(), b.Maker)
					row(m.FieldManufactured(), made)
					row(m.FieldBatterySerial(), b.Serial)
					row(m.FieldDesignCapacity(), opt(b.HasDesign, m.ValueMAh, b.DesignCapacity))
					row(m.FieldCurrentCapacity(), opt(b.HasCurrentMAh, m.ValueMAh, b.CurrentMAh))
					row(m.FieldFullChargeCapacity(), opt(b.HasMax, m.ValueMAh, b.MaxCapacity))
					row(m.FieldVoltage(), volts(b.VoltageMV))
					row(m.FieldBootVoltage(), volts(b.BootVoltageMV))
					row(m.StatCurrent(), opt(b.AmperageMA != 0, m.ValueMA, b.AmperageMA))
					row(m.FieldAtWarnLevel(), yes(b.AtWarnLevel, b.HasWarnLevel))
					row(m.FieldAtCriticalLevel(), yes(b.AtCriticalLevel, b.HasCriticalLevel))
					if b.ResistanceMetric > 0 {
						row(m.FieldResistance(), strconv.FormatInt(b.ResistanceMetric, 10))
					}
					// From iOS's analytics log, which an inspection reads.
					logDate := ""
					if !b.LogDate.IsZero() {
						logDate = b.LogDate.Format("2006-01-02")
					}
					row(m.StatLogHealth(), opt(b.LogHealth > 0, m.ValuePercent, b.LogHealth))
					row(m.StatLogCycles(), opt(b.HasLogCycles, m.ValueCycles, b.LogCycles))
					row(m.StatLogDate(), logDate)
				})
			})
			muted(c, m.BatteryFootnote()).Wrap()
			if b.LogNote != "" {
				muted(c, m.BatteryLogNote(b.LogNote)).Wrap()
			}
		})
	})
}

// kvRow is a label left and its value right; nothing when value is "".
func kvRow(c *ui.Context, label, value string) {
	if value == "" {
		return
	}
	ui.Row(c.Key(label)).Gap(12).Padding(6, 0).Children(func() {
		muted(c, label).FontSize(13).SingleLine()
		ui.Spacer(c)
		ui.Text(c, value).FontSize(13).TextAlign(ui.End).SingleLine()
	})
}

// opt says n with say when ok, else "".
func opt(ok bool, say func(int) string, n int64) string {
	if !ok {
		return ""
	}
	return say(int(n))
}

// chargeRows says how the battery is charging and its level, and its
// temperature when withTemp.
func chargeRows(c *ui.Context, m locales.Messages, b *inspect.Battery, withTemp bool) {
	kvRow(c, m.StatCharging(), chargingText(m, b))
	kvRow(c, m.FieldChargeLevel(), opt(b.HasCurrentLevel, m.ValuePercent, b.CurrentPercent))
	if withTemp && b.HasTemperature {
		kvRow(c, m.StatTemperature(), m.ValueCelsius(b.TemperatureC))
	}
}

// healthRows shows the health, the cycles and the rating, with a ring of
// the health in the rating's color.
func healthRows(c *ui.Context, lang string, b *inspect.Battery) {
	t := c.Theme()
	m := locales.In(lang)
	h := b.Health()
	rating, st := b.Rating(lang)
	ui.Row(c).Gap(16).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			kvRow(c, m.CheckBatteryHealth(), opt(h > 0, m.ValuePercent, int64(math.Round(h))))
			kvRow(c, m.CheckCycles(), opt(b.HasCycleCount, m.ValueCycles, b.CycleCount))
			if rating != "" {
				ui.Row(c).Gap(12).Padding(6, 0).Children(func() {
					muted(c, m.FieldHealthRating()).FontSize(13).SingleLine()
					ui.Spacer(c)
					ui.Text(c, rating).FontSize(13).FontWeight(600).TextColor(statusColor(t, st)).SingleLine()
				})
			}
		})
		if h > 0 {
			ring(c, h/100, statusColor(t, st), m.ValuePercent(int(math.Round(h))))
		}
	})
}

// chargingText says how the battery is charging, with the power into it
// while it charges.
func chargingText(m locales.Messages, b *inspect.Battery) string {
	state := m.ChargingNot()
	switch {
	case b.IsCharging:
		state = m.ChargingNow()
	case b.ExternalPower:
		state = m.ChargingPluggedIn()
	}
	if w := chargeWatts(b); w > 0 {
		state = m.ChargingWatts(state, w)
	}
	return state
}

// chargeWatts is the power into the battery while it charges, else 0.
func chargeWatts(b *inspect.Battery) float64 {
	if !b.IsCharging {
		return 0
	}
	return max(float64(b.VoltageMV)*float64(b.AmperageMA)/1e6, 0)
}

// batteryGlyph draws a battery filled to its charge level, in the warning
// color at 20% or below.
func batteryGlyph(c *ui.Context, b *inspect.Battery, height float32) {
	t := c.Theme()
	level := -1.0
	if b.HasCurrentLevel {
		level = float64(b.CurrentPercent) / 100
	}
	from, to := t.Accent, t.Success
	if level >= 0 && level <= 0.2 {
		from, to = t.Warning, t.Warning
	}
	ui.Box(c).Height(height).Draw(func(p *ui.Painter, r ui.Rect) {
		nubW, nubH, gap := r.H/10.5, r.H*0.31, float32(3)
		radius := r.H / 6
		body := ui.Rect{X: r.X, Y: r.Y + 4, W: r.W - nubW - gap, H: r.H - 8}
		p.Fill(ui.Rect{X: body.X + body.W + gap, Y: r.Y + (r.H-nubH)/2, W: nubW, H: nubH}, t.Text.Alpha(0.18), 3)
		p.Fill(body, t.Text.Alpha(0.06), radius)
		p.Stroke(body, t.Text.Alpha(0.14), radius, 1.5)
		if level <= 0 {
			return
		}
		const pad = 5
		fill := ui.Rect{X: body.X + pad, Y: body.Y + pad, W: float32(math.Min(level, 1)) * (body.W - 2*pad), H: body.H - 2*pad}
		p.FillGradient(fill, ui.LinearGradient{From: from, To: to, Angle: 90, Oklab: true}, radius-pad)
		// A sheen on the upper half.
		p.Fill(ui.Rect{X: fill.X, Y: fill.Y, W: fill.W, H: fill.H * 0.4}, ui.Hex("#ffffff").Alpha(0.18), radius-pad)
	})
}
