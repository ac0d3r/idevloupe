// Package devtable reads 3uTools' device table (devices_table.txt), a
// JSON file 3uTools downloads to its cache: colors by model and color code,
// release dates, screen sizes, chips and battery makers.
//
// The program reads the user's own copy when it finds one (Find), else a
// copy built in (resources.DevicesTable). Every lookup of a nil Table
// finds nothing, so the program works without it.
package devtable

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// EnvPath names the environment variable that gives the table's path.
const EnvPath = "IDEVLOUPE_DEVICES_TABLE"

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
	ChipName                string // "Apple A14"
	Cores, Frequency        string // "6", "3.1GHz"
	Resolution              string // "2532 x 1170"
}

type color struct {
	back, front         string // codes
	backName, frontName Text
}

// Table is the device table.
type Table struct {
	Version   string
	colors    map[string][]color // by ProductType
	models    map[string]Model   // by ProductType
	batteries map[string]Text    // battery makers by serial prefix
}

// file is the table's layout, as far as it is read.
type file struct {
	Version string `json:"version"`
	Images  []struct {
		SupportTypes []struct {
			ProductType string
		}
		Imgs []struct {
			BackColor, BackColorText, BackColorText_en    string
			FrontColor, FrontColorText, FrontColorText_en string
		} `json:"imgs"`
	}
	Devices []struct {
		ProductType, ReleaseDate, ReleaseDate_en, ScreenSize, ScreenSize_en string
	} `json:"devices"`
	CPUs []struct {
		ProductType   string `json:"productType"`
		CPU           string `json:"cpu"`
		DisplayName   string `json:"displayName"`
		DisplayNameEn string `json:"displayName_en"`
		CoreCount     string `json:"coreCount"`
		Rate          string `json:"rate"`
		Resolution    string `json:"resolution"`
	} `json:"cpus"`
	Batteries []struct {
		Cn, En, Prefix string
	} `json:"batfacotry"`
}

// Parse reads a table.
func Parse(data []byte) (*Table, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	t := &Table{Version: f.Version, colors: map[string][]color{}, models: map[string]Model{}, batteries: map[string]Text{}}
	for _, img := range f.Images {
		for _, st := range img.SupportTypes {
			for _, c := range img.Imgs {
				t.colors[st.ProductType] = append(t.colors[st.ProductType], color{
					back: c.BackColor, front: c.FrontColor,
					backName:  Text{c.BackColorText, c.BackColorText_en},
					frontName: Text{c.FrontColorText, c.FrontColorText_en},
				})
			}
		}
	}
	for _, d := range f.Devices {
		m := t.models[d.ProductType]
		m.ReleaseDate, m.ScreenSize = Text{d.ReleaseDate, d.ReleaseDate_en}, Text{d.ScreenSize, d.ScreenSize_en}
		t.models[d.ProductType] = m
	}
	for _, c := range f.CPUs {
		m := t.models[c.ProductType]
		m.Chip, m.ChipName = Text{c.DisplayName, c.DisplayNameEn}, c.CPU
		m.Cores, m.Frequency, m.Resolution = c.CoreCount, c.Rate, c.Resolution
		t.models[c.ProductType] = m
	}
	for _, b := range f.Batteries {
		if b.Prefix != "" {
			t.batteries[b.Prefix] = Text{b.Cn, b.En}
		}
	}
	if len(t.colors) == 0 && len(t.models) == 0 {
		return nil, errors.New("devtable: no devices in the table")
	}
	return t, nil
}

// Paths are where Find looks, in order: the environment variable's path,
// 3uTools' cache in the Downloads folder, and devices_table/ in the working
// directory.
func Paths() []string {
	var ps []string
	if p := os.Getenv(EnvPath); p != "" {
		ps = append(ps, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		ps = append(ps, filepath.Join(home, "Downloads", "3uToolsDownloads", "cache", "devices_table", "devices_table.txt"))
	}
	return append(ps, filepath.Join("devices_table", "devices_table.txt"))
}

// Find loads the first table of Paths that exists, returning its path; nil
// and no error when there is none.
func Find() (*Table, string, error) {
	for _, p := range Paths() {
		data, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, p, err
		}
		t, err := Parse(data)
		return t, p, err
	}
	return nil, "", nil
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
