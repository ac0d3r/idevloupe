// Package probe asks a device for more than the inspection reads, to find
// where it keeps information, such as the serial numbers of its parts,
// before a check relies on it.
package probe

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"howett.net/plist"

	"idevloupe/internal/inspect"
	"idevloupe/internal/lockdown"
	"idevloupe/internal/plistx"
	"idevloupe/internal/services"
)

// Query is one IORegistry lookup of diagnostics_relay.
type Query struct {
	Plane, Name, Class string
}

func (q Query) String() string {
	var parts []string
	for _, p := range [][2]string{{"plane", q.Plane}, {"name", q.Name}, {"class", q.Class}} {
		if p[1] != "" {
			parts = append(parts, p[0]+"="+p[1])
		}
	}
	return strings.Join(parts, " ")
}

func (q Query) request() map[string]any {
	req := map[string]any{"Request": "IORegistry"}
	if q.Plane != "" {
		req["CurrentPlane"] = q.Plane
	}
	if q.Name != "" {
		req["EntryName"] = q.Name
	}
	if q.Class != "" {
		req["EntryClass"] = q.Class
	}
	return req
}

// Queries are the lookups tried: whole planes, device tree nodes where
// factory configuration may sit, and the drivers of parts that are paired
// to the device (cameras, touch, display, Face ID, storage, baseband) in
// the names they have had across generations. Absent ones answer empty.
var Queries = func() []Query {
	qs := []Query{
		{Plane: "IODeviceTree"}, {Plane: "IOService"},
		{Plane: "IODeviceTree", Name: "device-tree"}, {Plane: "IOService", Name: "Root"},
	}
	for _, n := range []string{"chosen", "product", "arm-io", "defaults", "options", "buttons", "baseband", "camera", "display", "lcd", "backlight", "mesa", "pearl", "sep", "syscfg"} {
		qs = append(qs, Query{Plane: "IODeviceTree", Name: n}, Query{Name: n})
	}
	for _, c := range []string{
		"AppleH9CamIn", "AppleH10CamIn", "AppleH11CamIn", "AppleH12CamIn", "AppleH13CamIn", "AppleH14CamIn", "AppleH15CamIn", "AppleH16CamIn",
		"AppleMultitouchSPI", "AppleMultitouchZ2SPI", "AppleMultitouchN1SPI", "AppleMultitouchDevice",
		"AppleCLCD", "AppleCLCD2", "IOMobileFramebuffer", "AppleARMBacklight", "AppleDisplay",
		"AppleSEPManager", "ApplePearlSEPDriver", "AppleBiometricSensor", "AppleMesaSEPDriver", "AppleProxDriver",
		"AppleANS2NVMeController", "AppleANS3NVMeController", "AppleEmbeddedNVMeController",
		"AppleBasebandPCI", "AppleBaseband", "AppleSmartBattery", "AppleHPMDevice", "AppleTriStar",
		"AppleSPUHIDDevice", "AppleAOPAudioDevice",
	} {
		qs = append(qs, Query{Class: c})
	}
	return qs
}()

// Result is the answer to one query.
type Result struct {
	Query    Query
	Status   string // the response's Status, or the error
	Props    int    // properties of the entry returned
	Response map[string]any
}

// Probe runs every query and writes their whole responses to path as a
// binary plist, {query: response}.
func Probe(l *lockdown.Client, path string) ([]Result, error) {
	diag, err := services.OpenDiagnostics(l)
	if err != nil {
		return nil, err
	}
	defer diag.Close()
	var results []Result
	dump := map[string]any{}
	for _, q := range Queries {
		resp, err := diag.Query(q.request())
		r := Result{Query: q, Response: resp}
		if err != nil {
			r.Status = "error: " + err.Error()
		} else {
			r.Status, _ = resp["Status"].(string)
			if reg, ok := plistx.Path(resp, "Diagnostics", "IORegistry"); ok {
				if m, ok := plistx.Dict(reg); ok {
					r.Props = len(m)
				}
			}
			dump[q.String()] = resp
		}
		results = append(results, r)
		if err != nil {
			break // the connection is unusable
		}
	}
	data, err := plist.Marshal(dump, plist.BinaryFormat)
	if err != nil {
		return results, err
	}
	return results, os.WriteFile(path, data, 0o644)
}

// interestingKey matches property names that name serial numbers, the
// factory configuration (syscfg) and its four-letter keys of parts, and
// parts of interest.
var interestingKey = regexp.MustCompile(`(?i)serial|sn$|^sn|syscfg|^(fcms|bcms|mtsn|batt|bmac|wmac|nvsn|nsrn|lcm#|cmms|dclr|clhs|vbst|nvms)$|pearl|camera.*(module|sn)|multitouch|display.*(id|sn)|panel`)

// Hit is a property found by Find.
type Hit struct {
	Path, Value string
}

// Find walks a response and returns the properties whose names look like
// serial numbers or factory configuration, by the path of entry names that
// leads to them.
func Find(tree any) []Hit {
	var hits []Hit
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch t := v.(type) {
		case map[string]any:
			// An IORegistry entry: name the path after it.
			if name, ok := t["IORegistryEntryName"].(string); ok {
				path += "/" + name
			}
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				child := t[k]
				if k == "IORegistryEntryChildren" {
					walk(path, child)
					continue
				}
				if interestingKey.MatchString(k) {
					hits = append(hits, Hit{path + "/" + k, inspect.FormatValue(child)})
				}
				switch child.(type) {
				case map[string]any, []any:
					walk(path+"/"+k, child)
				}
			}
		case []any:
			for _, e := range t {
				walk(path, e)
			}
		}
	}
	walk("", tree)
	return hits
}

// Summary lists each query's outcome, then the serial-like properties the
// answered ones hold, values cut to width.
func Summary(results []Result, width int) string {
	var b strings.Builder
	b.WriteString("== Queries\n")
	for _, r := range results {
		fmt.Fprintf(&b, "%-45s %-20s %d properties\n", r.Query, r.Status, r.Props)
	}
	b.WriteString("\n== Fields that look like serial numbers or factory configuration\n")
	for _, r := range results {
		reg, _ := plistx.Path(r.Response, "Diagnostics", "IORegistry")
		for _, h := range Find(reg) {
			v := h.Value
			if rs := []rune(v); len(rs) > width {
				v = string(rs[:width]) + "…"
			}
			fmt.Fprintf(&b, "[%s] %s = %s\n", r.Query, h.Path, v)
		}
	}
	return b.String()
}
