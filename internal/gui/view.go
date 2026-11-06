package gui

import (
	"runtime"
	"strconv"
	"strings"

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

// badgeWidth fits the longest status of the language, so that the
// checks' titles line up.
func badgeWidth(lang string) float32 {
	if strings.HasPrefix(lang, "zh") {
		return 62
	}
	return 100
}

func badge(c *ui.Context, s inspect.Status) {
	col := statusColor(c.Theme(), s)
	lang := c.Locale()
	ui.Row(c).Gap(4).Padding(3, 8).Radius(999).Background(col.Alpha(0.13)).Width(badgeWidth(lang)).Justify(ui.Center).Children(func() {
		ui.Icon(c, statusIcon(s)).FontSize(12).TextColor(col)
		ui.Text(c, s.Label(lang)).FontSize(12).FontWeight(600).TextColor(col).SingleLine()
	})
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

// summaryHeader names the unit: its model, then the identity buyers copy
// (capacity, iOS and serial number), its color, region and part number.
func summaryHeader(c *ui.Context, s inspect.Summary) {
	t := c.Theme()
	m := locales.In(c.Locale())
	ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
		ui.Text(c, s.Marketing).FontSize(24).Bold().SingleLine()
		ui.Row(c).Gap(6).Wrap().Children(func() {
			chip := func(v string) ui.Element {
				if v == "" {
					return ui.Element{}
				}
				return ui.Text(c, v).FontSize(12).Padding(3, 9).Radius(999).Background(t.Text.Alpha(0.06)).SingleLine()
			}
			chip(s.Capacity)
			if s.IOS != "" {
				chip("iOS " + s.IOS)
			}
			if s.Serial != "" {
				chip(m.ChipSerial(s.Serial)).Selectable()
			}
			if s.ColorHex != "" {
				ui.Row(c).Gap(5).Padding(3, 9).Radius(999).Background(t.Text.Alpha(0.06)).Children(func() {
					ui.Box(c).Size(10, 10).Radius(5).Background(ui.Hex(s.ColorHex)).Border(1, t.Border)
					ui.Text(c, s.ColorHex).FontSize(12)
				})
			}
			chip(s.Region)
			chip(s.ModelNumber)
		})
	})
}

// overviewView shows what the device says about itself, judging nothing:
// the inspection runs when asked for.
func (a *App) overviewView(c *ui.Context, d *devState) {
	o := d.overviewIn(c.Locale())
	t := c.Theme()
	m := locales.In(o.Lang)
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).Padding(22, 28, 18, 28).Gap(10).Children(func() {
			ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
				summaryHeader(c, o.Summary)
				ui.Row(c).Gap(8).Children(func() {
					if d.report != nil {
						if ui.Button(c, "").Children(func() {
							ui.Icon(c, icCheck)
							ui.Text(c, m.ActionViewReport()).SingleLine()
						}).Clicked() {
							d.phase = phaseDone
						}
					}
					if ui.PrimaryButton(c, "").Children(func() {
						ui.Icon(c, icLoupe)
						ui.Text(c, m.ActionInspect()).SingleLine()
					}).Clicked() {
						a.inspectDevice(d)
					}
				})
			})
			muted(c, m.OverviewHint()).Wrap()
		})
		ui.Divider(c)
		ui.Scroll(c).Grow(1).Padding(20, 28, 28, 28).Gap(16).Children(func() {
			if len(o.Fields) > 0 {
				card(c.Key("fields")).Padding(16, 18).Children(func() {
					ui.Grid(c).ColumnTracks(ui.Fixed(130), ui.Fr(1), ui.Fixed(130), ui.Fr(1)).GapX(12).GapY(10).Selectable().Children(func() {
						for _, f := range o.Fields {
							ui.Text(c, f.Label).FontSize(13).TextColor(t.TextMuted).Wrap()
							ui.Text(c, f.Value).FontSize(13).Wrap()
						}
					})
				})
			}
			if total := o.StorageTotal; total > 0 {
				card(c.Key("storage")).Padding(16, 18).Gap(10).Children(func() {
					used := total - o.StorageFree
					ui.Row(c).Children(func() {
						ui.Text(c, m.CheckStorage()).FontSize(13).Bold()
						ui.Spacer(c)
						if o.StorageFree > 0 {
							muted(c, m.StorageUsage(float64(used)/1e9, float64(total)/1e9))
						} else {
							muted(c, m.ValueGB(float64(total)/1e9))
						}
					})
					if o.StorageFree > 0 {
						ui.Box(c).Height(8).Radius(4).Background(t.Text.Alpha(0.08)).Children(func() {
							ui.Box(c).Height(8).Radius(4).WidthPercent(float32(used) * 100 / float32(total)).Background(t.Accent)
						})
					}
				})
			}
			open := a.detailsOpen
			card(c.Key("details")).Padding(6, 12).Children(func() {
				if ui.Collapsible(c, m.OverviewDetails(), &open, func() {
					ui.Column(c).Padding(4, 4, 10, 4).Children(func() {
						sectionsGrid(c, o.Sections)
					})
				}).Changed() {
					a.detailsOpen = open
				}
			})
		})
	})
}

func (a *App) reportView(c *ui.Context, d *devState) {
	r := d.reportIn(c.Locale())
	m := locales.In(r.Lang)
	ui.Column(c).Fill().Children(func() {
		// Header
		ui.Column(c).Padding(22, 28, 0, 28).Gap(16).Children(func() {
			ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
				summaryHeader(c, r.Summary)
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, "").Children(func() {
						ui.Icon(c, icPhone)
						ui.Text(c, m.ActionOverview()).SingleLine()
					}).Clicked() {
						d.phase = phaseReady
					}
					if ui.Button(c, "").Tooltip(m.ActionInspectAgain()).Children(func() {
						ui.Icon(c, icRefresh)
						ui.Text(c, m.ActionInspectAgain()).SingleLine()
					}).Clicked() {
						a.inspectDevice(d)
					}
					if ui.Button(c, "").Children(func() {
						ui.Icon(c, icCopy)
						ui.Text(c, m.ActionCopy()).SingleLine()
					}).Clicked() {
						a.copyReport(r)
					}
					if ui.PrimaryButton(c, "").Children(func() {
						ui.Icon(c, icSave)
						ui.Text(c, m.ActionExport()).SingleLine()
					}).Clicked() {
						a.exportReport(r)
					}
				})
			})
			a.verdictCard(c, r)
			ui.Tabs(c, &a.tab, m.TabReport(), m.TabBattery(), m.TabInfo(), m.TabRaw())
		})
		ui.Divider(c)
		ui.Scroll(c).Key(a.tab).Grow(1).Padding(20, 28, 28, 28).Gap(16).Children(func() {
			switch a.tab {
			case 0:
				a.checksTab(c, r)
			case 1:
				a.batteryTab(c, r)
			case 2:
				sectionsGrid(c, r.Sections)
			case 3:
				a.rawTab(c, r)
			}
		})
	})
}

// verdictCard answers what is wrong with the unit: the verdict, the
// tally, and the worst issues by name. The battery's figures live on its
// page.
func (a *App) verdictCard(c *ui.Context, r *inspect.Report) {
	t := c.Theme()
	m := locales.In(r.Lang)
	verdict, vst := r.Verdict()
	cnt := r.Counts()
	vcol := statusColor(t, vst)
	const shown = 3
	issues := r.Issues()
	ui.Row(c).Padding(14, 18).Gap(14).Radius(10).Background(vcol.Alpha(0.1)).Border(1, vcol.Alpha(0.3)).Children(func() {
		ui.Box(c).Size(40, 40).Radius(20).Center().Background(vcol).Children(func() {
			ui.Icon(c, statusIcon(vst)).FontSize(22).TextColor(ui.Hex("#ffffff"))
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Baseline).Children(func() {
				ui.Text(c, verdict).FontSize(16).Bold().TextColor(vcol).SingleLine()
				ui.Spacer(c)
				for _, p := range []struct {
					n  int
					st inspect.Status
				}{{cnt.Pass, inspect.Pass}, {cnt.Info, inspect.Info}, {cnt.Warn, inspect.Warn}, {cnt.Fail, inspect.Fail}, {cnt.Unknown, inspect.Unknown}} {
					if p.n == 0 && p.st != inspect.Pass {
						continue
					}
					ui.Text(c, m.StatusCount(p.st.Label(r.Lang), p.n)).FontSize(12).TextColor(statusColor(t, p.st)).FontWeight(600)
				}
			})
			if len(issues) == 0 {
				return
			}
			ui.Row(c).Gap(6).Wrap().Children(func() {
				for i, is := range issues[:min(len(issues), shown)] {
					if i > 0 {
						muted(c, "·").FontSize(13)
					}
					ui.Text(c, is.Headline()).FontSize(13).FontWeight(600).TextColor(statusColor(t, is.Status))
				}
				if more := len(issues) - shown; more > 0 {
					muted(c, m.IssuesTotal(len(issues))).FontSize(13)
				}
			})
		})
	})
}

// checkRow shows one check: its status, title (after prefix), value and
// detail.
func checkRow(c *ui.Context, ch inspect.Check, prefix string) {
	t := c.Theme()
	ui.Row(c).Padding(10, 16).Gap(14).AlignItems(ui.Start).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		badge(c, ch.Status)
		ui.Text(c, prefix+ch.Title).Width(170).FontSize(13).FontWeight(600).Padding(2, 0, 0, 0).Wrap()
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Padding(2, 0, 0, 0).Children(func() {
			val := ch.Value
			if val == "" {
				val = locales.In(c.Locale()).ValueNotRead()
			}
			v := ui.Text(c, val).FontSize(13).Wrap()
			if ch.Value == "" {
				v.TextColor(t.TextMuted)
			}
			if ch.Detail != "" {
				muted(c, ch.Detail).Wrap()
			}
		})
	})
}

func (a *App) checksTab(c *ui.Context, r *inspect.Report) {
	t := c.Theme()
	m := locales.In(r.Lang)
	var cats []string
	by := map[string][]inspect.Check{}
	for _, ch := range r.Checks {
		if _, ok := by[ch.Category]; !ok {
			cats = append(cats, ch.Category)
		}
		by[ch.Category] = append(by[ch.Category], ch)
	}
	// What needs attention comes first, worst first, so that a failed
	// Face ID or a replaced battery is not left at the bottom of the list.
	// The same order as the verdict card's, then what could not be read.
	attention := r.Issues()
	for _, ch := range r.Checks {
		if ch.Status == inspect.Unknown {
			attention = append(attention, ch)
		}
	}
	if len(attention) > 0 {
		_, vst := r.Verdict()
		col := statusColor(t, vst)
		card(c.Key("attention")).Border(1, col.Alpha(0.45)).Children(func() {
			ui.Row(c).Gap(8).Padding(12, 16, 10, 16).Children(func() {
				ui.Icon(c, statusIcon(vst)).TextColor(col)
				ui.Text(c, m.AttentionTitle()).FontSize(13).Bold()
				muted(c, m.AttentionCount(len(attention)))
			})
			ui.Column(c).Selectable().Children(func() {
				for _, ch := range attention {
					checkRow(c.Key("a/"+ch.Category+"/"+ch.Title), ch, ch.Category+" · ")
				}
			})
		})
	}
	for _, cat := range cats {
		card(c.Key(cat)).Children(func() {
			ui.Text(c, cat).FontSize(13).Bold().Padding(12, 16, 10, 16)
			ui.Column(c).Selectable().Children(func() {
				for _, ch := range by[cat] {
					checkRow(c.Key(ch.Title), ch, "")
				}
			})
		})
	}
	if len(r.Manual) > 0 {
		card(c.Key("manual")).Padding(14, 16).Gap(8).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Icon(c, icHand).TextColor(infoBlue)
				ui.Text(c, m.SectionManual()).FontSize(13).Bold()
				muted(c, m.ManualHint())
			})
			for _, m := range r.Manual {
				ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
					ui.Box(c).Size(5, 5).Radius(3).Background(t.TextMuted).Margin(7, 0, 0, 2)
					ui.Text(c, m).FontSize(13).Wrap().Grow(1)
				})
			}
		})
	}
	if len(r.Errors) > 0 {
		card(c.Key("errors")).Padding(14, 16).Gap(6).Children(func() {
			ui.Text(c, m.ErrorsTitle()).FontSize(13).Bold().TextColor(t.Warning)
			for _, e := range r.Errors {
				muted(c, e).Wrap().Selectable()
			}
		})
	}
}

func (a *App) batteryTab(c *ui.Context, r *inspect.Report) {
	t := c.Theme()
	m := locales.In(r.Lang)
	b := r.Battery
	if b == nil {
		card(c).Padding(24).Children(func() { muted(c, m.BatteryUnread()).FontSize(13) })
		return
	}
	var hc inspect.Check
	for _, ch := range r.Checks {
		if ch.Title == m.CheckBatteryHealth() {
			hc = ch
		}
	}
	health := b.Health()
	ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
		card(c).Width(300).Padding(20).Gap(10).Children(func() {
			muted(c, m.BatteryHealthMax()).FontSize(13)
			col := statusColor(t, hc.Status)
			if health > 0 {
				ui.Row(c).AlignItems(ui.Baseline).Gap(2).Children(func() {
					ui.Textf(c, "%.0f", health).FontSize(52).Bold().TextColor(col)
					ui.Text(c, "%").FontSize(22).Bold().TextColor(col)
				})
				// A bar in the check's own color, so it agrees with the verdict.
				ui.Box(c).Height(8).Radius(4).Background(t.Text.Alpha(0.08)).Label(m.LabelValue(m.CheckBatteryHealth(), m.ValuePercent(int(health+0.5)))).Children(func() {
					ui.Box(c).Height(8).Radius(4).WidthPercent(float32(min(health, 100))).Background(col)
				})
			} else {
				ui.Text(c, "—").FontSize(52).Bold()
			}
			if hc.Detail != "" {
				muted(c, hc.Detail).Wrap()
			}
		})
		ui.Grid(c).Grow(1).Columns(3).Gap(12).Children(func() {
			// Only what was read: a model or iOS version that does not
			// report a value shows no empty tile for it.
			stat := func(label, value string) {
				if value == "" {
					return
				}
				card(c).Padding(12, 14).Gap(4).Children(func() {
					muted(c, label)
					ui.Text(c, value).FontSize(16).FontWeight(600).Selectable().Wrap()
				})
			}
			opt := func(ok bool, say func(int) string, n int64) string {
				if !ok {
					return ""
				}
				return say(int(n))
			}
			stat(m.CheckCycles(), opt(b.HasCycleCount, m.ValueCycles, b.CycleCount))
			stat(m.FieldDesignCapacity(), opt(b.HasDesign, m.ValueMAh, b.DesignCapacity))
			stat(m.FieldFullChargeCapacity(), opt(b.HasMax, m.ValueMAh, b.MaxCapacity))
			stat(m.FieldChargeLevel(), opt(b.HasCurrentLevel, m.ValuePercent, b.CurrentPercent))
			if b.HasTemperature {
				stat(m.StatTemperature(), m.ValueCelsius(b.TemperatureC))
			}
			stat(m.FieldVoltage(), opt(b.VoltageMV != 0, m.ValueMV, b.VoltageMV))
			stat(m.StatCurrent(), opt(b.AmperageMA != 0, m.ValueMA, b.AmperageMA))
			charging := m.ChargingNot()
			if b.IsCharging {
				charging = m.ChargingNow()
			} else if b.ExternalPower {
				charging = m.ChargingPluggedIn()
			}
			stat(m.StatCharging(), charging)
			stat(m.FieldBatterySerial(), b.Serial)
			if b.ResistanceMetric > 0 {
				stat(m.FieldResistance(), strconv.FormatInt(b.ResistanceMetric, 10))
			}
			logDate := ""
			if !b.LogDate.IsZero() {
				logDate = b.LogDate.Format("2006-01-02")
			}
			stat(m.StatLogHealth(), opt(b.LogHealth > 0, m.ValuePercent, b.LogHealth))
			stat(m.StatLogCycles(), opt(b.HasLogCycles, m.ValueCycles, b.LogCycles))
			stat(m.StatLogDate(), logDate)
		})
	})
	muted(c, m.BatteryFootnote()).Wrap()
	if b.LogNote != "" {
		muted(c, m.BatteryLogNote(b.LogNote)).Wrap()
	}
}

// sectionsGrid lays out the device information, two sections a row.
func sectionsGrid(c *ui.Context, secs []inspect.Section) {
	t := c.Theme()
	ui.Grid(c).Columns(2).Gap(16).AlignItems(ui.Start).Children(func() {
		for _, sec := range secs {
			card(c.Key(sec.Title)).Padding(14, 16).Gap(10).Children(func() {
				ui.Text(c, sec.Title).FontSize(13).Bold()
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

func (a *App) rawTab(c *ui.Context, r *inspect.Report) {
	t := c.Theme()
	m := locales.In(r.Lang)
	muted(c, m.RawIntro()).Wrap()
	for _, g := range r.Raw {
		open := a.rawOpen[g.Source]
		card(c.Key(g.Source)).Padding(6, 12).Children(func() {
			if ui.Collapsible(c, m.RawGroupTitle(g.Source, len(g.Items)), &open, func() {
				ui.Grid(c).ColumnTracks(ui.Fixed(280), ui.Fr(1)).GapX(12).GapY(5).Padding(4, 4, 10, 4).Selectable().Children(func() {
					for _, f := range g.Items {
						ui.Text(c, f.Label).Font(monoFont).FontSize(12).TextColor(t.TextMuted).Wrap()
						ui.Text(c, f.Value).Font(monoFont).FontSize(12).Wrap()
					}
				})
			}).Changed() {
				a.rawOpen[g.Source] = open
			}
		})
	}
}
