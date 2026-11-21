package inspect

import (
	"time"

	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/pkg/plistx"
)

const dataSyncDomain = "com.apple.mobile.data_sync"

type ICloud struct {
	Classes           []SyncClass
	Backup, HasBackup bool
	LastBackup        time.Time
}

func (ic *ICloud) On() bool {
	if ic.Backup {
		return true
	}
	for _, sc := range ic.Classes {
		if sc.On {
			return true
		}
	}
	return false
}

type SyncClass struct {
	Name      string
	On, Known bool
}

// Samples: a signed-in iPhone 12 (iOS 27.2) gives each class
// {AccountNames: [iCloud], Sources: [iCloud]}, emptied when turned off; an
// iPhone X (iOS 16.7) gives the domain empty. Reminders have no key.
var syncClasses = []struct {
	key  string
	name func(locales.Messages) string
}{
	{"Contacts", locales.Messages.ICloudContacts},
	{"Calendars", locales.Messages.ICloudCalendars},
	{"Bookmarks", locales.Messages.ICloudSafari},
	{"Notes", locales.Messages.ICloudNotes},
}

var cocoaEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

func parseICloud(m locales.Messages, raw *RawData) *ICloud {
	v := view{raw}
	ic := &ICloud{}
	sync := raw.Domains[dataSyncDomain]
	known := false
	for _, c := range syncClasses {
		sc := SyncClass{Name: c.name(m)}
		if d, ok := plistx.Dict(sync[c.key]); ok {
			sc.Known, known = true, true
			sources, _ := d["Sources"].([]any)
			for _, s := range sources {
				if s == "iCloud" {
					sc.On = true
				}
			}
		}
		ic.Classes = append(ic.Classes, sc)
	}
	ic.Backup, ic.HasBackup = v.domainBool("com.apple.mobile.backup", "CloudBackupEnabled")
	if x, ok := v.domain("com.apple.mobile.backup", "LastCloudBackupDate"); ok {
		switch t := x.(type) {
		case time.Time:
			ic.LastBackup = t
		default:
			// Seconds since 2001, as an iPhone 12 (iOS 27.2) gives it.
			if n, ok := plistx.Int(x); ok && n > 0 {
				ic.LastBackup = cocoaEpoch.Add(time.Duration(n) * time.Second)
			}
		}
	}
	if !known && !ic.HasBackup {
		return nil
	}
	return ic
}

// Another account's name can be the owner's email: keep only whether it is iCloud.
func scrubAccounts(d map[string]any) {
	for _, x := range d {
		class, ok := plistx.Dict(x)
		if !ok {
			continue
		}
		for _, k := range []string{"AccountNames", "Sources"} {
			list, _ := class[k].([]any)
			for i, e := range list {
				if e != "iCloud" {
					list[i] = "(other)"
				}
			}
		}
	}
}
