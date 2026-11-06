package inspect

import (
	"fmt"
	"strings"
	"time"

	"github.com/ac0d3r/idevloupe/internal/locales"
)

// Model is what an identifier such as "iPhone16,1" is.
type Model struct {
	Name string
	Year int // year of release
}

// models maps ProductType to the marketing name and release year. Unknown
// identifiers fall back to MobileGestalt's description or the raw value.
var models = map[string]Model{
	"iPhone8,1":  {"iPhone 6s", 2015},
	"iPhone8,2":  {"iPhone 6s Plus", 2015},
	"iPhone8,4":  {"iPhone SE (1st generation)", 2016},
	"iPhone9,1":  {"iPhone 7", 2016},
	"iPhone9,3":  {"iPhone 7", 2016},
	"iPhone9,2":  {"iPhone 7 Plus", 2016},
	"iPhone9,4":  {"iPhone 7 Plus", 2016},
	"iPhone10,1": {"iPhone 8", 2017},
	"iPhone10,4": {"iPhone 8", 2017},
	"iPhone10,2": {"iPhone 8 Plus", 2017},
	"iPhone10,5": {"iPhone 8 Plus", 2017},
	"iPhone10,3": {"iPhone X", 2017},
	"iPhone10,6": {"iPhone X", 2017},
	"iPhone11,2": {"iPhone XS", 2018},
	"iPhone11,4": {"iPhone XS Max", 2018},
	"iPhone11,6": {"iPhone XS Max", 2018},
	"iPhone11,8": {"iPhone XR", 2018},
	"iPhone12,1": {"iPhone 11", 2019},
	"iPhone12,3": {"iPhone 11 Pro", 2019},
	"iPhone12,5": {"iPhone 11 Pro Max", 2019},
	"iPhone12,8": {"iPhone SE (2nd generation)", 2020},
	"iPhone13,1": {"iPhone 12 mini", 2020},
	"iPhone13,2": {"iPhone 12", 2020},
	"iPhone13,3": {"iPhone 12 Pro", 2020},
	"iPhone13,4": {"iPhone 12 Pro Max", 2020},
	"iPhone14,4": {"iPhone 13 mini", 2021},
	"iPhone14,5": {"iPhone 13", 2021},
	"iPhone14,2": {"iPhone 13 Pro", 2021},
	"iPhone14,3": {"iPhone 13 Pro Max", 2021},
	"iPhone14,6": {"iPhone SE (3rd generation)", 2022},
	"iPhone14,7": {"iPhone 14", 2022},
	"iPhone14,8": {"iPhone 14 Plus", 2022},
	"iPhone15,2": {"iPhone 14 Pro", 2022},
	"iPhone15,3": {"iPhone 14 Pro Max", 2022},
	"iPhone15,4": {"iPhone 15", 2023},
	"iPhone15,5": {"iPhone 15 Plus", 2023},
	"iPhone16,1": {"iPhone 15 Pro", 2023},
	"iPhone16,2": {"iPhone 15 Pro Max", 2023},
	"iPhone17,3": {"iPhone 16", 2024},
	"iPhone17,4": {"iPhone 16 Plus", 2024},
	"iPhone17,1": {"iPhone 16 Pro", 2024},
	"iPhone17,2": {"iPhone 16 Pro Max", 2024},
	"iPhone17,5": {"iPhone 16e", 2025},
	"iPhone18,3": {"iPhone 17", 2025},
	"iPhone18,4": {"iPhone Air", 2025},
	"iPhone18,1": {"iPhone 17 Pro", 2025},
	"iPhone18,2": {"iPhone 17 Pro Max", 2025},
}

// HasFaceID reports whether a model has Face ID: every iPhone from the
// iPhone X on, except the iPhone SE (2nd and 3rd generation), which have
// Touch ID. Unknown identifiers report false.
func HasFaceID(productType string) bool {
	m, ok := models[productType]
	if !ok {
		return false
	}
	switch productType {
	case "iPhone12,8", "iPhone14,6":
		return false
	}
	return m.Year >= 2018 || productType == "iPhone10,3" || productType == "iPhone10,6"
}

// HasTouchID reports whether a model has Touch ID: the iPhone 6s to the
// iPhone 8 Plus and the iPhone SE of every generation. Unknown identifiers
// report false.
func HasTouchID(productType string) bool {
	switch productType {
	case "iPhone8,1", "iPhone8,2", "iPhone8,4", "iPhone9,1", "iPhone9,2", "iPhone9,3", "iPhone9,4",
		"iPhone10,1", "iPhone10,2", "iPhone10,4", "iPhone10,5", "iPhone12,8", "iPhone14,6":
		return true
	}
	return false
}

// localNames are the names of models that differ between languages.
var localNames = map[string]func(locales.Messages) string{
	"iPhone8,4":  locales.Messages.ModelSE1,
	"iPhone12,8": locales.Messages.ModelSE2,
	"iPhone14,6": locales.Messages.ModelSE3,
}

// LookupModel returns the model of a ProductType, named in the language
// lang.
func LookupModel(productType, lang string) (Model, bool) {
	m, ok := models[productType]
	if name, local := localNames[productType]; local {
		m.Name = name(locales.In(lang))
	}
	return m, ok
}

// regions maps the region part of a part number ("CH" of "MQ8X3CH/A") to
// where the device was sold. Only codes whose meaning is well established
// are listed; others are shown raw.
var regions = map[string]func(locales.Messages) string{
	"CH": locales.Messages.RegionCH,
	"ZP": locales.Messages.RegionZP,
	"TA": locales.Messages.RegionTA,
	"LL": locales.Messages.RegionLL,
	"J":  locales.Messages.RegionJ,
	"KH": locales.Messages.RegionKH,
	"ZA": locales.Messages.RegionZA,
	"B":  locales.Messages.RegionB,
	"C":  locales.Messages.RegionC,
	"CL": locales.Messages.RegionC,
	"X":  locales.Messages.RegionX,
	"FD": locales.Messages.RegionFD,
	"ZD": locales.Messages.RegionZD,
	"T":  locales.Messages.RegionT,
	"TY": locales.Messages.RegionT,
	"Y":  locales.Messages.RegionY,
	"RU": locales.Messages.RegionRU,
	"HN": locales.Messages.RegionHN,
	"TH": locales.Messages.RegionTH,
	"MY": locales.Messages.RegionMY,
	"PP": locales.Messages.RegionPP,
	"ID": locales.Messages.RegionID,
	"VN": locales.Messages.RegionVN,
	"AE": locales.Messages.RegionAE,
	"AB": locales.Messages.RegionAB,
	"BR": locales.Messages.RegionBR,
	"BZ": locales.Messages.RegionBR,
	"LZ": locales.Messages.RegionLZ,
	"E":  locales.Messages.RegionE,
	"TU": locales.Messages.RegionTU,
	"PL": locales.Messages.RegionPL,
	"KN": locales.Messages.RegionKN,
	"KS": locales.Messages.RegionKS,
}

// RegionName describes a RegionInfo value such as "CH/A" in the language
// lang.
func RegionName(regionInfo, lang string) (string, bool) {
	code, _, _ := strings.Cut(strings.TrimSpace(regionInfo), "/")
	name, ok := regions[strings.ToUpper(code)]
	if !ok {
		return "", false
	}
	return name(locales.In(lang)), true
}

// UnitKind is what the first letter of a part number says about the unit.
type UnitKind struct {
	Letter string
	Name   string
	Status Status
	Detail string
}

// UnitKindOf decodes the first character of a part number (ModelNumber),
// describing it in the language lang.
func UnitKindOf(modelNumber, lang string) UnitKind {
	m := locales.In(lang)
	if modelNumber == "" {
		return UnitKind{Status: Unknown, Name: m.UnitUnknown(), Detail: m.KeyNotRead("ModelNumber")}
	}
	l := strings.ToUpper(modelNumber[:1])
	switch l {
	case "M":
		return UnitKind{l, m.UnitRetail(), Pass, m.UnitRetailDetail()}
	case "F":
		return UnitKind{l, m.UnitRefurbished(), Warn, m.UnitRefurbishedDetail()}
	case "N":
		return UnitKind{l, m.UnitReplacement(), Warn, m.UnitReplacementDetail()}
	case "P":
		return UnitKind{l, m.UnitEngraved(), Info, m.UnitEngravedDetail()}
	case "3":
		return UnitKind{l, m.UnitDemo(), Fail, m.UnitDemoDetail()}
	}
	return UnitKind{l, m.UnitOther(), Warn, m.UnitOtherDetail(l)}
}

// SerialDate is a manufacture date decoded from a legacy 12-character serial.
type SerialDate struct {
	Year      int
	Week      int // 1..53
	Ambiguous bool
}

// Text says the week in the language lang.
func (d SerialDate) Text(lang string) string {
	m := locales.In(lang)
	if d.Ambiguous {
		return m.SerialWeekAmbiguous(d.Week, d.Year)
	}
	return m.SerialWeek(d.Week, d.Year)
}

const (
	serialYearCodes = "CDFGHJKLMNPQRSTVWXYZ"
	serialWeekCodes = "123456789CDFGHJKLMNPQRTVWXY"
)

// DecodeSerial decodes the manufacture week of the 12-character serials
// Apple used until 2021. It returns false for randomized serials (10
// characters, from 2021 on) and anything it cannot decode. releaseYear, when
// known, settles which decade the year letter means.
func DecodeSerial(serial string, releaseYear int, now time.Time) (SerialDate, bool) {
	serial = strings.ToUpper(strings.TrimSpace(serial))
	if len(serial) != 12 {
		return SerialDate{}, false
	}
	yi := strings.IndexByte(serialYearCodes, serial[3])
	wi := strings.IndexByte(serialWeekCodes, serial[4])
	if yi < 0 || wi < 0 {
		return SerialDate{}, false
	}
	half := yi % 2
	base := yi / 2
	week := wi + 1 + half*26
	if week > 53 {
		return SerialDate{}, false
	}
	var cands []int
	for decade := 2010; decade <= now.Year(); decade += 10 {
		y := decade + base
		if y > now.Year() {
			continue
		}
		// Production can start some months ahead of the launch.
		if releaseYear > 0 && y < releaseYear-1 {
			continue
		}
		cands = append(cands, y)
	}
	if len(cands) == 0 {
		return SerialDate{}, false
	}
	return SerialDate{Year: cands[0], Week: week, Ambiguous: releaseYear == 0 && len(cands) > 1}, true
}

// LuhnValid checks the check digit of an IMEI.
func LuhnValid(s string) bool {
	if len(s) < 2 {
		return false
	}
	sum := 0
	double := false
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
		d := int(c - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// NominalCapacity rounds a disk size in bytes up to the marketed capacity.
func NominalCapacity(bytes int64) string {
	gb := float64(bytes) / 1e9
	for _, n := range []int{8, 16, 32, 64, 128, 256, 512, 1024, 2048} {
		if gb <= float64(n) {
			if n >= 1024 {
				return fmt.Sprintf("%d TB", n/1024)
			}
			return fmt.Sprintf("%d GB", n)
		}
	}
	return fmt.Sprintf("%.0f GB", gb)
}

// jailbreakApps are bundle identifiers of package managers, jailbreak
// apps and tools that only install on modified systems.
var jailbreakApps = map[string]string{
	"com.saurik.Cydia":         "Cydia",
	"org.coolstar.SileoStore":  "Sileo",
	"org.coolstar.sileo":       "Sileo",
	"xyz.willy.Zebra":          "Zebra",
	"me.apptapp.installer":     "Installer",
	"com.opa334.TrollStore":    "TrollStore",
	"com.opa334.Dopamine":      "Dopamine",
	"com.tigisoftware.Filza":   "Filza",
	"science.xnu.undecimus":    "unc0ver",
	"com.electrateam.chimera":  "Chimera",
	"org.coolstar.electra1141": "Electra",
	"org.coolstar.odyssey":     "Odyssey",
	"kjc.loader":               "checkra1n Loader",
	"com.samiiau.loader":       "palera1n Loader",
}
