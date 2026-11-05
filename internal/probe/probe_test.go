package probe

import (
	"strings"
	"testing"
)

func TestFind(t *testing.T) {
	tree := map[string]any{
		"IORegistryEntryName": "Root",
		"IORegistryEntryChildren": []any{
			map[string]any{
				"IORegistryEntryName":        "AppleH10CamIn",
				"FrontCameraModuleSerialNum": []byte("DN8123456\x00"),
				"Temperature":                uint64(30),
			},
			map[string]any{
				"IORegistryEntryName": "syscfg",
				"FCMS":                []byte("DN8123456\x00"),
				"Mod#":                "MQA52",
			},
		},
	}
	var lines []string
	for _, h := range Find(tree) {
		lines = append(lines, h.Path+" = "+h.Value)
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{"/Root/AppleH10CamIn/FrontCameraModuleSerialNum = DN8123456", "/Root/syscfg/FCMS = DN8123456"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Temperature") {
		t.Errorf("uninteresting key reported:\n%s", got)
	}
}
