package probe

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"howett.net/plist"

	"github.com/ac0d3r/idevloupe/internal/inspect"
	"github.com/ac0d3r/idevloupe/pkg/lockdown"
	"github.com/ac0d3r/idevloupe/pkg/plistx"
	"github.com/ac0d3r/idevloupe/pkg/services"
)

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
		"AppleSmartBatteryManager", "AppleARMPMUCharger", "AppleHPMInterface",
	} {
		qs = append(qs, Query{Class: c})
	}
	return qs
}()

// Keys are the names a closed tool that compares factory and current part
// values asks for.
var Keys = []string{
	"SysCfg", "SysCfgData", "SysCfgDict",
	"BatterySerialNumber", "BatterySerialNumberRAW", "BatterySerialNumberMD",
	"PanelSerialNumber", "RawPanelSerialNumber", "CoverglassSerialNumber",
	"RearFacingCameraModuleSerialNumber", "FrontFacingCameraModuleSerialNumber", "BackSuperWideCameraSNUM",
	"InfraredCameraSerialNumber", "ArcModuleSerialNumber", "YonkersSerialNumber", "SavageSerialNumber", "RosalineSerialNumber",
	"MesaSerialNumber", "JasperSNUM", "LynxSerialNumber", "NovaSerialNum", "LatticeSerialNumber",
	"AmbientLightSensorSerialNumber", "WirelessBoardSerialNumber", "WirelessBoardSnum", "LunaFlexSerialNumber",
	"BasebandBoardSnum", "InterfaceModuleSerialNumber",
	"BatterySerialChanged", "BatterySerialChangedHistory", "BatterySerialNumberChanged",
	"BatteryAuth", "DeviceSupportsBatteryModuleAuthentication", "BatteryCellDisconnectCount",
	"BatteryShutdownReason", "CycleCountLastQmax", "LifeTimeQmaxUpdateCounter",
	"FDRSealingStatus", "FDRCalibration", "FDRReportError",
	"TouchIDHealthy", "MultiSensorOK",
	"NANDType", "DiskVendor", "DiskType", "NANDWriteCount", "NANDPowerOnTime", "NandControllerUID",
	"EffectiveProductionStatusSEP", "EffectiveSecurityModeSEP",
}

type KeyResult struct {
	Source, Key string
	Status      string
	Value       any
}

func probeKeys(l *lockdown.Client, diag *services.DiagnosticsClient) []KeyResult {
	var out []KeyResult
	for _, k := range Keys {
		v, err := l.GetValue("", k)
		switch {
		case err == nil:
			out = append(out, KeyResult{"lockdown", k, "ok", v})
		case errors.Is(err, lockdown.ErrMissingValue):
			out = append(out, KeyResult{"lockdown", k, "missing", nil})
		default:
			out = append(out, KeyResult{"lockdown", k, "error: " + err.Error(), nil})
		}
		if l.Broken() {
			break
		}
	}
	mg, err := diag.MobileGestalt(Keys...)
	if err != nil {
		return append(out, KeyResult{"MobileGestalt", "*", "error: " + err.Error(), nil})
	}
	for _, k := range Keys {
		if v, ok := mg[k]; ok {
			out = append(out, KeyResult{"MobileGestalt", k, "ok", v})
		} else {
			out = append(out, KeyResult{"MobileGestalt", k, "missing", nil})
		}
	}
	return out
}

type Result struct {
	Query    Query
	Status   string
	Props    int
	Response map[string]any
}

type Report struct {
	Keys    []KeyResult
	Queries []Result
}

// Probe writes a plist: {"keys/<source>": {key: value}, query: response}.
func Probe(l *lockdown.Client, path string) (*Report, error) {
	diag, err := services.OpenDiagnostics(l)
	if err != nil {
		return nil, err
	}
	defer diag.Close()
	rep := &Report{Keys: probeKeys(l, diag)}
	dump := map[string]any{}
	for _, k := range rep.Keys {
		if k.Status != "ok" {
			continue
		}
		src, _ := dump["keys/"+k.Source].(map[string]any)
		if src == nil {
			src = map[string]any{}
			dump["keys/"+k.Source] = src
		}
		src[k.Key] = k.Value
	}
	var results []Result
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
	rep.Queries = results
	data, err := plist.Marshal(dump, plist.BinaryFormat)
	if err != nil {
		return rep, err
	}
	return rep, os.WriteFile(path, data, 0o644)
}

var interestingKey = regexp.MustCompile(`(?i)serial|sn$|^sn|syscfg|^(fcms|bcms|mtsn|batt|bmac|wmac|nvsn|nsrn|lcm#|cmms|dclr|clhs|vbst|nvms)$|pearl|camera.*(module|sn)|multitouch|display.*(id|sn)|panel|auth|fdr|qmax|changed`)

type Hit struct {
	Path, Value string
}

func Find(tree any) []Hit {
	var hits []Hit
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch t := v.(type) {
		case map[string]any:
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

func Summary(rep *Report, width int) string {
	var b strings.Builder
	cut := func(v string) string {
		if rs := []rune(v); len(rs) > width {
			return string(rs[:width]) + "…"
		}
		return v
	}
	b.WriteString("== Keys\n")
	for _, k := range rep.Keys {
		v := ""
		if k.Status == "ok" {
			v = " = " + cut(inspect.FormatValue(k.Value))
		}
		fmt.Fprintf(&b, "%-14s %-42s %s%s\n", k.Source, k.Key, k.Status, v)
	}
	results := rep.Queries
	b.WriteString("\n== Queries\n")
	for _, r := range results {
		fmt.Fprintf(&b, "%-45s %-20s %d properties\n", r.Query, r.Status, r.Props)
	}
	b.WriteString("\n== Fields that look like serial numbers or factory configuration\n")
	for _, r := range results {
		reg, _ := plistx.Path(r.Response, "Diagnostics", "IORegistry")
		for _, h := range Find(reg) {
			fmt.Fprintf(&b, "[%s] %s = %s\n", r.Query, h.Path, cut(h.Value))
		}
	}
	return b.String()
}
