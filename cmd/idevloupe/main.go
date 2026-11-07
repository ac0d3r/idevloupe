// Command idevloupe manages iPhones connected by USB from the desktop; for
// now it inspects them.
//
//	idevloupe            open the desktop window
//	idevloupe -list      list connected devices
//	idevloupe -report    print the inspection report of the first device (or -udid)
//	idevloupe -json      the same, as JSON with every raw value
//	idevloupe -dump-ioreg f.plist   probe parts' keys and the IORegistry, with a summary in f.plist.txt
//	-lang en|zh-Hans|de  the language of the report (default: the system's)
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"

	"github.com/ac0d3r/idevloupe/internal/devtable"
	"github.com/ac0d3r/idevloupe/internal/gui"
	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/internal/probe"
	"github.com/ac0d3r/idevloupe/pkg/device"
	"github.com/ac0d3r/idevloupe/pkg/lockdown"
	"github.com/ac0d3r/idevloupe/pkg/usbmux"
	"github.com/ac0d3r/idevloupe/resources"
)

func main() {
	usbmux.ProgName, usbmux.BundleID = "idevloupe", "com.imipy.idevloupe"
	m := locales.In(systemLanguage())
	list := flag.Bool("list", false, m.CliList())
	report := flag.Bool("report", false, m.CliReport())
	asJSON := flag.Bool("json", false, m.CliJSON())
	udid := flag.String("udid", "", m.CliUDID())
	dump := flag.String("dump-ioreg", "", m.CliDump())
	lang := flag.String("lang", "", m.CliLang())
	flag.Parse()
	// 3uTools' device table names colors and chips: the user's own copy,
	// which 3uTools keeps up to date, else the one built in. A table that
	// does not read is said and done without.
	t, path, err := devtable.Find()
	if t == nil && err == nil {
		t, err = devtable.Parse(resources.DevicesTable)
		path = "resources/devices_table.txt"
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, m.CliTableUnread(path, err.Error()))
	} else {
		inspect.Table = t
	}
	if *lang == "" {
		*lang = systemLanguage()
	} else {
		*lang = i18n.Match([]string{*lang}, locales.Catalog.Languages())
	}
	m = locales.In(*lang)
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, m.CliError(inspect.ErrorText(*lang, err)))
		os.Exit(1)
	}

	switch {
	case *list:
		devs, err := usbmux.ListDevices()
		if err != nil {
			fail(err)
		}
		for _, d := range devs {
			b, _ := device.ReadBasic(d)
			fmt.Printf("%s  %-8s %-12s iOS %-8s %s\n", d.UDID, d.ConnectionType, b.ProductType, b.ProductVersion, b.DeviceName)
		}
	case *dump != "":
		if err := dumpIORegistry(*lang, *udid, *dump); err != nil {
			fail(err)
		}
	case *report || *asJSON:
		if err := cli(*lang, *udid, *asJSON); err != nil {
			fail(err)
		}
	default:
		if err := gui.Run(); err != nil {
			log.Fatal(err)
		}
	}
}

// systemLanguage is the language of the catalogs that suits the
// environment's (LC_ALL, LC_MESSAGES, LANG) best, else the catalogs'
// source language. The window asks the system through MyGo instead.
func systemLanguage() string {
	var preferred []string
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v, _, _ := strings.Cut(os.Getenv(k), ".")
		if v != "" && v != "C" && v != "POSIX" {
			preferred = append(preferred, strings.ReplaceAll(v, "_", "-"))
		}
	}
	return i18n.Match(preferred, locales.Catalog.Languages())
}

// openDevice finds the device (the first one, or udid) and opens a trusted
// session with it, pairing first when it does not trust this computer.
func openDevice(lang, udid string) (usbmux.Device, *lockdown.Client, error) {
	m := locales.In(lang)
	devs, err := usbmux.ListDevices()
	if err != nil {
		return usbmux.Device{}, nil, err
	}
	var dev *usbmux.Device
	for i := range devs {
		if udid == "" || devs[i].UDID == udid {
			dev = &devs[i]
			break
		}
	}
	if dev == nil {
		return usbmux.Device{}, nil, errors.New(m.ErrorNoDevice())
	}
	l, err := device.Open(*dev)
	if errors.Is(err, device.ErrNotTrusted) {
		fmt.Fprintln(os.Stderr, m.CliPairing())
		var pairing lockdown.Pairing
		for {
			perr := device.Pair(*dev, &pairing)
			if perr == nil {
				break
			}
			if !errors.Is(perr, lockdown.ErrPairingPending) && !errors.Is(perr, lockdown.ErrPasswordLocked) {
				return *dev, nil, perr
			}
			fmt.Fprintln(os.Stderr, inspect.ErrorText(lang, perr))
			time.Sleep(2 * time.Second)
		}
		l, err = device.Open(*dev)
	}
	return *dev, l, err
}

func cli(lang, udid string, asJSON bool) error {
	dev, l, err := openDevice(lang, udid)
	if err != nil {
		return err
	}
	raw, err := inspect.Collect(inspect.Reuse(l, func() (*lockdown.Client, error) { return device.Open(dev) }), func(s inspect.Step) {
		fmt.Fprintln(os.Stderr, "·", s.Text(lang, ""))
	})
	if err != nil {
		return err
	}
	r := inspect.Analyze(raw, time.Now(), lang)
	if asJSON {
		b, err := r.JSON()
		if err != nil {
			return err
		}
		os.Stdout.Write(append(b, '\n'))
		return nil
	}
	fmt.Print(r.Text())
	return nil
}

// dumpIORegistry runs the probe of keys and the IORegistry, writing the
// responses to path and a summary of them to path.txt.
func dumpIORegistry(lang, udid, path string) error {
	m := locales.In(lang)
	_, l, err := openDevice(lang, udid)
	if err != nil {
		return err
	}
	defer l.Close()
	fmt.Fprintln(os.Stderr, m.CliProbing(len(probe.Keys)*2+len(probe.Queries)))
	rep, err := probe.Probe(l, path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".txt", []byte(probe.Summary(rep, 300)), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, m.CliWrote(path))
	return nil
}
