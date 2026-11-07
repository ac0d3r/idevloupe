package resources

import (
	"testing"

	"github.com/ac0d3r/idevloupe/internal/devtable"
)

// The built-in table reads and knows the sample devices.
func TestDevicesTable(t *testing.T) {
	tab, err := devtable.Parse(DevicesTable)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := tab.Model("iPhone10,1"); !ok || m.Cores != "6" {
		t.Errorf("iPhone 8: %+v, %v", m, ok)
	}
	if name, ok := tab.BackColor("iPhone10,1", "8", "zh-Hans"); !ok || name == "" {
		t.Errorf("iPhone 8 back color 8: %q, %v", name, ok)
	}
	if name, ok := tab.BatteryMaker("FG99425SCDAHXYDA2", "zh-Hans"); !ok || name == "" {
		t.Errorf("battery maker: %q, %v", name, ok)
	}
}
