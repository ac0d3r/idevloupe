package gui

import (
	"github.com/egoist/mygo/ui"

	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/internal/locales"
)

// icloudRow is the info card's iCloud row, beside activation and Find My:
// on or off, and a link to what it syncs, grayed out when iCloud is off or
// the device does not say.
func (a *App) icloudRow(c *ui.Context, m locales.Messages, ic *inspect.ICloud) {
	t := c.Theme()
	on := ic != nil && ic.On()
	ui.Row(c.Key("icloud")).Gap(12).Padding(8, 0).Children(func() {
		ui.Text(c, m.ICloudLabel()).FontSize(13).TextColor(t.TextMuted).SingleLine()
		ui.Spacer(c)
		if on {
			ui.Text(c, m.ValueOn()).FontSize(13).TextColor(t.Success).SingleLine()
		} else {
			ui.Text(c, m.ValueOff()).FontSize(13).TextColor(t.TextMuted).SingleLine()
		}
		if linkAction(c, m.ActionICloudDetails(), nil, on) {
			a.icloudOpen = true
		}
	})
}

// backupRows says whether iCloud backup is on and when it last ran.
func backupRows(c *ui.Context, m locales.Messages, ic *inspect.ICloud) {
	onOff(c, m, m.ICloudBackup(), ic.Backup, ic.HasBackup)
	if !ic.LastBackup.IsZero() {
		kvRow(c, m.ICloudLastBackup(), ic.LastBackup.Local().Format("2006-01-02 15:04"))
	}
}

// onOff is a row of a setting: on in the success color, off muted, and
// "not reported" when the device does not say.
func onOff(c *ui.Context, m locales.Messages, label string, on, known bool) {
	t := c.Theme()
	ui.Row(c.Key(label)).Gap(12).Padding(6, 0).Children(func() {
		muted(c, label).FontSize(13).SingleLine()
		ui.Spacer(c)
		switch {
		case !known:
			muted(c, m.ValueUnread()).FontSize(13).SingleLine()
		case on:
			ui.Text(c, m.ValueOn()).FontSize(13).FontWeight(600).TextColor(t.Success).SingleLine()
		default:
			muted(c, m.ValueOff()).FontSize(13).SingleLine()
		}
	})
}

// icloudModal lists the data classes the device syncs with iCloud, and the
// iCloud backup.
func (a *App) icloudModal(c *ui.Context, o *inspect.Overview) {
	ic := o.ICloud
	if ic == nil {
		return
	}
	m := locales.In(o.Lang)
	ui.Modal(c, &a.icloudOpen, func() {
		ui.Column(c).Width(460).Gap(12).Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, m.ICloudDetailsTitle()).FontSize(15).Bold()
				ui.Spacer(c)
				if ui.Button(c, m.ActionClose()).Clicked() {
					a.icloudOpen = false
				}
			})
			card(c.Key("sync")).Padding(12, 18).Children(func() {
				ui.Text(c, m.ICloudSyncTitle()).FontSize(13).Bold().Margin(0, 0, 4, 0)
				for _, sc := range ic.Classes {
					onOff(c, m, sc.Name, sc.On, sc.Known)
				}
				muted(c, m.ICloudSyncNote()).Wrap().Margin(4, 0, 0, 0)
			})
			card(c.Key("backup")).Padding(12, 18).Children(func() {
				backupRows(c, m, ic)
			})
		})
	})
}
