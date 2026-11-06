package inspect

import (
	"fmt"
	"math"
	"time"
)

// Overview is what a device says about itself, shown before it is
// inspected: values only, with no check and no verdict.
type Overview struct {
	Lang    string
	Summary Summary
	// Fields are the facts a buyer looks for first.
	Fields []Field
	// StorageTotal and StorageFree are in bytes; 0 when not read.
	StorageTotal, StorageFree int64
	Battery                   *Battery
	// Sections are all the device information, as the report has it.
	Sections []Section
}

// Describe lays out raw data, as CollectInfo or Collect read it, in the
// language lang. now settles the decade of a legacy serial's date.
func Describe(raw *RawData, now time.Time, lang string) *Overview {
	a := newAnalysis(raw, now, lang)
	a.summary()
	m, v, s := a.m, a.v, a.r.Summary
	o := &Overview{Lang: lang, Summary: s, Battery: parseBattery(raw), Sections: buildSections(m, v, s)}
	o.StorageTotal, _ = v.domainNum("com.apple.disk_usage", "TotalDiskCapacity")
	o.StorageFree, _ = v.domainNum("com.apple.disk_usage", "AmountDataAvailable")

	activation := v.str("ActivationState")
	switch activation {
	case "Activated":
		activation = m.ActivationActivated()
	case "Unactivated":
		activation = m.ActivationUnactivated()
	case "FactoryActivated":
		activation = m.ActivationFactory()
	}
	findMy := ""
	if on, ok, _ := a.findMyState(); ok {
		findMy = m.ValueOff()
		if on {
			findMy = m.FindMyOn()
		}
	}
	made := ""
	releaseYear := 0
	if a.known {
		releaseYear = a.model.Year
	}
	if d, ok := DecodeSerial(s.Serial, releaseYear, now); ok {
		made = d.Text(lang)
	}
	health, cycles := "", ""
	if b := o.Battery; b != nil {
		if h := b.Health(); h > 0 {
			health = m.ValuePercent(int(math.Round(h)))
		}
		if b.HasCycleCount {
			cycles = m.ValueCycles(int(b.CycleCount))
		}
	}
	udid := v.str("UniqueDeviceID")
	if udid == "" {
		udid = raw.UDID
	}
	ecid := ""
	if n, ok := v.num("UniqueChipID"); ok {
		ecid = fmt.Sprintf("%X", uint64(n))
	}
	for _, f := range []Field{
		{m.FieldIOSVersion(), s.IOS},
		{m.CheckActivation(), activation},
		{m.FieldModelIdentifier(), s.ProductType},
		{m.CheckFindMy(), findMy},
		{m.FieldPartNumber(), s.ModelNumber},
		{m.CheckRegion(), s.Region},
		{m.CheckSerial(), s.Serial},
		{m.FieldManufactured(), made},
		{"IMEI", s.IMEI},
		{m.CheckBatteryHealth(), health},
		{"ECID", ecid},
		{m.CheckCycles(), cycles},
		{"UDID", udid},
	} {
		if f.Value != "" {
			o.Fields = append(o.Fields, f)
		}
	}
	return o
}
