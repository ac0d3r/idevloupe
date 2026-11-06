package inspect

import (
	"cmp"
	"encoding/binary"
	"strconv"
	"strings"

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

// Color codes are numbers whose meaning differs between models: the same
// space gray is 1 on an iPhone X and 8 on an iPhone 8. Only codes a sample
// settled are named, from what 3uTools names those units; the others show
// as codes. Front codes: 1 is black on both samples.
var (
	frontColors = map[string]func(locales.Messages) string{
		"1": locales.Messages.ColorBlack,
	}
	backColors = map[[2]string]func(locales.Messages) string{
		{"iPhone10,1", "8"}: locales.Messages.ColorSpaceGray, // iPhone 8
		{"iPhone10,4", "8"}: locales.Messages.ColorSpaceGray,
		{"iPhone10,3", "1"}: locales.Messages.ColorSpaceGray, // iPhone X
		{"iPhone10,6", "1"}: locales.Messages.ColorSpaceGray,
	}
)

// colorName names a color code, "" when no sample settled it. A code given
// as "#rrggbb" is its own name.
func colorName(m locales.Messages, productType, code string, front bool) string {
	if strings.HasPrefix(code, "#") {
		return code
	}
	name, ok := frontColors[code]
	if !front {
		name, ok = backColors[[2]string{productType, code}]
	}
	if !ok {
		return ""
	}
	return name(m)
}

// colorText says a color code with its name when it has one: "Space Gray
// (code 1)", else "code 8".
func colorText(m locales.Messages, productType, code string, front bool) string {
	switch {
	case code == "":
		return ""
	case strings.HasPrefix(code, "#"):
		return code
	}
	if name := colorName(m, productType, code, front); name != "" {
		return m.ColorNamed(name, code)
	}
	return m.ColorCode(code)
}
