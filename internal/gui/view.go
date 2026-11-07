package gui

import (
	"runtime"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/internal/locales"
)

// languages are the languages the window offers, each named in itself.
var languages = []string{"zh-Hans", "en", "de"}

var monoFont = func() string {
	switch runtime.GOOS {
	case "darwin":
		return "Menlo"
	case "windows":
		return "Consolas"
	}
	return "monospace"
}()

var infoBlue = ui.Hex("#3b82f6")

func statusColor(t *ui.Theme, s inspect.Status) ui.Color {
	switch s {
	case inspect.Pass:
		return t.Success
	case inspect.Info:
		return infoBlue
	case inspect.Warn:
		return t.Warning
	case inspect.Fail:
		return t.Danger
	}
	return t.TextMuted
}

func statusIcon(s inspect.Status) *ui.SVG {
	switch s {
	case inspect.Pass:
		return icCheck
	case inspect.Info:
		return icInfo
	case inspect.Warn:
		return icAlert
	case inspect.Fail:
		return icX
	}
	return icHelp
}

// sidebarBg is a tone apart from the window background.
func sidebarBg(t *ui.Theme) ui.Color { return t.Background.Mix(t.Text, 0.035) }

func card(c *ui.Context) ui.Element {
	t := c.Theme()
	return ui.Column(c).Radius(10).Background(t.Background).Border(1, t.Border).
		Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.04))
}

func muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(12).TextColor(c.Theme().TextMuted)
}

// View builds the window.
func (a *App) View(c *ui.Context) {
	t := *c.Theme()
	t.Radius = 7
	c.SetTheme(&t)
	if a.toast != "" {
		c.Toast(a.toast)
		a.toast = ""
	}
	// Overviews and reports follow the language: one made in another is
	// made again.
	for _, d := range a.devices {
		d.overviewIn(c.Locale())
		d.reportIn(c.Locale())
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			a.mainPane(c)
		})
	})
}

func (a *App) sidebar(c *ui.Context) {
	t := c.Theme()
	m := locales.In(c.Locale())
	ui.Column(c).Width(248).Background(sidebarBg(t)).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).
		Padding(18, 12).Gap(6).Children(func() {
		ui.Row(c).Gap(10).Padding(0, 6, 14, 6).Children(func() {
			logo(c, 34, 9, 20)
			ui.Column(c).Gap(1).Children(func() {
				ui.Text(c, "iDevLoupe").FontSize(15).Bold()
				muted(c, m.AppTagline())
			})
		})
		muted(c, m.SidebarDevices()).Padding(4, 8).FontWeight(600)
		if len(a.devices) == 0 {
			ui.Column(c).Padding(10, 8).Gap(4).Children(func() {
				ui.Text(c, m.SidebarNoDevice()).FontSize(13)
				muted(c, m.SidebarNoDeviceHint()).Wrap()
			})
		}
		cur := a.current()
		for _, d := range a.devices {
			a.deviceItem(c.Key(d.dev.UDID), d, d == cur)
		}
		if a.muxErr != nil {
			ui.Column(c).Margin(8, 0).Padding(10).Gap(4).Radius(8).Background(t.Danger.Alpha(0.1)).Children(func() {
				ui.Text(c, m.SidebarNoUsbmuxd()).FontSize(12).Bold().TextColor(t.Danger)
				muted(c, inspect.ErrorText(c.Locale(), a.muxErr)).Wrap()
			})
		}
		ui.Spacer(c)
		a.languagePicker(c)
		muted(c, m.SidebarPrivacy()).Padding(0, 8).Wrap()
	})
}

// languagePicker chooses the app's language, which the app keeps for the
// next launches.
func (a *App) languagePicker(c *ui.Context) {
	lang := c.Locale()
	names := make([]string, len(languages))
	chosen := ""
	for i, tag := range languages {
		names[i] = i18n.Autonym(tag)
		if i18n.Match([]string{lang}, []string{tag}) == tag {
			chosen = names[i]
		}
	}
	ui.Row(c).Gap(8).Padding(0, 8, 4, 8).Children(func() {
		ui.Icon(c, icGlobe).TextColor(c.Theme().TextMuted)
		if ui.Select(c, &chosen, names).Grow(1).Label(locales.In(lang).FieldLanguage()).Changed() {
			for i, n := range names {
				if n == chosen {
					a.setLocale(languages[i])
				}
			}
		}
	})
}

func (a *App) deviceItem(c *ui.Context, d *devState, selected bool) {
	t := c.Theme()
	lang := c.Locale()
	item := ui.Row(c).Gap(10).Padding(9, 10).Radius(8).Cursor(ui.CursorPointer).Label(d.title(lang))
	if selected {
		item.Background(t.Accent.Alpha(0.14))
	} else if item.Hovered() {
		item.Background(t.Text.Alpha(0.05))
	}
	if item.Clicked() {
		a.selected = d.dev.UDID
	}
	var dot ui.Color
	switch {
	case d.report != nil && (d.phase == phaseDone || d.phase == phaseReady):
		_, st := d.report.Verdict()
		dot = statusColor(t, st)
	case d.phase == phaseReady:
		dot = t.Accent
	}
	switch d.phase {
	case phaseDone, phaseReady:
	case phaseError:
		dot = t.Danger
	case phasePairing:
		dot = t.Warning
	default:
		dot = infoBlue
	}
	item.Children(func() {
		ui.Icon(c, icPhone).FontSize(20).TextColor(t.TextMuted)
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, d.title(lang)).FontSize(13).FontWeight(600).SingleLine()
			sub := d.subtitle(lang)
			if d.dev.ConnectionType == "Network" {
				sub += " · Wi-Fi"
			}
			muted(c, sub).SingleLine()
		})
		ui.Box(c).Size(8, 8).Radius(4).Background(dot)
	})
}

func (a *App) mainPane(c *ui.Context) {
	d := a.current()
	if d == nil {
		a.emptyState(c)
		return
	}
	lang := c.Locale()
	m := locales.In(lang)
	step := ""
	if d.step != nil {
		step = d.step(lang)
	}
	switch d.phase {
	case phaseReady:
		a.overviewView(c, d)
	case phaseDone:
		a.reportView(c, d)
	case phasePairing:
		centered(c, func() {
			ui.Box(c).Size(72, 72).Radius(36).Center().Background(c.Theme().Warning.Alpha(0.14)).Children(func() {
				ui.Icon(c, icHand).FontSize(34).TextColor(c.Theme().Warning)
			})
			ui.Text(c, m.PairingTitle()).FontSize(20).Bold()
			ui.Text(c, step).TextColor(c.Theme().TextMuted).TextAlign(ui.Center)
			muted(c, m.PairingHint()).TextAlign(ui.Center)
			ui.Spinner(c).Margin(6, 0, 0, 0)
		})
	case phaseError:
		centered(c, func() {
			ui.Box(c).Size(72, 72).Radius(36).Center().Background(c.Theme().Danger.Alpha(0.12)).Children(func() {
				ui.Icon(c, icAlert).FontSize(32).TextColor(c.Theme().Danger)
			})
			ui.Text(c, m.ErrorTitle()).FontSize(20).Bold()
			if d.err != nil {
				ui.Text(c, inspect.ErrorText(lang, d.err)).TextColor(c.Theme().TextMuted).TextAlign(ui.Center).Selectable().MaxWidth(520).Wrap()
			}
			if ui.PrimaryButton(c, m.Retry()).Margin(6, 0, 0, 0).Clicked() {
				// An inspection that failed is tried again; a connection,
				// made again.
				if d.info != nil {
					a.inspectDevice(d)
				} else {
					a.start(d)
				}
			}
		})
	default:
		centered(c, func() {
			ui.Spinner(c).Size(36, 36)
			ui.Text(c, d.title(lang)).FontSize(20).Bold()
			if s := d.subtitle(lang); s != "" {
				muted(c, s)
			}
			ui.Text(c, step+"…").TextColor(c.Theme().TextMuted)
		})
	}
}

func centered(c *ui.Context, fn func()) {
	ui.Column(c).Fill().Center().Gap(10).Padding(32).Children(fn)
}

func (a *App) emptyState(c *ui.Context) {
	t := c.Theme()
	m := locales.In(c.Locale())
	centered(c, func() {
		ui.Box(c).Size(88, 88).Radius(44).Center().Background(t.Accent.Alpha(0.1)).Children(func() {
			ui.Icon(c, icPlug).FontSize(40).TextColor(t.Accent)
		})
		ui.Text(c, m.EmptyTitle()).FontSize(22).Bold().Margin(6, 0, 0, 0)
		muted(c, m.EmptySubtitle()).FontSize(13).TextAlign(ui.Center).MaxWidth(560).Wrap()
		ui.Row(c).Gap(12).Margin(18, 0, 0, 0).AlignItems(ui.Stretch).Children(func() {
			for i, s := range []string{m.EmptyStepConnect(), m.EmptyStepTrust(), m.EmptyStepDone()} {
				card(c).Width(180).Padding(14).Gap(6).Children(func() {
					ui.Box(c).Size(24, 24).Radius(12).Center().Background(t.Accent).Children(func() {
						ui.Textf(c, "%d", i+1).FontSize(12).Bold().TextColor(t.AccentText)
					})
					ui.Text(c, s).FontSize(13).Wrap()
				})
			}
		})
	})
}

// sectionsGrid lays out the device information, two sections a row.
func sectionsGrid(c *ui.Context, secs []inspect.Section, columns int) {
	t := c.Theme()
	ui.Grid(c).Columns(columns).Gap(16).AlignItems(ui.Start).Children(func() {
		for _, sec := range secs {
			card(c.Key(sec.Title)).Padding(14, 16).Gap(10).Children(func() {
				ui.Text(c, sec.Title).FontSize(13).Bold()
				if sec.Note != "" {
					muted(c, sec.Note).Wrap()
				}
				ui.Grid(c).ColumnTracks(ui.Fixed(150), ui.Fr(1)).GapX(12).GapY(8).Selectable().Children(func() {
					for _, f := range sec.Fields {
						ui.Text(c, f.Label).FontSize(13).TextColor(t.TextMuted).Wrap()
						ui.Text(c, f.Value).FontSize(13).Wrap()
					}
				})
			})
		}
	})
}
