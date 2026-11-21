package gui

import (
	"math"

	"github.com/egoist/mygo/ui"

	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/internal/locales"
)

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

func (a *App) infoCard(c *ui.Context, o *inspect.Overview) {
	t := c.Theme()
	m := locales.In(o.Lang)
	card(c.Key("info")).Grow(1).MinWidth(0).Padding(8, 18, 16, 18).Children(func() {
		ui.Column(c).Selectable().Children(func() {
			// The same rows for every device, "--" for what it lacks, so the card keeps its shape.
			for _, f := range o.Fields {
				ui.Row(c.Key(f.Label)).Gap(12).Padding(8, 0).AlignItems(ui.Start).Children(func() {
					ui.Text(c, f.Label).FontSize(13).TextColor(t.TextMuted).SingleLine()
					ui.Spacer(c)
					if f.Value == "" {
						muted(c, "--").FontSize(13).SingleLine()
						return
					}
					v := ui.Text(c, f.Value).FontSize(13).TextAlign(ui.End).MinWidth(0).Wrap()
					if col, ok := valueColor(t, m, f); ok {
						v.TextColor(col)
					}
				})
				if f.Label == m.CheckFindMy() {
					a.statusRows(c, m, o)
				}
			}
		})
		ui.Spacer(c)
		ui.Row(c).Gap(8).Margin(8, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			if ui.Button(c, m.ActionDetails()).Clicked() {
				a.detailsOpen = true
			}
		})
	})
}

func (a *App) statusRows(c *ui.Context, m locales.Messages, o *inspect.Overview) {
	a.icloudRow(c, m, o.ICloud)
	a.warrantyRow(c, m, o.Summary.Serial)
}

// Warranty status is only on Apple's captcha-protected page: copy the serial
// for the person to paste there.
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

func valueColor(t *ui.Theme, m locales.Messages, f inspect.Field) (ui.Color, bool) {
	if f.Label == m.CheckFindMy() && f.Value == m.FindMyOn() {
		return t.Warning, true
	}
	switch f.Value {
	case m.ValueOn():
		return t.Success, true
	case m.ActivationActivated():
		return t.Accent, true
	case m.ValueOff():
		return t.TextMuted, true
	}
	return ui.Color{}, false
}

const warrantyURL = "https://checkcoverage.apple.com/"

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

func (a *App) batteryCard(c *ui.Context, o *inspect.Overview) {
	m := locales.In(o.Lang)
	b := o.Battery
	card(c.Key("battery")).Padding(14, 16).Gap(6).Children(func() {
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
		ui.Row(c).Padding(4, 0).Children(func() {
			storageAmount(c, m.StorageUsed(), m.ValueWithPercent(bytesText(m, total-free), int(math.Round(used*100))), t.Accent)
			ui.Spacer(c)
			storageAmount(c, m.StorageFree(), bytesText(m, free), t.Text.Alpha(0.18))
		})
	})
}

func storageAmount(c *ui.Context, label, value string, col ui.Color) {
	ui.Row(c.Key(label)).Gap(6).Tooltip(label).Label(label).Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(col)
		ui.Text(c, value).FontSize(13).SingleLine()
	})
}

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
		ui.Spacer(c)
		if r == nil {
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

func (a *App) detailsModal(c *ui.Context, o *inspect.Overview) {
	m := locales.In(o.Lang)
	w, h := c.Size()
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
