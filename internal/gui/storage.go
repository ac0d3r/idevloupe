package gui

import (
	"github.com/egoist/mygo/ui"

	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/internal/locales"
)

// usage is one category of the storage bar.
type usage struct {
	label string
	bytes int64
	color ui.Color
}

// storageModal shows how the storage is used, as a bar and its legend,
// and what the storage controller says of itself.
func (a *App) storageModal(c *ui.Context, o *inspect.Overview) {
	s := o.Storage
	if s == nil {
		return
	}
	t := c.Theme()
	m := locales.In(o.Lang)
	parts := []usage{
		{m.StorageSystem(), s.System, ui.Hex("#8e8e93")},
		{m.StorageApps(), s.Apps, t.Accent},
		{m.StoragePhotos(), s.Photos, ui.Hex("#ff9f0a")},
		{m.StorageOther(), s.Other(), ui.Hex("#af52de")},
	}
	if !s.HasApps {
		parts = append(parts[:1], parts[2:]...)
	}
	free := usage{m.StorageFree(), s.Free, t.Text.Alpha(0.1)}
	ui.Modal(c, &a.storageOpen, func() {
		ui.Column(c).Width(760).Gap(14).Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, joinDot(m.StorageDetailsTitle(), o.Summary.Capacity)).FontSize(15).Bold()
				ui.Spacer(c)
				if ui.Button(c, m.ActionClose()).Clicked() {
					a.storageOpen = false
				}
			})
			if s.Total > 0 {
				card(c.Key("usage")).Padding(18, 20).Gap(14).Children(func() {
					storageBar(c, s.Total, parts)
					ui.Row(c).Gap(28).Wrap().Children(func() {
						for _, p := range append(parts, free) {
							legendItem(c, m, p)
						}
					})
					if !s.HasApps {
						muted(c, m.StorageAppsAfterInspect()).Wrap()
					}
				})
			}
			ui.Row(c).Gap(12).AlignItems(ui.Stretch).Children(func() {
				fieldsCard(c, "info", m.StorageInfoTitle(), s.Info, false)
				fieldsCard(c, "limits", m.StorageLimitsTitle(), s.Limits, true)
			})
		})
	})
}

// storageBar draws the categories end to end over the whole capacity; the
// rest of the track is the free space.
func storageBar(c *ui.Context, total int64, parts []usage) {
	t := c.Theme()
	ui.Box(c).Height(22).Draw(func(p *ui.Painter, r ui.Rect) {
		const radius = 11
		p.Fill(r, t.Text.Alpha(0.08), radius)
		p.Clip(r, radius, func() {
			x := r.X
			for _, u := range parts {
				w := float32(float64(u.bytes) / float64(total) * float64(r.W))
				if w <= 0 {
					continue
				}
				// A sliver stays visible, as 3uTools' bar keeps it.
				w = max(w, 2)
				p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, u.color, 0)
				x += w
			}
		})
	})
}

func legendItem(c *ui.Context, m locales.Messages, u usage) {
	ui.Column(c.Key(u.label)).Gap(2).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			ui.Box(c).Size(8, 8).Radius(4).Background(u.color)
			muted(c, u.label).FontSize(12).SingleLine()
		})
		ui.Text(c, bytesText(m, u.bytes)).FontSize(13).Margin(0, 0, 0, 14).SingleLine()
	})
}

// bytesText says a size in GB, or in MB under one GB.
func bytesText(m locales.Messages, n int64) string {
	if n < 1e9 {
		return m.ValueMB(float64(n) / 1e6)
	}
	return m.ValueGB(float64(n) / 1e9)
}

// fieldsCard lists fields under a title, label left and value right; mono
// sets the values in the monospaced font, for numbers to line up.
func fieldsCard(c *ui.Context, key, title string, fields []inspect.Field, mono bool) {
	if len(fields) == 0 {
		return
	}
	card(c.Key(key)).Grow(1).Basis(0).MinWidth(0).Padding(12, 18).Selectable().Children(func() {
		ui.Text(c, title).FontSize(13).Bold().Margin(0, 0, 4, 0)
		for _, f := range fields {
			ui.Row(c.Key(f.Label)).Gap(12).Padding(5, 0).Children(func() {
				muted(c, f.Label).FontSize(13).SingleLine()
				ui.Spacer(c)
				v := ui.Text(c, f.Value).FontSize(13).TextAlign(ui.End).SingleLine()
				if mono {
					v.Font(monoFont)
				}
			})
		}
	})
}
