// Package devtable is device data taken from 3uTools' device table
// (devices_table.txt). Every lookup of a nil Table finds nothing.
package devtable

import (
	"cmp"
	"strings"
)

type Text struct{ Zh, En string }

func (t Text) In(lang string) string {
	if strings.HasPrefix(lang, "zh") && t.Zh != "" {
		return t.Zh
	}
	return cmp.Or(t.En, t.Zh)
}

type Model struct {
	ReleaseDate, ScreenSize Text
	Chip                    Text
	Cores, Frequency        string
	Resolution              string
}

type color struct {
	back, front         string
	backName, frontName Text
}

type Table struct {
	colors    map[string][]color
	models    map[string]Model
	batteries map[string]Text
}

func Builtin() *Table {
	return &Table{colors: builtinColors, models: builtinModels, batteries: builtinBatteries}
}

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

func (t *Table) Model(productType string) (Model, bool) {
	if t == nil {
		return Model{}, false
	}
	m, ok := t.models[productType]
	return m, ok
}

func (t *Table) BatteryMaker(serial, lang string) (string, bool) {
	if t == nil || len(serial) < 2 {
		return "", false
	}
	b, ok := t.batteries[strings.ToUpper(serial[:2])]
	return b.In(lang), ok
}
