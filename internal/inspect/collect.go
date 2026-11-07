package inspect

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"

	"github.com/ac0d3r/idevloupe/internal/locales"
	"github.com/ac0d3r/idevloupe/pkg/lockdown"
	"github.com/ac0d3r/idevloupe/pkg/services"
	"github.com/ac0d3r/idevloupe/pkg/syscfg"
)

// Domains read besides the root domain.
var Domains = []string{
	"com.apple.disk_usage",
	"com.apple.disk_usage.factory",
	"com.apple.mobile.battery",
	"com.apple.fmip",
	"com.apple.mobile.chaperone",
	"com.apple.PurpleBuddy",
	"com.apple.mobile.backup",
	dataSyncDomain,
	"com.apple.international",
	"com.apple.mobile.software_behavior",
	"com.apple.mobile.wireless_lockdown",
	"com.apple.mobile.internal",
	"com.apple.mobile.restriction",
	"com.apple.xcode.developerdomain",
	"com.apple.security.mac.amfi",
}

// rootKeys are root-domain keys the checks use. lockdownd leaves some
// keys out of the whole-domain answer, so those missing are asked for one by
// one.
var rootKeys = []string{
	"ProductionSOC", "ReleaseType", "ModelNumber", "RegionInfo", "SerialNumber",
	"MLBSerialNumber", "BasebandVersion", "BasebandStatus", "NonVolatileRAM",
	"BrickState", "FusingStatus", "ActivationState", "TelephonyCapability",
	"InternationalMobileEquipmentIdentity", "InternationalMobileEquipmentIdentity2",
	"SIMTrayStatus", "BasebandSerialNumber",
}

// gestaltKeys are MobileGestalt keys read where the device still answers.
var gestaltKeys = []string{
	"ArtworkDeviceProductDescription",
	"HWModelStr",
	"SerialNumber",
	"ModelNumber",
	"RegionInfo",
	"DeviceColor",
	"DeviceEnclosureColor",
	"ReleaseType",
	"ProductionSOC",
	// The factory configuration, with the serial numbers of the parts
	// fitted at the factory: parsed into RawData.SysCfg.
	"SysCfg",
}

// RawData is everything read from a device, before analysis.
type RawData struct {
	UDID    string
	Root    map[string]any
	Domains map[string]map[string]any

	BatteryReg  map[string]any
	PlatformReg map[string]any
	Gestalt     map[string]any
	GestaltErr  error
	// SysCfg is the factory configuration by tag, from MobileGestalt; nil
	// when it was not read (SysCfgErr says why, if it failed to parse).
	SysCfg    map[string][]byte
	SysCfgErr error
	// DiagAll is the "All" diagnostics: GasGauge, NAND, WiFi, HDMI.
	DiagAll map[string]any
	// Parts are the IORegistry entries of parts (cameras, Face ID, display,
	// touch, storage), by the lookup that found them.
	Parts map[string]map[string]any

	Apps    []services.App
	AppsErr error

	AFC2    *bool
	AFC2Err error

	MC    *services.MCInfo
	MCErr error

	// Analytics is the battery state of iOS's analytics logs.
	Analytics     *services.AnalyticsBattery
	AnalyticsScan *services.AnalyticsScan
	AnalyticsErr  error

	// Errors are the steps that failed, for the report.
	Errors []*StepError
}

// inspected tells whether raw is an inspection's, which reads the apps
// and AFC2, rather than CollectInfo's.
func (raw *RawData) inspected() bool {
	return raw.Apps != nil || raw.AppsErr != nil || raw.AFC2 != nil
}

func (raw *RawData) fail(step Step, target string, err error) {
	raw.Errors = append(raw.Errors, &StepError{Step: step, Target: target, Err: err})
}

// Step is a step of the collection, which Collect reports as it begins
// it, or which failed (StepError).
type Step int

// The steps Collect reports as it goes, and those that may fail.
const (
	StepDeviceInfo Step = iota
	StepDiagnostics
	StepAnalytics
	StepApps
	StepJailbreak
	StepProfiles

	StepReadDomain // Target is the domain
	StepReadBattery
	StepStartDiagnostics
	StepReadApps
	StepReadProfiles
	StepReconnect
)

// Text names the step in the language lang; target is the StepError's.
func (s Step) Text(lang, target string) string {
	m := locales.In(lang)
	switch s {
	case StepDeviceInfo:
		return m.StepDeviceInfo()
	case StepDiagnostics:
		return m.StepDiagnostics()
	case StepAnalytics:
		return m.StepAnalytics()
	case StepApps:
		return m.StepApps()
	case StepJailbreak:
		return m.StepJailbreak()
	case StepProfiles:
		return m.StepProfiles()
	case StepReadDomain:
		return m.StepReadDomain(target)
	case StepReadBattery:
		return m.StepReadBattery()
	case StepStartDiagnostics:
		return m.StepStartDiagnostics()
	case StepReadApps:
		return m.StepReadApps()
	case StepReadProfiles:
		return m.StepReadProfiles()
	case StepReconnect:
		return m.StepReconnect()
	}
	return fmt.Sprintf("step %d", int(s))
}

// StepError is a step that failed. ErrorText says it in a language; Error,
// in English.
type StepError struct {
	Step   Step
	Target string
	Err    error
}

func (e *StepError) Error() string { return e.Step.Text("en", e.Target) + ": " + e.Err.Error() }

func (e *StepError) Unwrap() error { return e.Err }

// partLookups are the IORegistry entries that report parts' serial
// numbers and self-tests, as found on an iPhone X (iOS 16): by class, or
// by name for device tree nodes. Driver names change with the chip, so
// each part lists the generations' names; the first that answers is kept.
var partLookups = []struct {
	key, name string
	classes   []string
}{
	{key: "camera", classes: []string{"AppleH16CamIn", "AppleH15CamIn", "AppleH14CamIn", "AppleH13CamIn", "AppleH12CamIn", "AppleH11CamIn", "AppleH10CamIn", "AppleH9CamIn"}},
	{key: "pearl", classes: []string{"ApplePearlSEPDriver"}},
	{key: "touchid", classes: []string{"AppleBiometricSensor"}},
	{key: "mesadriver", classes: []string{"AppleMesaSEPDriver"}},
	{key: "mesa", name: "mesa"},
	{key: "display", classes: []string{"AppleCLCD", "AppleCLCD2", "IOMobileFramebuffer"}},
	{key: "touch", classes: []string{"AppleMultitouchDevice"}},
	{key: "storage", classes: []string{"AppleANS3NVMeController", "AppleANS2NVMeController", "AppleEmbeddedNVMeController"}},
	{key: "product", name: "product"},
}

// readParts reads the parts by partLookups, or only those whose keys are
// given.
func readParts(diag *services.DiagnosticsClient, only ...string) map[string]map[string]any {
	parts := map[string]map[string]any{}
	for _, p := range partLookups {
		if len(only) > 0 && !slices.Contains(only, p.key) {
			continue
		}
		if p.name != "" {
			if reg, err := diag.IORegistry("", p.name, ""); err == nil && len(reg) > 0 {
				parts[p.key] = reg
			}
			continue
		}
		for _, c := range p.classes {
			if reg, err := diag.IORegistry("", "", c); err == nil && len(reg) > 0 {
				parts[p.key] = reg
				break
			}
		}
	}
	return parts
}

// Opener opens a trusted lockdown session with the device.
type Opener func() (*lockdown.Client, error)

// Reuse returns an Opener that first hands out l, a session already open,
// and opens new ones with open after that.
func Reuse(l *lockdown.Client, open Opener) Opener {
	return func() (*lockdown.Client, error) {
		if l != nil {
			c := l
			l = nil
			return c, nil
		}
		return open()
	}
}

// session hands out a lockdown client, replacing it when an exchange left
// it unusable, so that one timeout does not spoil the steps after it.
type session struct {
	open Opener
	l    *lockdown.Client
}

func (s *session) client() (*lockdown.Client, error) {
	if s.l != nil && !s.l.Broken() {
		return s.l, nil
	}
	if s.l != nil {
		s.l.Close()
		s.l = nil
	}
	l, err := s.open()
	if err != nil {
		return nil, &StepError{Step: StepReconnect, Err: err}
	}
	s.l = l
	return l, nil
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// do runs fn with a client, and once more with a new session when the
// first attempt broke the connection without timing out (a timeout would
// most likely time out again).
func (s *session) do(fn func(l *lockdown.Client) error) error {
	l, err := s.client()
	if err != nil {
		return err
	}
	err = fn(l)
	if err != nil && l.Broken() && !isTimeout(err) {
		if l, err2 := s.client(); err2 == nil {
			err = fn(l)
		}
	}
	return err
}

func (s *session) close() {
	if s.l != nil {
		s.l.Close()
	}
}

// CollectInfo reads what the overview shows: lockdown's values and the
// battery gauge. It is quick and judges nothing; Collect reads the rest an
// inspection needs.
func CollectInfo(open Opener) (*RawData, error) {
	ss := &session{open: open}
	defer ss.close()
	raw := &RawData{Domains: map[string]map[string]any{}}
	if err := raw.readLockdown(ss); err != nil {
		return nil, err
	}
	err := ss.do(func(l *lockdown.Client) error {
		diag, err := services.OpenDiagnostics(l)
		if err != nil {
			return err
		}
		defer diag.Close()
		raw.readBattery(diag)
		// The storage controller, for the storage details.
		raw.Parts = readParts(diag, "storage")
		return nil
	})
	if err != nil {
		raw.fail(StepStartDiagnostics, "", err)
	}
	raw.scrub()
	return raw, nil
}

// Collect reads a device through trusted lockdown sessions that open
// makes: the first is used for every step, and replaced when a step leaves
// it unusable. It reads as much as it can and records what failed; it fails
// only when even the root domain cannot be read. progress, if not nil, is
// told each step.
func Collect(open Opener, progress func(Step)) (*RawData, error) {
	say := func(s Step) {
		if progress != nil {
			progress(s)
		}
	}
	ss := &session{open: open}
	defer ss.close()
	raw := &RawData{Domains: map[string]map[string]any{}}

	say(StepDeviceInfo)
	if err := raw.readLockdown(ss); err != nil {
		return nil, err
	}

	say(StepDiagnostics)
	err := ss.do(func(l *lockdown.Client) error {
		diag, err := services.OpenDiagnostics(l)
		if err != nil {
			return err
		}
		defer diag.Close()
		raw.readBattery(diag)
		if reg, err := diag.IORegistry("", "", "IOPlatformExpertDevice"); err == nil {
			raw.PlatformReg = reg
		}
		if all, err := diag.All(); err == nil {
			raw.DiagAll = all
		}
		raw.Parts = readParts(diag)
		raw.Gestalt, raw.GestaltErr = diag.MobileGestalt(gestaltKeys...)
		raw.readSysCfg()
		return nil
	})
	if err != nil {
		raw.fail(StepStartDiagnostics, "", err)
	}

	say(StepAnalytics)
	raw.AnalyticsErr = ss.do(func(l *lockdown.Client) error {
		var err error
		raw.Analytics, raw.AnalyticsScan, err = services.ReadAnalyticsBattery(l)
		return err
	})

	say(StepApps)
	raw.AppsErr = ss.do(func(l *lockdown.Client) error {
		var err error
		raw.Apps, err = services.ListApps(l)
		return err
	})
	if raw.AppsErr != nil {
		raw.fail(StepReadApps, "", raw.AppsErr)
	}

	say(StepJailbreak)
	raw.AFC2Err = ss.do(func(l *lockdown.Client) error {
		ok, err := services.ServiceAvailable(l, services.AFC2)
		if err == nil {
			raw.AFC2 = &ok
		}
		return err
	})

	say(StepProfiles)
	raw.MCErr = ss.do(func(l *lockdown.Client) error {
		var err error
		raw.MC, err = services.ReadMC(l)
		return err
	})
	if raw.MCErr != nil {
		raw.fail(StepReadProfiles, "", raw.MCErr)
	}
	raw.scrub()
	return raw, nil
}

// readLockdown reads the root domain, the root keys it left out, and the
// other domains. It fails only when the root domain cannot be read.
func (raw *RawData) readLockdown(ss *session) error {
	err := ss.do(func(l *lockdown.Client) error {
		raw.UDID = l.Device().UDID
		root, err := l.GetDomain("")
		if err != nil {
			return err
		}
		raw.Root = root
		return nil
	})
	if err != nil {
		return &StepError{Step: StepDeviceInfo, Err: err}
	}
	for _, k := range rootKeys {
		if _, ok := raw.Root[k]; ok {
			continue
		}
		_ = ss.do(func(l *lockdown.Client) error {
			v, err := l.GetValue("", k)
			if err == nil {
				raw.Root[k] = v
			}
			if errors.Is(err, lockdown.ErrMissingValue) {
				return nil
			}
			return err
		})
	}
	for _, d := range Domains {
		err := ss.do(func(l *lockdown.Client) error {
			m, err := l.GetDomain(d)
			if err == nil {
				raw.Domains[d] = m
			}
			return err
		})
		if err != nil && !errors.Is(err, lockdown.ErrMissingValue) {
			raw.fail(StepReadDomain, d, err)
		}
	}
	return nil
}

// readBattery reads the battery's IORegistry entry.
func (raw *RawData) readBattery(diag *services.DiagnosticsClient) {
	if reg, err := diag.IORegistry("", "", "IOPMPowerSource"); err == nil {
		raw.BatteryReg = reg
	} else if reg, err2 := diag.IORegistry("", "AppleSmartBattery", ""); err2 == nil {
		raw.BatteryReg = reg
	} else {
		raw.fail(StepReadBattery, "", err)
	}
}

// readSysCfg parses the factory configuration out of the MobileGestalt
// answer, where it is a block too large to keep as one raw value.
func (raw *RawData) readSysCfg() {
	b, ok := raw.Gestalt["SysCfg"].([]byte)
	if !ok {
		return
	}
	delete(raw.Gestalt, "SysCfg")
	raw.SysCfg, raw.SysCfgErr = syscfg.Parse(b)
}

// scrub drops the subscriber values from everything read.
func (raw *RawData) scrub() {
	scrubSubscriber(raw.Root)
	for _, m := range raw.Domains {
		scrubSubscriber(m)
	}
	scrubSubscriber(raw.Gestalt)
	scrubAccounts(raw.Domains[dataSyncDomain])
}

// subscriberKeyRe matches the values that identify the SIM card's owner
// rather than the device: the phone number, ICCID and IMSI, per SIM slot
// (a trailing 2) and inside CarrierBundleInfoArray. Reports are handed to
// the other party of a sale, so these are dropped as soon as they are read.
var subscriberKeyRe = regexp.MustCompile(`^(?:PhoneNumber|IntegratedCircuitCardIdentity|InternationalMobileSubscriberIdentity|ICCID|IMSI)\d*$`)

// scrubSubscriber deletes the subscriber keys from v, at any depth.
func scrubSubscriber(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if subscriberKeyRe.MatchString(k) {
				delete(x, k)
			} else {
				scrubSubscriber(e)
			}
		}
	case []any:
		for _, e := range x {
			scrubSubscriber(e)
		}
	}
}
