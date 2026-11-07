// Package gui is the desktop interface, drawn with MyGo's native UI.
package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/pkg/device"
	"github.com/ac0d3r/idevloupe/pkg/lockdown"
	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

type phase int

const (
	phaseConnecting phase = iota
	phasePairing
	phaseReady // the overview is shown
	phaseInspecting
	phaseDone // the report is shown
	phaseError
)

// devState is one connected device as the window shows it. Only the main
// thread touches it; workers change it through App.update.
type devState struct {
	dev   usbmux.Device
	basic device.Basic
	phase phase
	// step says what the inspection is doing, in a language.
	step func(lang string) string
	err  error
	// info is what was read on connecting; overview is made from it in the
	// window's language, and made again when that changes.
	info     *inspect.RawData
	infoTime time.Time
	overview *inspect.Overview
	// raw is what the inspection read, the report likewise made from it.
	raw    *inspect.RawData
	time   time.Time
	report *inspect.Report
	cancel context.CancelFunc
}

// stepOf says msg in the language asked.
func stepOf(msg func(locales.Messages) string) func(string) string {
	return func(lang string) string { return msg(locales.In(lang)) }
}

// errorStep says err in the language asked.
func errorStep(err error) func(string) string {
	return func(lang string) string { return inspect.ErrorText(lang, err) }
}

// reportIn returns the report in lang, making it from the raw data when
// there is none in that language yet.
func (d *devState) reportIn(lang string) *inspect.Report {
	if d.raw == nil {
		return d.report
	}
	if d.report == nil || d.report.Lang != lang {
		d.report = inspect.Analyze(d.raw, d.time, lang)
		if d.report.Summary.DeviceName != "" {
			d.basic.DeviceName = d.report.Summary.DeviceName
		}
	}
	return d.report
}

// overviewIn returns the overview in lang, made from what was read on
// connecting, or else from the inspection's data.
func (d *devState) overviewIn(lang string) *inspect.Overview {
	raw, at := d.info, d.infoTime
	if d.raw != nil {
		raw, at = d.raw, d.time
	}
	if raw == nil {
		return nil
	}
	if d.overview == nil || d.overview.Lang != lang {
		d.overview = inspect.Describe(raw, at, lang)
		if d.overview.Summary.DeviceName != "" {
			d.basic.DeviceName = d.overview.Summary.DeviceName
		}
	}
	return d.overview
}

func (d *devState) title(lang string) string {
	if d.basic.DeviceName != "" {
		return d.basic.DeviceName
	}
	return locales.In(lang).DeviceFallbackName()
}

func (d *devState) subtitle(lang string) string {
	if o := d.overviewIn(lang); o != nil {
		var parts []string
		for _, p := range []string{o.Summary.Marketing, o.Summary.Capacity} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		return strings.Join(parts, " · ")
	}
	if m, ok := inspect.LookupModel(d.basic.ProductType, lang); ok {
		return m.Name
	}
	return d.basic.ProductType
}

// App is the window's state.
type App struct {
	win *mygo.Window

	// update runs fn on the main thread and redraws; tests replace it.
	update func(fn func())

	devices  []*devState
	selected string // UDID
	muxErr   error
	toast    string

	// hideIDs is whether the report hides the serials and addresses that
	// name the unit, for a screenshot to share.
	hideIDs bool
	// detailsOpen is whether the overview's device details are shown.
	detailsOpen bool
	// batteryOpen is whether the overview's battery details are shown.
	batteryOpen bool
	// storageOpen is whether the overview's storage details are shown.
	storageOpen bool
	// icloudOpen is whether the overview's iCloud details are shown.
	icloudOpen bool

	// setLocale changes the app's language; tests replace it.
	setLocale func(tag string)
}

// New makes the state; Start begins watching for devices.
func New() *App {
	a := &App{setLocale: mygo.App.SetLocale}
	a.update = func(fn func()) {
		if a.win != nil {
			a.win.Update(fn)
		} else {
			fn()
		}
	}
	return a
}

// Run opens the window and runs the app.
func Run() error {
	a := New()
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     windowTitle(mygo.App.Locale()),
			Width:     1180,
			Height:    780,
			MinWidth:  940,
			MinHeight: 620,
			StateKey:  "main",
			Content:   ui.View(a.View),
		})
		mygo.App.OnLocaleChanged(func(lang string) { a.win.SetTitle(windowTitle(lang)) })
		go a.watch()
	})
	return mygo.App.Run()
}

func windowTitle(lang string) string {
	return "iDevLoupe · " + locales.In(lang).AppTagline()
}

func (a *App) find(udid string) *devState {
	for _, d := range a.devices {
		if d.dev.UDID == udid {
			return d
		}
	}
	return nil
}

func (a *App) current() *devState {
	if d := a.find(a.selected); d != nil {
		return d
	}
	if len(a.devices) > 0 {
		return a.devices[0]
	}
	return nil
}

// watch follows usbmuxd's attach and detach events, reconnecting if
// usbmuxd goes away.
func (a *App) watch() {
	for {
		events, err := usbmux.Listen(context.Background())
		if err != nil {
			a.update(func() { a.muxErr = err })
			time.Sleep(3 * time.Second)
			continue
		}
		a.update(func() { a.muxErr = nil })
		for ev := range events {
			a.update(func() { a.handle(ev) })
		}
		// The listener ended: usbmuxd restarted. Forget the devices; the
		// new listener reports those still attached.
		a.update(func() {
			for _, d := range a.devices {
				if d.cancel != nil {
					d.cancel()
				}
			}
			a.devices = nil
		})
		time.Sleep(time.Second)
	}
}

func (a *App) handle(ev usbmux.Event) {
	if !ev.Attached {
		for i, d := range a.devices {
			if d.dev.DeviceID == ev.Device.DeviceID {
				if d.cancel != nil {
					d.cancel()
				}
				a.devices = append(a.devices[:i], a.devices[i+1:]...)
				return
			}
		}
		return
	}
	if existing := a.find(ev.Device.UDID); existing != nil {
		// Prefer USB over a Wi-Fi entry of the same device.
		if existing.dev.ConnectionType != "USB" && ev.Device.ConnectionType == "USB" {
			existing.dev = ev.Device
		}
		return
	}
	d := &devState{dev: ev.Device}
	a.devices = append(a.devices, d)
	if a.selected == "" {
		a.selected = d.dev.UDID
	}
	a.start(d)
}

// start connects to d in the background, pairing if need be, and reads
// what its overview shows.
func (a *App) start(d *devState) {
	d.info, d.overview, d.raw, d.report = nil, nil, nil, nil
	d.phase, d.step, d.err = phaseConnecting, stepOf(locales.Messages.StepConnecting), nil
	ctx, set := a.run(d)
	go a.connect(ctx, set, d, d.dev)
}

// inspectDevice runs an inspection of d in the background.
func (a *App) inspectDevice(d *devState) {
	d.phase, d.step, d.err = phaseInspecting, stepOf(locales.Messages.StepInspecting), nil
	_, set := a.run(d)
	go a.inspect(set, d, d.dev)
}

// run cancels what runs for d and returns the context of the work that
// replaces it, and its setter, which changes d on the main thread while
// the work is current.
func (a *App) run(d *devState) (context.Context, func(fn func())) {
	if d.cancel != nil {
		d.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	return ctx, func(fn func()) {
		if ctx.Err() != nil {
			return
		}
		a.update(func() {
			if ctx.Err() == nil {
				fn()
			}
		})
	}
}

// connect runs on its own goroutine: it reads nothing of d, which the
// main thread owns, and changes it through set. dev is d's device as the
// work began.
func (a *App) connect(ctx context.Context, set func(func()), d *devState, dev usbmux.Device) {
	fail := func(err error) {
		set(func() { d.phase, d.err = phaseError, err })
	}

	if b, err := device.ReadBasic(dev); err == nil {
		set(func() { d.basic = b })
	}

	var l *lockdown.Client
	for {
		var err error
		l, err = device.Open(dev)
		if err == nil {
			break
		}
		if !errors.Is(err, device.ErrNotTrusted) {
			fail(err)
			return
		}
		// Ask for trust, then keep asking while the alert is up.
		set(func() { d.phase, d.step = phasePairing, errorStep(lockdown.ErrPairingPending) })
		var pairing lockdown.Pairing
		for {
			err = device.Pair(dev, &pairing)
			if err == nil {
				break
			}
			switch {
			case errors.Is(err, lockdown.ErrPairingPending), errors.Is(err, lockdown.ErrPasswordLocked):
				// err changes on the next turn: the main thread gets this one.
				step := errorStep(err)
				set(func() { d.step = step })
			default:
				fail(err)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(1500 * time.Millisecond):
			}
		}
		// Paired: open the session on the next turn of the loop.
	}

	set(func() { d.phase, d.step = phaseConnecting, stepOf(locales.Messages.StepDeviceInfo) })
	info, err := inspect.CollectInfo(inspect.Reuse(l, func() (*lockdown.Client, error) { return device.Open(dev) }))
	if err != nil {
		fail(err)
		return
	}
	now := time.Now()
	set(func() {
		d.info, d.infoTime, d.overview = info, now, nil
		d.phase = phaseReady
	})
}

// inspect runs on its own goroutine, as connect does.
func (a *App) inspect(set func(func()), d *devState, dev usbmux.Device) {
	l, err := device.Open(dev)
	if err != nil {
		set(func() { d.phase, d.err = phaseError, err })
		return
	}
	raw, err := inspect.Collect(inspect.Reuse(l, func() (*lockdown.Client, error) { return device.Open(dev) }), func(step inspect.Step) {
		set(func() { d.step = func(lang string) string { return step.Text(lang, "") } })
	})
	if err != nil {
		set(func() { d.phase, d.err = phaseError, err })
		return
	}
	now := time.Now()
	set(func() {
		d.raw, d.time, d.report, d.overview = raw, now, nil, nil
		d.phase = phaseDone
	})
}

func (a *App) copyReport(r *inspect.Report) {
	m := locales.In(r.Lang)
	if err := mygo.Clipboard.Write(transfer.TextData(r.Text())); err != nil {
		a.toast = m.ToastCopyFailed(err.Error())
		return
	}
	a.toast = m.ToastCopied()
}

// exporting is held while the save dialog is open, so that a second
// click opens no second dialog.
var exporting sync.Mutex

func (a *App) exportReport(r *inspect.Report) {
	go func() {
		if !exporting.TryLock() {
			return
		}
		defer exporting.Unlock()
		m := locales.In(r.Lang)
		name := m.ExportFileName()
		if r.Summary.Serial != "" {
			name += "-" + r.Summary.Serial
		}
		name += "-" + r.Time.Format("20060102-1504") + ".txt"
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      a.win,
			Title:       m.ExportTitle(),
			DefaultPath: name,
			Filters: []mygo.FileFilter{
				{Name: m.ExportFilterText(), Extensions: []string{"txt"}},
				{Name: m.ExportFilterJSON(), Extensions: []string{"json"}},
			},
		})
		if err != nil || path == "" {
			return
		}
		var data []byte
		if strings.EqualFold(filepath.Ext(path), ".json") {
			data, err = r.JSON()
		} else {
			data = []byte(r.Text())
		}
		if err == nil {
			err = os.WriteFile(path, data, 0o644)
		}
		msg := m.ToastExported(path)
		if err != nil {
			msg = m.ToastExportFailed(err.Error())
		}
		a.update(func() { a.toast = msg })
	}()
}
