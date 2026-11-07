package inspect

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/pkg/plistx"
)

// Storage is how the storage is used and what the storage controller says
// of itself. Byte counts are 0 when not read.
type Storage struct {
	Total, Free int64
	// System is the system volume; Apps the user apps, bundle and data,
	// known once an inspection listed them (HasApps); Photos the photo
	// library as disk_usage counts it.
	System, Apps, Photos int64
	HasApps              bool
	// Info is the controller's identity: maker, NAND, model, firmware.
	// Limits are its transfer limits, as the NVMe driver reports them.
	Info, Limits []Field
}

// Other is what no category claims: the used space beyond the system, the
// apps and the photos.
func (s *Storage) Other() int64 {
	return max(s.Total-s.Free-s.System-s.Apps-s.Photos, 0)
}

// cellTypes names NAND by the bits it keeps in a cell (cell-type).
var cellTypes = map[int64]string{1: "SLC", 2: "MLC", 3: "TLC", 4: "QLC"}

// parseStorage reads the storage from disk_usage, the apps and the storage
// controller's node (AppleANS2NVMeController on an iPhone X, iOS 16:
// vendor-name and the NAND's description under Controller
// Characteristics, the transfer limits at the top). nil when nothing is
// known of it.
func parseStorage(m locales.Messages, raw *RawData) *Storage {
	v := view{raw}
	s := &Storage{}
	s.Total, _ = v.domainNum("com.apple.disk_usage", "TotalDiskCapacity")
	s.Free, _ = v.domainNum("com.apple.disk_usage", "AmountDataAvailable")
	s.System, _ = v.domainNum("com.apple.disk_usage", "TotalSystemCapacity")
	for _, k := range []string{"PhotoUsage", "CameraUsage"} {
		if n, ok := v.domainNum("com.apple.disk_usage.factory", k); ok {
			s.Photos += n
		}
	}
	if raw.Apps != nil {
		s.HasApps = true
		for _, app := range raw.Apps {
			if app.Type == "User" {
				s.Apps += app.DiskUsage
			}
		}
	}

	node := raw.Parts["storage"]
	chars, _ := plistx.Dict(node["Controller Characteristics"])
	text := func(d map[string]any, key string) string {
		x, ok := d[key]
		if !ok {
			return ""
		}
		if b, ok := x.([]byte); ok {
			str, _ := plistx.String(b)
			return strings.TrimSpace(str)
		}
		return strings.TrimSpace(FormatValue(x))
	}
	cell := ""
	if n, ok := plistx.Int(chars["cell-type"]); ok {
		cell = cellTypes[n]
		if cell == "" {
			cell = strconv.FormatInt(n, 10)
		}
	}
	keep := func(fields ...Field) []Field {
		var out []Field
		for _, f := range fields {
			if f.Value != "" {
				out = append(out, f)
			}
		}
		return out
	}
	s.Info = keep(
		Field{m.StorageVendor(), text(chars, "vendor-name")},
		Field{m.StorageCellType(), cell},
		Field{m.StorageModel(), text(node, "Model Number")},
		Field{m.StorageChip(), text(chars, "chip-id")},
		Field{m.StorageFirmware(), cmp.Or(text(chars, "firmware-version"), text(node, "Firmware Revision"))},
		Field{m.CheckSerial(), text(node, "Serial Number")},
		Field{m.StorageNANDName(), text(chars, "nand-marketing-name")},
		Field{m.StorageMSPVersion(), text(chars, "msp-version")},
		Field{m.StorageEncryption(), text(chars, "Encryption Type")},
	)
	s.Limits = keep(
		Field{"Max Read Bytes", text(node, "IOMaximumSegmentByteCountRead")},
		Field{"Max Write Bytes", text(node, "IOMaximumSegmentByteCountWrite")},
		Field{"Max Read Count", text(node, "IOMaximumSegmentCountRead")},
		Field{"Max Write Count", text(node, "IOMaximumSegmentCountWrite")},
		Field{"Max Write Swap", text(node, "IOMaximumSwapWrite")},
		Field{"Min Segment Align Bytes", text(node, "IOMinimumSegmentAlignmentByteCount")},
		Field{"Min Saturation Bytes", text(node, "IOMinimumSaturationByteCount")},
		Field{"Preferred IO Size", text(chars, "Preferred IO Size")},
	)
	if s.Total <= 0 && len(s.Info) == 0 && len(s.Limits) == 0 {
		return nil
	}
	return s
}
