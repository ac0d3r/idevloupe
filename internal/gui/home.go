package gui

import (
	"math"

	"github.com/egoist/mygo/ui"

	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/internal/locales"
)

// overviewView is the device's home: what it says about itself, with no
// verdict, and the inspection, run when asked for, summed up by category.
func (a *App) overviewView(c *ui.Context, d *devState) {
	o := d.overviewIn(c.Locale())
	ui.Column(c).Fill().Children(func() {
		a.homeBar(c, d, o)
		ui.Divider(c)
		ui.Scroll(c).Grow(1).Padding(20, 28, 28, 28).Children(func() {
			ui.Row(c).Gap(16).AlignItems(ui.Stretch).Children(func() {
				a.infoCard(c, o)
				ui.Column(c).Width(290).Gap(16).Children(func() {
					a.batteryCard(c, o)
					a.storageCard(c, o)
					a.inspectCard(c, d)
				})
			})
		})
		a.detailsModal(c, o)
		a.batteryModal(c, o)
		a.storageModal(c, o)
		a.icloudModal(c, o)
	})
}

// homeBar names the device; its model, capacity and charge are in the
// sidebar and the cards below.
func (a *App) homeBar(c *ui.Context, d *devState, o *inspect.Overview) {
	ui.Row(c).Padding(14, 28).Children(func() {
		ui.Text(c, d.title(o.Lang)).FontSize(15).Bold().SingleLine()
	})
}

func joinDot(parts ...string) string {
	out := ""
	for _, p := range parts {
		switch {
		case p == "":
		case out == "":
			out = p
		default:
			out += " · " + p
		}
	}
	return out
}

// infoCard lists the facts a buyer looks for first, label left and value
// right, and opens the device details.
func (a *App) infoCard(c *ui.Context, o *inspect.Overview) {
	t := c.Theme()
	m := locales.In(o.Lang)
	card(c.Key("info")).Grow(1).MinWidth(0).Padding(8, 18, 16, 18).Children(func() {
		ui.Column(c).Selectable().Children(func() {
			// Every device shows the same rows, "--" for what it lacks or
			// does not say, so the card keeps its shape; iCloud and the
			// warranty follow the activation lock.
			for _, f := range o.Fields {
				ui.Row(c.Key(f.Label)).Gap(12).Padding(8, 0).AlignItems(ui.Start).Children(func() {
					ui.Text(c, f.Label).FontSize(13).TextColor(t.TextMuted).SingleLine()
					ui.Spacer(c)
					if f.Value == "" {
						muted(c, "--").FontSize(13).SingleLine()
						return
					}
					v := ui.Text(c, f.Value).FontSize(13).TextAlign(ui.End).MinWidth(0).Wrap()
					if col, ok := valueColor(t, m, f.Value); ok {
						v.TextColor(col)
					}
				})
				if f.Label == m.CheckFindMy() {
					a.statusRows(c, m, o)
				}
			}
		})
		// The buttons sit at the bottom, level with the inspect card's.
		ui.Spacer(c)
		ui.Row(c).Gap(8).Margin(8, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			if ui.Button(c, m.ActionDetails()).Clicked() {
				a.detailsOpen = true
			}
		})
	})
}

// statusRows are the info card's rows that act: iCloud, and the warranty.
func (a *App) statusRows(c *ui.Context, m locales.Messages, o *inspect.Overview) {
	a.icloudRow(c, m, o.ICloud)
	a.warrantyRow(c, m, o.Summary.Serial)
}

// warrantyRow looks the warranty up online. It is known only to Apple,
// whose page asks for a captcha: copy the serial and let the person paste
// it there. Grayed out with no serial to look up.
func (a *App) warrantyRow(c *ui.Context, m locales.Messages, serial string) {
	t := c.Theme()
	ui.Row(c.Key("warranty")).Gap(12).Padding(8, 0).Children(func() {
		ui.Text(c, m.FieldWarranty()).FontSize(13).TextColor(t.TextMuted).SingleLine()
		ui.Spacer(c)
		if linkAction(c, m.ActionCheckWarranty(), icExternal, serial != "") {
			c.WriteClipboard(serial)
			c.OpenURL(warrantyURL)
			a.toast = m.ToastWarrantySerialCopied()
		}
	})
}

// linkAction is an action set as a link: text in the accent color, an
// icon after it if any, true when clicked; muted and inert when not
// enabled.
func linkAction(c *ui.Context, label string, icon *ui.SVG, enabled bool) bool {
	t := c.Theme()
	col := t.TextMuted.Alpha(0.6)
	link := ui.Row(c.Key(label)).Gap(3).Label(label).Disabled(!enabled)
	if enabled {
		link.Cursor(ui.CursorPointer)
		col = t.Accent
		if link.Hovered() {
			col = t.Accent.Alpha(0.75)
		}
	}
	link.Children(func() {
		ui.Text(c, label).FontSize(13).TextColor(col).SingleLine()
		if icon != nil {
			ui.Icon(c, icon).Size(12, 12).TextColor(col)
		}
	})
	return enabled && link.Clicked()
}

// valueColor colors a state in the info card: on in green, off in gray,
// activated in blue; ok is false for any other value, which keeps the text color.
func valueColor(t *ui.Theme, m locales.Messages, value string) (ui.Color, bool) {
	switch value {
	case m.ValueOn(), m.FindMyOn():
		return t.Success, true
	case m.ActivationActivated():
		return t.Accent, true
	case m.ValueOff():
		return t.TextMuted, true
	}
	return ui.Color{}, false
}

// warrantyURL is Apple's coverage check, which takes a serial number.
const warrantyURL = "https://checkcoverage.apple.com/"

// ring draws frac of a circle from the top, clockwise, with label in it.
func ring(c *ui.Context, frac float64, col ui.Color, label string) {
	t := c.Theme()
	const size, width = 64, 6
	ui.Box(c).Size(size, size).Center().Label(label).Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy, rad := r.X+r.W/2, r.Y+r.H/2, r.W/2-width/2-1
		var track ui.Path
		track.Circle(cx, cy, rad)
		p.StrokePath(&track, width, t.Text.Alpha(0.08))
		if frac <= 0 {
			return
		}
		// A circle from the top, clockwise: four quarters of cubics.
		k := rad * 0.5522847498
		var arc ui.Path
		arc.MoveTo(cx, cy-rad).
			CubeTo(cx+k, cy-rad, cx+rad, cy-k, cx+rad, cy).
			CubeTo(cx+rad, cy+k, cx+k, cy+rad, cx, cy+rad).
			CubeTo(cx-k, cy+rad, cx-rad, cy+k, cx-rad, cy).
			CubeTo(cx-rad, cy-k, cx-k, cy-rad, cx, cy-rad).
			Trim(0, float32(min(frac, 1)))
		p.StrokePath(&arc, width, col)
	}).Children(func() {
		ui.Text(c, label).FontSize(13).Bold()
	})
}

// batteryCard shows the battery: its charge level, and the power into it
// while it charges, above; its health and cycles as the gauge reports
// them below, judging nothing: the inspection weighs them. Its button opens
// the battery details.
func (a *App) batteryCard(c *ui.Context, o *inspect.Overview) {
	m := locales.In(o.Lang)
	b := o.Battery
	card(c.Key("battery")).Padding(14, 16).Gap(6).Children(func() {
		// The title, then the charge level and the power while charging:
		// the badge says what it is, with no label.
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, m.BatteryCardTitle()).FontSize(13).Bold().SingleLine()
			if b != nil && b.HasCurrentLevel {
				levelBadge(c, m, b)
				if w := chargeWatts(b); w > 0 {
					muted(c, m.ValueWatts(w)).FontSize(12).SingleLine()
				}
			}
			ui.Spacer(c)
			if b != nil && ui.Button(c, m.ActionBatteryDetails()).Clicked() {
				a.batteryOpen = true
			}
		})
		if b == nil {
			muted(c, m.BatteryUnread()).Wrap()
			return
		}
		ui.Column(c).Children(func() {
			h := b.Health()
			iconRow(c, icHeart, m.CheckBatteryHealth(), opt(h > 0, m.ValuePercent, int64(math.Round(h))))
			iconRow(c, icCycle, m.CheckCycles(), opt(b.HasCycleCount, m.ValueCycles, b.CycleCount))
			if h <= 0 && !b.HasCycleCount {
				muted(c, m.BatteryCardNoHealth())
			}
		})
	})
}

// iconRow is kvRow with an icon before the label; nothing when value is "".
func iconRow(c *ui.Context, icon *ui.SVG, label, value string) {
	if value == "" {
		return
	}
	t := c.Theme()
	ui.Row(c.Key(label)).Gap(8).Padding(6, 0).Children(func() {
		ui.Icon(c, icon).Size(15, 15).TextColor(t.TextMuted)
		muted(c, label).FontSize(13).SingleLine()
		ui.Spacer(c)
		ui.Text(c, value).FontSize(13).TextAlign(ui.End).SingleLine()
	})
}

// levelBadge shows the charge level on green, on the warning color at 20%
// or below, with a bolt while the battery charges.
func levelBadge(c *ui.Context, m locales.Messages, b *inspect.Battery) {
	t := c.Theme()
	col := t.Success
	if b.CurrentPercent <= 20 {
		col = t.Warning
	}
	white := ui.Hex("#ffffff")
	ui.Row(c).Gap(2).Padding(1, 6).Radius(4).Background(col).Children(func() {
		if b.IsCharging {
			ui.Icon(c, icBolt).Size(11, 11).TextColor(white)
		}
		ui.Text(c, m.ValuePercent(int(b.CurrentPercent))).FontSize(12).Bold().TextColor(white).SingleLine()
	})
}

// storageCard shows how much of the storage is used: a bar of the used
// share over the used and the free space.
func (a *App) storageCard(c *ui.Context, o *inspect.Overview) {
	t := c.Theme()
	m := locales.In(o.Lang)
	total, free := o.StorageTotal, o.StorageFree
	if total <= 0 {
		return
	}
	card(c.Key("storage")).Padding(14, 16).Gap(6).Children(func() {
		ui.Row(c).Children(func() {
			ui.Text(c, joinDot(m.CheckStorage(), o.Summary.Capacity)).FontSize(13).Bold().SingleLine()
			ui.Spacer(c)
			if o.Storage != nil && ui.Button(c, m.ActionStorageDetails()).Clicked() {
				a.storageOpen = true
			}
		})
		if free <= 0 || free > total {
			return
		}
		used := float64(total-free) / float64(total)
		ui.Box(c).Height(6).Margin(6, 0, 2, 0).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, t.Text.Alpha(0.08), 3)
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: float32(used) * r.W, H: r.H}, t.Accent, 3)
		})
		// The used and the free space, each by a dot of its color in the
		// bar; the words are its tooltip.
		ui.Row(c).Padding(4, 0).Children(func() {
			storageAmount(c, m.StorageUsed(), m.ValueWithPercent(bytesText(m, total-free), int(math.Round(used*100))), t.Accent)
			ui.Spacer(c)
			storageAmount(c, m.StorageFree(), bytesText(m, free), t.Text.Alpha(0.18))
		})
	})
}

// storageAmount is an amount of space after a dot of its color in the
// storage bar, named by label for the tooltip and screen readers.
func storageAmount(c *ui.Context, label, value string, col ui.Color) {
	ui.Row(c.Key(label)).Gap(6).Tooltip(label).Label(label).Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(col)
		ui.Text(c, value).FontSize(13).SingleLine()
	})
}

// inspectCard sums the inspection up by category: "--" before it runs,
// each category's worst status after.
func (a *App) inspectCard(c *ui.Context, d *devState) {
	t := c.Theme()
	lang := c.Locale()
	m := locales.In(lang)
	r := d.reportIn(lang)
	card(c.Key("inspect")).Grow(1).Padding(14, 16).Gap(10).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.Icon(c, icLoupe).TextColor(t.Accent)
			ui.Text(c, m.InspectCardTitle()).FontSize(13).Bold()
			if r != nil {
				ui.Spacer(c)
				verdict, st := r.Verdict()
				ui.Text(c, verdict).FontSize(12).FontWeight(600).TextColor(statusColor(t, st)).SingleLine()
			}
		})
		// The categories two a row, each with its worst status as an icon
		// ("--" before the inspection), to keep the card short.
		cats := inspect.Categories(lang)
		statuses := map[string]inspect.Status{}
		if r != nil {
			for _, cs := range r.CategoryStatuses() {
				statuses[cs.Name] = cs.Status
			}
		}
		ui.Grid(c).Columns(2).GapX(20).GapY(2).Children(func() {
			for _, name := range cats {
				ui.Row(c.Key(name)).Padding(5, 0).Gap(6).Children(func() {
					ui.Text(c, name).FontSize(13).SingleLine()
					ui.Spacer(c)
					if r == nil {
						muted(c, "--").FontSize(13)
						return
					}
					st := statuses[name]
					ui.Icon(c, statusIcon(st)).Size(15, 15).TextColor(statusColor(t, st)).Tooltip(st.Label(lang)).Label(st.Label(lang))
				})
			}
		})
		// The actions sit at the bottom, level with the info card's.
		ui.Spacer(c)
		if r == nil {
			// What an inspection reads is the button's tooltip.
			if ui.PrimaryButton(c, "").Tooltip(m.OverviewHint()).Children(func() {
				ui.Icon(c, icLoupe)
				ui.Text(c, m.ActionInspect()).SingleLine()
			}).Clicked() {
				a.inspectDevice(d)
			}
			return
		}
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c, m.ActionViewReport()).Grow(1).Clicked() {
				d.phase = phaseDone
			}
			if ui.Button(c, "").Tooltip(m.ActionInspectAgain()).Children(func() {
				ui.Icon(c, icRefresh)
			}).Label(m.ActionInspectAgain()).Clicked() {
				a.inspectDevice(d)
			}
		})
	})
}

// detailsModal shows all the device information, as tall as it is so
// that one screenshot takes it all; it scrolls only in a window too short
// for it.
func (a *App) detailsModal(c *ui.Context, o *inspect.Overview) {
	m := locales.In(o.Lang)
	w, h := c.Size()
	// The modal's own padding and its margin to the window.
	const inset = 80
	width := min(w-inset, 1280)
	columns := 3
	if width < 1000 {
		columns = 2
	}
	ui.Modal(c, &a.detailsOpen, func() {
		ui.Column(c).Width(width).MaxHeight(h - inset).Gap(12).Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, m.ActionDetails()).FontSize(15).Bold()
				ui.Spacer(c)
				if ui.Button(c, m.ActionClose()).Clicked() {
					a.detailsOpen = false
				}
			})
			ui.Scroll(c).Grow(1).Children(func() {
				sectionsGrid(c, o.Sections, columns)
			})
		})
	})
}
