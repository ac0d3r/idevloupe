package gui

import (
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestIcon renders the app icon: ICON_OUT=resources/icon.png go test -run TestIcon ./internal/gui
func TestIcon(t *testing.T) {
	out := os.Getenv("ICON_OUT")
	if out == "" {
		t.Skip("ICON_OUT not set")
	}
	view := func(c *ui.Context) {
		t := *c.Theme()
		t.Background = ui.Transparent
		c.SetTheme(&t)
		// macOS icon grid: an 824-point rounded square in a 1024 canvas.
		ui.Box(c).Fill().Center().Children(func() {
			logo(c, 412, 92, 250)
		})
	}
	tt := ui.NewTester(view, 512, 512)
	tt.SetScale(2)
	tt.Frame()
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
