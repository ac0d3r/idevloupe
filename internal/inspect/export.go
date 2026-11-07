package inspect

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ac0d3r/idevloupe/internal/locales"
)

// Text renders the report as plain text, in its language, for copying and
// saving.
func (r *Report) Text() string {
	m := locales.In(r.Lang)
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	heading := func(title string) { line("\n" + m.ReportHeading(title)) }
	field := func(label, value string) { line("  " + m.LabelValue(label, value)) }

	s := r.Summary
	line(m.ReportTitle())
	line(m.ReportTime(r.Time.Format("2006-01-02 15:04:05")) + "\n")
	line(strings.Join(nonEmpty(s.Marketing, s.Capacity, s.ModelNumber), "  "))
	line(m.ReportSerial(s.Serial, s.IOS))
	verdict, _ := r.Verdict()
	c := r.Counts()
	var counts []string
	for _, p := range []struct {
		st Status
		n  int
	}{{Pass, c.Pass}, {Info, c.Info}, {Warn, c.Warn}, {Fail, c.Fail}, {Unknown, c.Unknown}} {
		counts = append(counts, m.StatusCount(p.st.Label(r.Lang), p.n))
	}
	line(m.ReportVerdict(verdict, strings.Join(counts, " · ")))
	if issues := r.Issues(); len(issues) > 0 {
		var heads []string
		for _, is := range issues {
			heads = append(heads, is.Headline())
		}
		line(m.ReportIssues(strings.Join(heads, " · ")))
	}

	cat := ""
	for _, ch := range r.Checks {
		if ch.Category != cat {
			cat = ch.Category
			heading(cat)
		}
		val := ch.Value
		if val == "" {
			val = "—"
		}
		line(fmt.Sprintf("  [%s] %s", ch.Status.Label(r.Lang), m.LabelValue(ch.Title, val)))
		if ch.Detail != "" {
			line("         " + ch.Detail)
		}
	}
	if bt := r.Battery; bt != nil && bt.Present {
		heading(m.SectionBatteryDetails())
		if bt.HasDesign {
			field(m.FieldDesignCapacity(), m.ValueMAh(int(bt.DesignCapacity)))
		}
		if bt.HasMax {
			field(m.FieldFullChargeCapacity(), m.ValueMAh(int(bt.MaxCapacity)))
		}
		if bt.HasCurrentLevel {
			field(m.FieldChargeLevel(), m.ValuePercent(int(bt.CurrentPercent)))
		}
		if bt.ResistanceMetric > 0 {
			field(m.FieldResistance(), m.ResistanceValue(int(bt.ResistanceMetric)))
		}
		if bt.VoltageMV != 0 {
			field(m.FieldVoltage(), m.ValueMV(int(bt.VoltageMV)))
		}
		if bt.Serial != "" {
			field(m.FieldBatterySerial(), bt.Serial)
			if bt.Maker != "" {
				field(m.FieldBatteryMaker(), bt.Maker)
			}
		}
	}
	for _, sec := range r.Sections {
		heading(sec.Title)
		if sec.Note != "" {
			field(m.FieldNote(), sec.Note)
		}
		for _, f := range sec.Fields {
			field(f.Label, f.Value)
		}
	}
	if len(r.Manual) > 0 {
		heading(m.SectionManual())
		for _, s := range r.Manual {
			line("  · " + s)
		}
	}
	if len(r.Errors) > 0 {
		heading(m.SectionUnread())
		for _, e := range r.Errors {
			line("  · " + e)
		}
	}
	return b.String()
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// JSON renders the report, including the raw values, as indented JSON. A
// check's Status is its code ("pass", "warn"...), the same in every
// language, and StatusText names it in the report's.
func (r *Report) JSON() ([]byte, error) {
	type check struct {
		Category, Title, Value, Status, StatusText, Detail string
	}
	out := struct {
		*Report
		Checks  []check
		Verdict string
	}{Report: r}
	for _, c := range r.Checks {
		out.Checks = append(out.Checks, check{c.Category, c.Title, c.Value, c.Status.String(), c.Status.Label(r.Lang), c.Detail})
	}
	out.Verdict, _ = r.Verdict()
	return json.MarshalIndent(out, "", "  ")
}
