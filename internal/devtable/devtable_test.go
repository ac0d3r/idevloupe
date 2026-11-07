package devtable

import (
	"os"
	"path/filepath"
	"testing"
)

// sample is a table in 3uTools' layout, cut down to what is read.
const sample = `{"version":"2026.10.23.01",
"Images":[{"SupportTypes":[{"ProductType":"iPhone13,1"},{"ProductType":"iPhone13,2"}],"imgs":[
 {"BackColor":"1","BackColorText":"黑色","BackColorText_en":"Black","FrontColor":"1","FrontColorText":"黑色","FrontColorText_en":"Black"},
 {"BackColor":"9","BackColorText":"蓝色","BackColorText_en":"Blue","FrontColor":"1","FrontColorText":"黑色","FrontColorText_en":"Black"}]}],
"devices":[{"ProductType":"iPhone13,2","ReleaseDate":"2020年10月13日","ReleaseDate_en":"10/13/2020","ScreenSize":"6.1 英寸","ScreenSize_en":"6.1 inches"}],
"cpus":[{"productType":"iPhone13,2","cpu":"Apple A14","displayName":"A14 六核","displayName_en":"A14 Hexa","coreCount":"6","rate":"3.1GHz","resolution":"2532 x 1170"}],
"batfacotry":[{"cn":"常熟新普","en":"SIMPLO","prefix":"FG"}]}`

func TestParse(t *testing.T) {
	tb, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := tb.BackColor("iPhone13,2", "9", "zh-Hans"); !ok || c != "蓝色" {
		t.Errorf("back color %q %v", c, ok)
	}
	if c, _ := tb.BackColor("iPhone13,1", "1", "de"); c != "Black" {
		t.Errorf("German falls back to English: %q", c)
	}
	if _, ok := tb.BackColor("iPhone13,2", "7", "en"); ok {
		t.Error("an unknown code named")
	}
	if c, _ := tb.FrontColor("iPhone13,2", "1", "en"); c != "Black" {
		t.Errorf("front %q", c)
	}
	m, ok := tb.Model("iPhone13,2")
	if !ok || m.Chip.In("en") != "A14 Hexa" || m.Cores != "6" || m.ReleaseDate.In("zh-Hans") != "2020年10月13日" || m.ScreenSize.In("en") != "6.1 inches" {
		t.Errorf("model %+v", m)
	}
	if b, ok := tb.BatteryMaker("FG99425S3TCZWQLAH", "zh-Hans"); !ok || b != "常熟新普" {
		t.Errorf("battery maker %q", b)
	}
	if _, ok := tb.BatteryMaker("B055422L9MESM7PEQ", "en"); ok {
		t.Error("an unknown battery maker named")
	}
}

// A nil table finds nothing, so that the program runs without one.
func TestNil(t *testing.T) {
	var tb *Table
	if _, ok := tb.BackColor("iPhone13,2", "1", "en"); ok {
		t.Error("nil table named a color")
	}
	if _, ok := tb.Model("iPhone13,2"); ok {
		t.Error("nil table has a model")
	}
	if _, ok := tb.BatteryMaker("FG99", "en"); ok {
		t.Error("nil table has a battery maker")
	}
}

func TestFind(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.txt")
	if err := os.WriteFile(p, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvPath, p)
	tb, got, err := Find()
	if err != nil || got != p || tb.Version != "2026.10.23.01" {
		t.Errorf("Find: %v %q %v", tb, got, err)
	}
	if err := os.WriteFile(p, []byte(`{"version":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Find(); err == nil {
		t.Error("a table with no devices loaded")
	}
}

// The built-in data knows the sample devices.
func TestBuiltin(t *testing.T) {
	tb := Builtin()
	if m, ok := tb.Model("iPhone10,1"); !ok || m.Cores != "6" {
		t.Errorf("iPhone 8: %+v, %v", m, ok)
	}
	if m, _ := tb.Model("iPhone13,2"); m.Chip.In("en") != "A14 Hexa" || m.ScreenSize.In("en") != "6.1 inches" {
		t.Errorf("iPhone 12: %+v", m)
	}
	if name, ok := tb.BackColor("iPhone10,1", "8", "zh-Hans"); !ok || name == "" {
		t.Errorf("iPhone 8 back color 8: %q, %v", name, ok)
	}
	if name, ok := tb.BatteryMaker("FG99425S3TCZWQLAH", "zh-Hans"); !ok || name != "常熟新普" {
		t.Errorf("battery maker: %q, %v", name, ok)
	}
}
