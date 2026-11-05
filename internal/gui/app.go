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

	"idevloupe/internal/device"
	"idevloupe/internal/inspect"
	"idevloupe/internal/locales"
	"idevloupe/internal/lockdown"
	"idevloupe/internal/usbmux"
)

type phase int

const (
	phaseConnecting phase = iota
	phasePairing
	phaseInspecting
	phaseDone
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
	// raw is what the inspection read; report is made from it in the
	// window's language, and made again when that changes.
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

func (d *devState) title(lang string) string {
	if d.basic.DeviceName != "" {
		return d.basic.DeviceName
	}
	return locales.In(lang).DeviceFallbackName()
}

func (d *devState) subtitle(lang string) string {
	if r := d.reportIn(lang); r != nil {
		var parts []string
		for _, p := range []string{r.Summary.Marketing, r.Summary.Capacity} {
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
	tab      int
	muxErr   error
	toast    string

	rawOpen map[string]bool

	// setLocale changes the app's language; tests replace it.
	setLocale func(tag string)
}

// New makes the state; Start begins watching for devices.
func New() *App {
	a := &App{rawOpen: map[string]bool{}, setLocale: mygo.App.SetLocale}
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

// start runs an inspection of d in the background.
func (a *App) start(d *devState) {
	if d.cancel != nil {
		d.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	d.phase, d.step, d.err = phaseConnecting, stepOf(locales.Messages.StepConnecting), nil
	go a.inspect(ctx, d, d.dev)
}

// inspect runs on its own goroutine: it reads nothing of d, which the
// main thread owns, and changes it through set. dev is d's device as the
// inspection began.
func (a *App) inspect(ctx context.Context, d *devState, dev usbmux.Device) {
	set := func(fn func()) {
		if ctx.Err() != nil {
			return
		}
		a.update(func() {
			if ctx.Err() == nil {
				fn()
			}
		})
	}
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

	set(func() { d.phase, d.step = phaseInspecting, stepOf(locales.Messages.StepInspecting) })
	raw, err := inspect.Collect(inspect.Reuse(l, func() (*lockdown.Client, error) { return device.Open(dev) }), func(step inspect.Step) {
		set(func() { d.step = func(lang string) string { return step.Text(lang, "") } })
	})
	if err != nil {
		fail(err)
		return
	}
	now := time.Now()
	set(func() {
		d.raw, d.time, d.report = raw, now, nil
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
