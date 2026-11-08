package inspect

import (
	"cmp"
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/ac0d3r/idevloupe/internal/devtable"
	"github.com/ac0d3r/idevloupe/internal/locales"
)

// sysColor reads a color code of the factory record: CLCG (the cover
// glass, the front) or CLHS (the housing, the back), whose last 32-bit word
// is the code.
func (v view) sysColor(tag string) string {
	b := v.raw.SysCfg[tag]
	if len(b) != 16 {
		return ""
	}
	if n := binary.LittleEndian.Uint32(b[12:]); n != 0 {
		return strconv.FormatUint(uint64(n), 10)
	}
	return ""
}

// colors returns the codes of the front and back colors: lockdown's, else
// MobileGestalt's, else the factory record's.
func (v view) colors() (front, back string) {
	front = cmp.Or(v.str("DeviceColor"), v.gestaltStr("DeviceColor"), v.sysColor("CLCG"))
	back = cmp.Or(v.str("DeviceEnclosureColor"), v.gestaltStr("DeviceEnclosureColor"), v.sysColor("CLHS"))
	return front, back
}

// Table is the device table; nil, it adds nothing.
var Table = devtable.Builtin()

// colorName names a color code in lang by the device table, as codes mean
// different colors on different models; "" when it does not know it. A
// code given as "#rrggbb" is its own name.
func colorName(lang, productType, code string, front bool) string {
	if strings.HasPrefix(code, "#") {
		return code
	}
	lookup := Table.BackColor
	if front {
		lookup = Table.FrontColor
	}
	name, _ := lookup(productType, code, lang)
	return name
}

// colorText says a color code with its name when it has one: "Space Gray
// (code 1)", else "code 8".
func colorText(m locales.Messages, lang, productType, code string, front bool) string {
	switch {
	case code == "":
		return ""
	case strings.HasPrefix(code, "#"):
		return code
	}
	if name := colorName(lang, productType, code, front); name != "" {
		return m.ColorNamed(name, code)
	}
	return m.ColorCode(code)
}
