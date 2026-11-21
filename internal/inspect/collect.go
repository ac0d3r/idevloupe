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

// lockdownd omits some keys from the whole-domain answer; missing ones are
// read one by one.
var rootKeys = []string{
	"ProductionSOC", "ReleaseType", "ModelNumber", "RegionInfo", "SerialNumber",
	"MLBSerialNumber", "BasebandVersion", "BasebandStatus", "NonVolatileRAM",
	"BrickState", "FusingStatus", "ActivationState", "TelephonyCapability",
	"InternationalMobileEquipmentIdentity", "InternationalMobileEquipmentIdentity2",
	"SIMTrayStatus", "BasebandSerialNumber",
}

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
	"SysCfg",
}

type RawData struct {
	UDID    string
	Root    map[string]any
	Domains map[string]map[string]any

	BatteryReg  map[string]any
	PlatformReg map[string]any
	Gestalt     map[string]any
	GestaltErr  error
	// nil when not read; SysCfgErr says why if parsing failed.
	SysCfg    map[string][]byte
	SysCfgErr error
	DiagAll   map[string]any
	Parts     map[string]map[string]any

	Apps    []services.App
	AppsErr error

	AFC2    *bool
	AFC2Err error

	MC    *services.MCInfo
	MCErr error

	Analytics     *services.AnalyticsBattery
	AnalyticsScan *services.AnalyticsScan
	AnalyticsErr  error

	Errors []*StepError
}

func (raw *RawData) inspected() bool {
	return raw.Apps != nil || raw.AppsErr != nil || raw.AFC2 != nil
}

func (raw *RawData) fail(step Step, target string, err error) {
	raw.Errors = append(raw.Errors, &StepError{Step: step, Target: target, Err: err})
}

type Step int

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

type StepError struct {
	Step   Step
	Target string
	Err    error
}

func (e *StepError) Error() string { return e.Step.Text("en", e.Target) + ": " + e.Err.Error() }

func (e *StepError) Unwrap() error { return e.Err }

// Driver names change with the chip; the first lookup that answers is kept.
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

type Opener func() (*lockdown.Client, error)

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

// session replaces a client left unusable, so one timeout does not spoil the
// steps after it.
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

// Retried once with a new session, unless it timed out (which would likely recur).
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
		raw.Parts = readParts(diag, "storage")
		return nil
	})
	if err != nil {
		raw.fail(StepStartDiagnostics, "", err)
	}
	raw.scrub()
	return raw, nil
}

// Collect fails only when the root domain cannot be read; other failures are
// recorded.
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

func (raw *RawData) readBattery(diag *services.DiagnosticsClient) {
	if reg, err := diag.IORegistry("", "", "IOPMPowerSource"); err == nil {
		raw.BatteryReg = reg
	} else if reg, err2 := diag.IORegistry("", "AppleSmartBattery", ""); err2 == nil {
		raw.BatteryReg = reg
	} else {
		raw.fail(StepReadBattery, "", err)
	}
}

func (raw *RawData) readSysCfg() {
	b, ok := raw.Gestalt["SysCfg"].([]byte)
	if !ok {
		return
	}
	delete(raw.Gestalt, "SysCfg")
	raw.SysCfg, raw.SysCfgErr = syscfg.Parse(b)
}

func (raw *RawData) scrub() {
	scrubSubscriber(raw.Root)
	for _, m := range raw.Domains {
		scrubSubscriber(m)
	}
	scrubSubscriber(raw.Gestalt)
	scrubAccounts(raw.Domains[dataSyncDomain])
}

// Subscriber identifiers (phone number, ICCID, IMSI) are dropped on read:
// reports are handed to the other party of a sale.
var subscriberKeyRe = regexp.MustCompile(`^(?:PhoneNumber|IntegratedCircuitCardIdentity|InternationalMobileSubscriberIdentity|ICCID|IMSI)\d*$`)

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
