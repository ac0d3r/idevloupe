package inspect

import "time"

type Overview struct {
	Lang                      string
	Summary                   Summary
	Fields                    []Field
	StorageTotal, StorageFree int64
	Battery                   *Battery
	Storage                   *Storage
	ICloud                    *ICloud
	Sections                  []Section
}

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

func (a *analysis) made() string {
	releaseYear := 0
	if a.known {
		releaseYear = a.model.Year
	}
	if d, ok := DecodeSerial(a.v.str("SerialNumber"), releaseYear, a.now); ok {
		return d.Text(a.lang)
	}
	// A randomized serial tells no date; the logic board's dates the board only.
	return ""
}
