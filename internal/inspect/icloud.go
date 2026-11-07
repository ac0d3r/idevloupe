package inspect

import (
	"time"

	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/pkg/plistx"
)

const dataSyncDomain = "com.apple.mobile.data_sync"

// ICloud is what the device says it keeps in iCloud: the data classes it
// syncs there, and the iCloud backup.
type ICloud struct {
	// Classes are the data classes, in the order iOS's settings list them.
	Classes []SyncClass
	// Backup is whether iCloud backup is on, when HasBackup; LastBackup
	// when it last ran, zero if never or unread.
	Backup, HasBackup bool
	LastBackup        time.Time
}

// On is whether the device uses iCloud at all: its backup or any data
// class syncing with it.
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

// SyncClass is one data class and whether iCloud syncs it, when Known.
type SyncClass struct {
	Name      string
	On, Known bool
}

// syncClasses are the data_sync keys the details show, with their names.
// Samples, an iPhone 12 (iOS 27.2) signed in to iCloud: each class is
// {AccountNames: [iCloud], Sources: [iCloud]}, and turning Contacts off in
// Settings empties both lists. An iPhone X (iOS 16.7) reports the domain
// empty. Reminders have no key.
var syncClasses = []struct {
	key  string
	name func(locales.Messages) string
}{
	{"Contacts", locales.Messages.ICloudContacts},
	{"Calendars", locales.Messages.ICloudCalendars},
	{"Bookmarks", locales.Messages.ICloudSafari},
	{"Notes", locales.Messages.ICloudNotes},
}

// cocoaEpoch is where Apple's absolute times count from.
var cocoaEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// parseICloud reads data_sync and the backup domain; nil when neither says
// anything of iCloud.
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

// scrubAccounts keeps of data_sync's account names and sources only
// whether one is iCloud: another account's name can be its owner's email
// address, and reports are handed to the other party of a sale.
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
