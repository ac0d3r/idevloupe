package gui

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/internal/locales"
)

// The report table's fixed columns: the check's name and its result. The
// factory and read values share the rest.
const (
	itemColumn   = 140
	resultColumn = 84
)

// reportView is the inspection report on one page, made to be read and
// shared as one screenshot: the verdict and the unit's key facts on the
// left; on the right what needs attention, then every check, its factory
// value beside the value read where the factory recorded one.
func (a *App) reportView(c *ui.Context, d *devState) {
	r := d.reportIn(c.Locale())
	m := locales.In(r.Lang)
	mask := a.masker(r)
	ui.Column(c).Fill().Children(func() {
		a.reportBar(c, d, r)
		ui.Divider(c)
		ui.Scroll(c).Grow(1).Padding(20, 28, 28, 28).Children(func() {
			ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Width(220).Gap(16).Children(func() {
					verdictCard(c, r)
					reportFacts(c, r, mask)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(16).Children(func() {
					reportBanner(c, r)
					checksTable(c, r, mask)
					manualCard(c, m, r.Manual)
					errorsCard(c, m, r.Errors)
				})
			})
		})
	})
}

// reportBar names the unit and holds the report's actions.
func (a *App) reportBar(c *ui.Context, d *devState, r *inspect.Report) {
	m := locales.In(r.Lang)
	ui.Row(c).Padding(12, 28).Gap(8).Children(func() {
		if ui.Button(c, "").Tooltip(m.ActionOverview()).Label(m.ActionOverview()).Children(func() {
			ui.Icon(c, icPhone)
		}).Clicked() {
			d.phase = phaseReady
		}
		ui.Text(c, joinDot(r.Summary.Marketing, r.Summary.Capacity)).FontSize(15).Bold().SingleLine().Margin(0, 0, 0, 4)
		ui.Spacer(c)
		ui.Checkbox(c, &a.hideIDs, m.ActionHideIdentifiers())
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
}

// masker hides the values that name the unit when the person asks to, as
// for a screenshot to share; it returns its argument otherwise.
func (a *App) masker(r *inspect.Report) func(string) string {
	if !a.hideIDs {
		return func(s string) string { return s }
	}
	var pairs []string
	for _, id := range r.Identifiers() {
		pairs = append(pairs, id, maskID(id))
	}
	rep := strings.NewReplacer(pairs...)
	return rep.Replace
}

// maskID keeps enough of an identifier to tell it from another: a network
// address's octets but the fourth, the first and last three characters of
// any other.
func maskID(id string) string {
	if parts := strings.Split(id, ":"); len(parts) == 6 {
		parts[3] = "**"
		return strings.Join(parts, ":")
	}
	if len(id) <= 6 {
		return strings.Repeat("*", len(id))
	}
	return id[:3] + "***" + id[len(id)-3:]
}

// verdictCard is the inspection's conclusion and its tally.
func verdictCard(c *ui.Context, r *inspect.Report) {
	t := c.Theme()
	m := locales.In(r.Lang)
	verdict, vst := r.Verdict()
	col := statusColor(t, vst)
	cnt := r.Counts()
	card(c.Key("verdict")).Padding(16, 18).Gap(12).Children(func() {
		ui.Row(c).Gap(12).Children(func() {
			ui.Box(c).Size(40, 40).Radius(20).Center().Background(col).Children(func() {
				ui.Icon(c, statusIcon(vst)).FontSize(22).TextColor(ui.Hex("#ffffff"))
			})
			ui.Text(c, verdict).FontSize(14).Bold().TextColor(col).Grow(1).MinWidth(0).Wrap()
		})
		ui.Row(c).Gap(12).Wrap().Children(func() {
			for _, p := range []struct {
				n  int
				st inspect.Status
			}{{cnt.Pass, inspect.Pass}, {cnt.Info, inspect.Info}, {cnt.Warn, inspect.Warn}, {cnt.Fail, inspect.Fail}, {cnt.Unknown, inspect.Unknown}} {
				if p.n == 0 && p.st != inspect.Pass {
					continue
				}
				ui.Text(c, m.StatusCount(p.st.Label(r.Lang), p.n)).FontSize(12).FontWeight(600).TextColor(statusColor(t, p.st)).SingleLine()
			}
		})
	})
}

// reportFacts lists what a buyer asks of the unit first, from the summary
// and the checks.
func reportFacts(c *ui.Context, r *inspect.Report, mask func(string) string) {
	t := c.Theme()
	m := locales.In(r.Lang)
	value := func(title string) string {
		for _, ch := range r.Checks {
			if ch.Title == title {
				return ch.Value
			}
		}
		return ""
	}
	facts := []inspect.Field{
		{Label: m.FieldModelIdentifier(), Value: r.Summary.ProductType},
		{Label: m.FieldIOSVersion(), Value: r.Summary.IOS},
		{Label: m.CheckSerial(), Value: r.Summary.Serial},
		{Label: m.CheckRegion(), Value: r.Summary.Region},
		{Label: m.CheckActivation(), Value: value(m.CheckActivation())},
		{Label: m.CheckFindMy(), Value: value(m.CheckFindMy())},
		{Label: m.CheckJailbreak(), Value: value(m.CheckJailbreak())},
		{Label: m.CheckBatteryHealth(), Value: value(m.CheckBatteryHealth())},
		{Label: m.CheckCycles(), Value: value(m.CheckCycles())},
	}
	card(c.Key("facts")).Padding(14, 18).Gap(4).Selectable().Children(func() {
		ui.Text(c, m.ReportFactsTitle()).FontSize(13).Bold().Margin(0, 0, 4, 0)
		for _, f := range facts {
			ui.Row(c.Key(f.Label)).Gap(10).Padding(4, 0).AlignItems(ui.Start).Children(func() {
				ui.Text(c, f.Label).FontSize(12).TextColor(t.TextMuted).Width(76).Wrap()
				if f.Value == "" {
					muted(c, "--").FontSize(12)
					return
				}
				ui.Text(c, mask(f.Value)).FontSize(12).Grow(1).MinWidth(0).Wrap()
			})
		}
	})
}

// reportBanner says what needs attention, worst first, and when the
// inspection ran.
func reportBanner(c *ui.Context, r *inspect.Report) {
	t := c.Theme()
	m := locales.In(r.Lang)
	verdict, vst := r.Verdict()
	col := statusColor(t, vst)
	const shown = 3
	issues := r.Issues()
	ui.Row(c.Key("banner")).Padding(10, 14).Gap(10).Radius(8).Background(col.Alpha(0.12)).Border(1, col.Alpha(0.3)).Children(func() {
		ui.Icon(c, statusIcon(vst)).TextColor(col)
		ui.Row(c).Grow(1).MinWidth(0).Gap(6).Wrap().Children(func() {
			if len(issues) == 0 {
				ui.Text(c, verdict).FontSize(13).FontWeight(600).TextColor(col)
				return
			}
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
		if !r.Time.IsZero() {
			muted(c, r.Time.Local().Format("2006-01-02 15:04:05")).FontSize(12).SingleLine()
		}
	})
}

// checksTable lists every check by category: its name, the factory value
// and the value read, and its result. A check that is not a pass says why
// under its row; a pass says it on hover.
func checksTable(c *ui.Context, r *inspect.Report, mask func(string) string) {
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
	card(c.Key("checks")).Padding(8, 12, 12, 12).Children(func() {
		ui.Row(c).Gap(12).Padding(10, 12).Children(func() {
			head := func(s string) ui.Element { return ui.Text(c, s).FontSize(12).Bold().SingleLine() }
			head(m.ReportColumnItem()).Width(itemColumn)
			head(m.ReportColumnFactory()).Grow(1).Basis(0).MinWidth(0)
			head(m.ReportColumnRead()).Grow(1).Basis(0).MinWidth(0)
			head(m.ReportColumnResult()).Width(resultColumn)
		})
		ui.Column(c).Selectable().Children(func() {
			for _, cat := range cats {
				ui.Text(c.Key("cat/"+cat), cat).FontSize(12).Bold().TextColor(t.TextMuted).Padding(12, 12, 4, 12)
				for i, ch := range by[cat] {
					checkLine(c.Key(cat+"/"+ch.Title), m, ch, i%2 == 0, mask)
				}
			}
		})
	})
}

// checkLine is one row of the checks table, banded when striped.
func checkLine(c *ui.Context, m locales.Messages, ch inspect.Check, striped bool, mask func(string) string) {
	t := c.Theme()
	row := ui.Column(c).Padding(8, 12).Gap(4).Radius(6)
	if striped {
		row.Background(t.Text.Alpha(0.03))
	}
	if ch.Status == inspect.Pass && ch.Detail != "" {
		row.Tooltip(mask(ch.Detail))
	}
	row.Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Text(c, ch.Name()).FontSize(13).Width(itemColumn).Wrap()
			if ch.Factory.Value != "" || ch.Read.Value != "" {
				readingCell(c, ch.Factory, mask)
				readingCell(c, ch.Read, mask)
			} else {
				ui.Box(c).Grow(1).Basis(0).MinWidth(0)
				ui.Box(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
					if ch.Value == "" {
						muted(c, m.ValueNotRead()).FontSize(13).Wrap()
						return
					}
					ui.Text(c, mask(ch.Value)).FontSize(13).Wrap()
				})
			}
			resultCell(c, ch.Status, m)
		})
		if ch.Status != inspect.Pass && ch.Detail != "" {
			muted(c, mask(ch.Detail)).FontSize(12).Wrap().Margin(0, resultColumn+12, 0, itemColumn+12)
		}
	})
}

// readingCell is a factory or read value, with the date its serial
// carries muted under it.
func readingCell(c *ui.Context, rd inspect.Reading, mask func(string) string) {
	ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
		if rd.Value == "" {
			muted(c, "--").FontSize(13)
			return
		}
		ui.Text(c, mask(rd.Value)).FontSize(13).Wrap()
		if rd.Date != "" {
			muted(c, rd.Date).FontSize(11).SingleLine()
		}
	})
}

// resultCell is a check's status, as an icon and its name in its color.
func resultCell(c *ui.Context, st inspect.Status, m locales.Messages) {
	t := c.Theme()
	col := statusColor(t, st)
	ui.Row(c).Width(resultColumn).Gap(6).Children(func() {
		ui.Icon(c, statusIcon(st)).Size(14, 14).TextColor(col)
		ui.Text(c, st.Label(c.Locale())).FontSize(13).TextColor(col).SingleLine()
	})
}

// manualCard lists what software cannot verify, to check by hand.
func manualCard(c *ui.Context, m locales.Messages, manual []string) {
	if len(manual) == 0 {
		return
	}
	t := c.Theme()
	card(c.Key("manual")).Padding(14, 16).Gap(8).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.Icon(c, icHand).TextColor(infoBlue)
			ui.Text(c, m.SectionManual()).FontSize(13).Bold()
			muted(c, m.ManualHint())
		})
		for _, s := range manual {
			ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
				ui.Box(c).Size(5, 5).Radius(3).Background(t.TextMuted).Margin(7, 0, 0, 2)
				ui.Text(c, s).FontSize(13).Wrap().Grow(1)
			})
		}
	})
}

// errorsCard lists the steps of the collection that failed.
func errorsCard(c *ui.Context, m locales.Messages, errs []string) {
	if len(errs) == 0 {
		return
	}
	t := c.Theme()
	card(c.Key("errors")).Padding(14, 16).Gap(6).Children(func() {
		ui.Text(c, m.ErrorsTitle()).FontSize(13).Bold().TextColor(t.Warning)
		for _, e := range errs {
			muted(c, e).Wrap().Selectable()
		}
	})
}
