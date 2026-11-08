package devtable

import "testing"

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
