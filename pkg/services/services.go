// Package services reads lockdown services of a device: diagnostics_relay
// (IORegistry, the "All" diagnostics, MobileGestalt), installation_proxy
// (installed apps), MCInstall (profiles, cloud configuration), and over AFC
// the analytics logs that crashreportcopymobile serves. ServiceAvailable
// tells whether a service is registered at all.
//
// The plist services exchange 4-byte length-prefixed plists; AFC has its
// own framing (afc.go).
package services

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/ac0d3r/idevloupe/pkg/lockdown"
	"github.com/ac0d3r/idevloupe/pkg/plistx"
	"github.com/ac0d3r/idevloupe/pkg/usbmux"
)

// Service names.
const (
	Diagnostics       = "com.apple.mobile.diagnostics_relay"
	DiagnosticsLegacy = "com.apple.iosdiagnostics.relay"
	InstallationProxy = "com.apple.mobile.installation_proxy"
	MCInstall         = "com.apple.mobile.MCInstall"
	AFC2              = "com.apple.afc2"
)

func call(c net.Conn, req map[string]any) (map[string]any, error) {
	return callTimeout(c, req, 30*time.Second)
}

func callTimeout(c net.Conn, req map[string]any, timeout time.Duration) (map[string]any, error) {
	_ = c.SetDeadline(time.Now().Add(timeout))
	defer c.SetDeadline(time.Time{})
	if err := plistx.WriteFramed(c, req); err != nil {
		return nil, err
	}
	return plistx.ReadFramed(c)
}

// DiagnosticsClient is a diagnostics_relay connection.
type DiagnosticsClient struct {
	c net.Conn
	// broken is set when an exchange failed mid-way: a late reply could
	// answer the next request, so the connection is not used again.
	broken bool
}

// OpenDiagnostics starts diagnostics_relay. The device refuses the
// connection for a moment after another client used the service, as right
// after an IORegistry probe, so a refusal is retried.
func OpenDiagnostics(l *lockdown.Client) (*DiagnosticsClient, error) {
	var c net.Conn
	var err error
	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		c, err = l.OpenService(Diagnostics)
		var re *usbmux.ResultError
		if !errors.As(err, &re) || re.Code != usbmux.ResultConnRefused {
			break
		}
	}
	if err != nil {
		var err2 error
		c, err2 = l.OpenService(DiagnosticsLegacy)
		if err2 != nil {
			return nil, err
		}
	}
	return &DiagnosticsClient{c: c}, nil
}

// Close says goodbye and closes the connection.
func (d *DiagnosticsClient) Close() error {
	if !d.broken {
		_, _ = call(d.c, map[string]any{"Request": "Goodbye"})
	}
	return d.c.Close()
}

var errServiceBroken = errors.New("service connection unusable after an earlier error")

func (d *DiagnosticsClient) request(req map[string]any) (map[string]any, error) {
	return d.requestTimeout(req, 30*time.Second)
}

func (d *DiagnosticsClient) requestTimeout(req map[string]any, timeout time.Duration) (map[string]any, error) {
	if d.broken {
		return nil, errServiceBroken
	}
	resp, err := callTimeout(d.c, req, timeout)
	if err != nil {
		d.broken = true
		return nil, err
	}
	if s, _ := resp["Status"].(string); s != "Success" {
		return nil, fmt.Errorf("diagnostics %v: status %q", req["Request"], s)
	}
	diag, _ := plistx.Dict(resp["Diagnostics"])
	if diag == nil {
		return nil, errors.New("diagnostics: response without Diagnostics")
	}
	return diag, nil
}

// IORegistry queries one IORegistry entry by class or name (either may be
// empty) and returns its properties.
func (d *DiagnosticsClient) IORegistry(plane, name, class string) (map[string]any, error) {
	req := map[string]any{"Request": "IORegistry"}
	if plane != "" {
		req["CurrentPlane"] = plane
	}
	if name != "" {
		req["EntryName"] = name
	}
	if class != "" {
		req["EntryClass"] = class
	}
	diag, err := d.request(req)
	if err != nil {
		return nil, err
	}
	reg, _ := plistx.Dict(diag["IORegistry"])
	if reg == nil {
		return nil, errors.New("diagnostics: no IORegistry in response")
	}
	return reg, nil
}

// Query sends one diagnostics request and returns the whole response,
// whatever its status, for probing what the device answers.
func (d *DiagnosticsClient) Query(req map[string]any) (map[string]any, error) {
	if d.broken {
		return nil, errServiceBroken
	}
	resp, err := callTimeout(d.c, req, time.Minute)
	if err != nil {
		d.broken = true
	}
	return resp, err
}

// All runs the "All" diagnostics request, whose GasGauge, NAND and WiFi
// sections come from the device's own diagnostics (as ipsw reads them).
func (d *DiagnosticsClient) All() (map[string]any, error) {
	return d.request(map[string]any{"Request": "All"})
}

// ErrGestaltDeprecated means the device no longer answers MobileGestalt
// queries over diagnostics_relay (iOS 17.4 and later).
var ErrGestaltDeprecated = errors.New("MobileGestalt queries are disabled on this iOS version")

// MobileGestalt reads MobileGestalt keys.
func (d *DiagnosticsClient) MobileGestalt(keys ...string) (map[string]any, error) {
	diag, err := d.request(map[string]any{"Request": "MobileGestalt", "MobileGestaltKeys": keys})
	if err != nil {
		return nil, err
	}
	mg, _ := plistx.Dict(diag["MobileGestalt"])
	if mg == nil {
		return nil, errors.New("diagnostics: no MobileGestalt in response")
	}
	if s, _ := mg["Status"].(string); s == "MobileGestaltDeprecated" {
		return nil, ErrGestaltDeprecated
	}
	delete(mg, "Status")
	return mg, nil
}

// App is an installed application.
type App struct {
	BundleID string
	Name     string
	Version  string
	Type     string // "User", "System", "Internal", ...
}

// ListApps lists installed applications of every type.
func ListApps(l *lockdown.Client) ([]App, error) {
	c, err := l.OpenService(InstallationProxy)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(60 * time.Second))
	err = plistx.WriteFramed(c, map[string]any{
		"Command": "Browse",
		"ClientOptions": map[string]any{
			"ApplicationType": "Any",
			"ReturnAttributes": []string{
				"CFBundleIdentifier", "CFBundleDisplayName", "CFBundleName",
				"CFBundleShortVersionString", "ApplicationType",
			},
		},
	})
	if err != nil {
		return nil, err
	}
	var apps []App
	for {
		resp, err := plistx.ReadFramed(c)
		if err != nil {
			return apps, err
		}
		if e, _ := resp["Error"].(string); e != "" {
			return apps, fmt.Errorf("installation_proxy: %s", e)
		}
		list, _ := resp["CurrentList"].([]any)
		for _, it := range list {
			m, ok := plistx.Dict(it)
			if !ok {
				continue
			}
			a := App{}
			a.BundleID, _ = m["CFBundleIdentifier"].(string)
			a.Name, _ = m["CFBundleDisplayName"].(string)
			if a.Name == "" {
				a.Name, _ = m["CFBundleName"].(string)
			}
			a.Version, _ = m["CFBundleShortVersionString"].(string)
			a.Type, _ = m["ApplicationType"].(string)
			apps = append(apps, a)
		}
		if s, _ := resp["Status"].(string); s == "Complete" {
			return apps, nil
		}
	}
}

// Profile is an installed configuration profile.
type Profile struct {
	Identifier   string
	DisplayName  string
	Organization string
	IsMDM        bool
	// RemovalDisallowed means the user cannot delete the profile, as
	// management profiles are installed.
	RemovalDisallowed bool
}

// MCInfo is what MCInstall reports.
type MCInfo struct {
	Profiles []Profile
	// Cloud is the cloud (DEP / Automated Device Enrollment) configuration,
	// nil when the device returns none.
	Cloud map[string]any
	// RawProfiles is the GetProfileList response, for the raw data view.
	RawProfiles map[string]any
}

// ReadMC reads installed profiles and the cloud configuration.
func ReadMC(l *lockdown.Client) (*MCInfo, error) {
	c, err := l.OpenService(MCInstall)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	info := &MCInfo{}
	resp, err := call(c, map[string]any{"RequestType": "GetProfileList"})
	if err != nil {
		return nil, err
	}
	info.RawProfiles = resp
	meta, _ := plistx.Dict(resp["ProfileMetadata"])
	order, _ := resp["OrderedIdentifiers"].([]any)
	seen := map[string]bool{}
	add := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		p := Profile{Identifier: id}
		if m, ok := plistx.Dict(meta[id]); ok {
			p.DisplayName, _ = m["PayloadDisplayName"].(string)
			p.Organization, _ = m["PayloadOrganization"].(string)
			p.IsMDM, _ = m["IsMDMProfile"].(bool)
			p.RemovalDisallowed, _ = m["PayloadRemovalDisallowed"].(bool)
		}
		info.Profiles = append(info.Profiles, p)
	}
	for _, o := range order {
		if id, ok := o.(string); ok {
			add(id)
		}
	}
	for id := range meta {
		add(id)
	}
	if resp, err := call(c, map[string]any{"RequestType": "GetCloudConfiguration"}); err == nil {
		if cc, ok := plistx.Dict(resp["CloudConfiguration"]); ok && len(cc) > 0 {
			info.Cloud = cc
		}
	}
	return info, nil
}

// ServiceAvailable reports whether lockdownd starts the named service. It is
// used for com.apple.afc2, which only jailbroken devices register.
func ServiceAvailable(l *lockdown.Client, name string) (bool, error) {
	info, err := l.StartService(name)
	if err != nil {
		if errors.Is(err, lockdown.ErrServiceForbidden) {
			return false, nil
		}
		return false, err
	}
	// Connect and close so the service does not linger waiting.
	if c, err := l.DialService(info); err == nil {
		c.Close()
	}
	return true, nil
}
