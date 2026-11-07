package inspect

import "time"

// Overview is what a device says about itself, shown before it is
// inspected: values only, with no check and no verdict.
type Overview struct {
	Lang    string
	Summary Summary
	// Fields are the facts a buyer looks for first, the same rows for every
	// device so that the info card keeps its shape; Value is "" when the
	// device does not have it or does not say.
	Fields []Field
	// StorageTotal and StorageFree are in bytes; 0 when not read.
	StorageTotal, StorageFree int64
	Battery                   *Battery
	// Storage is how the storage is used and what its controller says;
	// nil when unread.
	Storage *Storage
	// ICloud is what the device keeps in iCloud; nil when it says nothing.
	ICloud *ICloud
	// Sections are all the device information, as the report has it.
	Sections []Section
}

// Describe lays out raw data, as CollectInfo or Collect read it, in the
// language lang. now settles the decade of a legacy serial's date. Data an
// inspection read adds the parts and the jailbreak check to the sections.
func Describe(raw *RawData, now time.Time, lang string) *Overview {
	a := newAnalysis(raw, now, lang)
	a.summary()
	if raw.inspected() {
		a.jailbreak()
	}
	m, v, s := a.m, a.v, a.r.Summary
	o := &Overview{Lang: lang, Summary: s, Battery: parseBattery(raw)}
	o.Sections = buildSections(a, o.Battery)
	o.StorageTotal, _ = v.domainNum("com.apple.disk_usage", "TotalDiskCapacity")
	o.StorageFree, _ = v.domainNum("com.apple.disk_usage", "AmountDataAvailable")
	o.Storage = parseStorage(m, raw)
	o.ICloud = parseICloud(m, raw)

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
	if b := o.Battery; b != nil {
		b.identify(lang, now)
	}
	o.Fields = append(o.Fields, []Field{
		{m.FieldIOSVersion(), s.IOS},
		{m.CheckSerial(), s.Serial},
		{"IMEI", s.IMEI},
		{"IMEI 2", v.str("InternationalMobileEquipmentIdentity2")},
		{m.FieldModelIdentifier(), s.ProductType},
		{m.FieldPartNumber(), s.ModelNumber},
		{m.CheckRegion(), s.Region},
		{m.CheckActivation(), activation},
		{m.CheckFindMy(), findMy},
		{m.FieldManufactured(), a.made()},
	}...)
	return o
}

// made is the week the device was made, as its legacy serial tells; ""
// when it does not.
func (a *analysis) made() string {
	releaseYear := 0
	if a.known {
		releaseYear = a.model.Year
	}
	if d, ok := DecodeSerial(a.v.str("SerialNumber"), releaseYear, a.now); ok {
		return d.Text(a.lang)
	}
	// A randomized serial tells no date. The logic board's serial dates the
	// board, not the device, and is not taken for it.
	return ""
}
