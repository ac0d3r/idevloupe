// Package devtable is device data taken from 3uTools' device table
// (devices_table.txt) and built in: colors by model and color code,
// release dates, screen sizes, chips and battery makers. Every lookup of a
// nil Table finds nothing.
package devtable

import (
	"cmp"
	"strings"
)

// Text is a value in Chinese and in English.
type Text struct{ Zh, En string }

// In returns the text for lang: Chinese for Chinese, else English.
func (t Text) In(lang string) string {
	if strings.HasPrefix(lang, "zh") && t.Zh != "" {
		return t.Zh
	}
	return cmp.Or(t.En, t.Zh)
}

// Model is what the table says of a model.
type Model struct {
	ReleaseDate, ScreenSize Text
	Chip                    Text   // "A14 六核", "A14 Hexa"
	Cores, Frequency        string // "6", "3.1GHz"
	Resolution              string // "2532 x 1170"
}

type color struct {
	back, front         string // codes
	backName, frontName Text
}

// Table is the device table.
type Table struct {
	colors    map[string][]color // by ProductType
	models    map[string]Model   // by ProductType
	batteries map[string]Text    // battery makers by serial prefix
}

// Builtin returns the table built in.
func Builtin() *Table {
	return &Table{colors: builtinColors, models: builtinModels, batteries: builtinBatteries}
}

// BackColor names a model's back color code in lang.
func (t *Table) BackColor(productType, code, lang string) (string, bool) {
	if t == nil || code == "" {
		return "", false
	}
	for _, c := range t.colors[productType] {
		if c.back == code {
			return c.backName.In(lang), true
		}
	}
	return "", false
}

// FrontColor names a model's front color code in lang.
func (t *Table) FrontColor(productType, code, lang string) (string, bool) {
	if t == nil || code == "" {
		return "", false
	}
	for _, c := range t.colors[productType] {
		if c.front == code {
			return c.frontName.In(lang), true
		}
	}
	return "", false
}

// Model returns what the table says of a model.
func (t *Table) Model(productType string) (Model, bool) {
	if t == nil {
		return Model{}, false
	}
	m, ok := t.models[productType]
	return m, ok
}

// BatteryMaker names the maker of a battery by the first two characters
// of its serial number.
func (t *Table) BatteryMaker(serial, lang string) (string, bool) {
	if t == nil || len(serial) < 2 {
		return "", false
	}
	b, ok := t.batteries[strings.ToUpper(serial[:2])]
	return b.In(lang), ok
}
