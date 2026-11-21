package inspect

import (
	"cmp"
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/ac0d3r/idevloupe/internal/devtable"
	"github.com/ac0d3r/idevloupe/internal/locales"
)

// CLCG (cover glass, front) or CLHS (housing, back); the code is the last 32-bit word.
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

func (v view) colors() (front, back string) {
	front = cmp.Or(v.str("DeviceColor"), v.gestaltStr("DeviceColor"), v.sysColor("CLCG"))
	back = cmp.Or(v.str("DeviceEnclosureColor"), v.gestaltStr("DeviceEnclosureColor"), v.sysColor("CLHS"))
	return front, back
}

var Table = devtable.Builtin()

// Codes mean different colors on different models.
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
