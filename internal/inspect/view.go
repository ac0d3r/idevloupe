package inspect

import (
	"fmt"
	"strings"

	"github.com/ac0d3r/idevloupe/pkg/plistx"
	"github.com/ac0d3r/idevloupe/pkg/syscfg"
)

// view gives typed access to the raw data.
type view struct {
	raw *RawData
}

func (v view) root(key string) (any, bool) {
	x, ok := v.raw.Root[key]
	return x, ok
}

func (v view) str(key string) string {
	x, _ := v.root(key)
	s, _ := plistx.String(x)
	return strings.TrimSpace(s)
}

func (v view) boolean(key string) (bool, bool) {
	x, ok := v.root(key)
	if !ok {
		return false, false
	}
	return plistx.Bool(x)
}

func (v view) num(key string) (int64, bool) {
	x, ok := v.root(key)
	if !ok {
		return 0, false
	}
	return plistx.Int(x)
}

// nvram returns the NonVolatileRAM dictionary of the root domain.
func (v view) nvram() map[string]any {
	x, ok := v.root("NonVolatileRAM")
	if !ok {
		return nil
	}
	m, _ := plistx.Dict(x)
	return m
}

func (v view) domain(d, key string) (any, bool) {
	m, ok := v.raw.Domains[d]
	if !ok {
		return nil, false
	}
	x, ok := m[key]
	return x, ok
}

func (v view) domainBool(d, key string) (bool, bool) {
	x, ok := v.domain(d, key)
	if !ok {
		return false, false
	}
	return plistx.Bool(x)
}

func (v view) domainNum(d, key string) (int64, bool) {
	x, ok := v.domain(d, key)
	if !ok {
		return 0, false
	}
	return plistx.Int(x)
}

func (v view) domainStr(d, key string) string {
	x, _ := v.domain(d, key)
	s, _ := plistx.String(x)
	return s
}

func (v view) gestaltStr(key string) string {
	if v.raw.Gestalt == nil {
		return ""
	}
	s, _ := plistx.String(v.raw.Gestalt[key])
	return strings.TrimSpace(s)
}

func (v view) platformStr(keys ...string) string {
	if v.raw.PlatformReg == nil {
		return ""
	}
	for _, k := range keys {
		if s, ok := plistx.String(v.raw.PlatformReg[k]); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// sysText returns the factory configuration's tag as text.
func (v view) sysText(tag string) string {
	s, _ := syscfg.Text(v.raw.SysCfg[tag])
	return s
}

// sysMAC returns the factory configuration's tag as a MAC address, the
// first 6 of its bytes, written as lockdown writes addresses; "" when unset.
func (v view) sysMAC(tag string) string {
	b := v.raw.SysCfg[tag]
	if len(b) < 6 || string(b[:6]) == "\x00\x00\x00\x00\x00\x00" {
		return ""
	}
	parts := make([]string, 6)
	for i, c := range b[:6] {
		parts[i] = fmt.Sprintf("%02x", c)
	}
	return strings.Join(parts, ":")
}
