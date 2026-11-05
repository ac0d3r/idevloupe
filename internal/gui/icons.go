package gui

import "github.com/egoist/mygo/ui"

func icon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

var (
	icPhone   = icon(`<rect x="6" y="2" width="12" height="20" rx="3"/><path d="M11 18h2"/>`)
	icRefresh = icon(`<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 4v7h-7"/>`)
	icCopy    = icon(`<rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"/>`)
	icSave    = icon(`<path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/>`)
	icCheck   = icon(`<path d="m5 12 5 5 9-10"/>`)
	icAlert   = icon(`<path d="M12 3 2 20h20L12 3z"/><path d="M12 10v4"/><path d="M12 17h.01"/>`)
	icX       = icon(`<path d="M6 6l12 12"/><path d="M18 6 6 18"/>`)
	icInfo    = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 11v5"/><path d="M12 8h.01"/>`)
	icHelp    = icon(`<circle cx="12" cy="12" r="9"/><path d="M9.5 9.5a2.5 2.5 0 0 1 5 0c0 1.7-2.5 2-2.5 3.5"/><path d="M12 17h.01"/>`)
	icLoupe   = icon(`<circle cx="10.5" cy="10.5" r="6.5"/><path d="m15.4 15.4 5.1 5.1"/><path d="m7.6 10.6 2 2 3.8-3.9"/>`)
	icHand    = icon(`<path d="M8 13V5.5a1.5 1.5 0 0 1 3 0V12"/><path d="M11 11.5v-2a1.5 1.5 0 0 1 3 0V12"/><path d="M14 10.5a1.5 1.5 0 0 1 3 0V12"/><path d="M17 11.5a1.5 1.5 0 0 1 3 0V16a6 6 0 0 1-6 6h-1.5a6 6 0 0 1-4.6-2.2L4.6 16.4a1.6 1.6 0 0 1 2.4-2L8 15.5"/>`)
	icGlobe   = icon(`<circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3a14 14 0 0 1 0 18"/><path d="M12 3a14 14 0 0 0 0 18"/>`)
	icPlug    = icon(`<path d="M9 2v6"/><path d="M15 2v6"/><path d="M6 8h12v3a6 6 0 0 1-12 0V8z"/><path d="M12 17v5"/>`)
)

// logoGradient is the app's color, from violet to deep purple.
var logoGradient = ui.LinearGradient{Stops: []ui.GradientStop{{Color: ui.Hex("#a855f7"), At: 0}, {Color: ui.Hex("#6d28d9"), At: 1}}, Angle: 135}

// logo draws the app's mark, a loupe over a check, on its gradient: in the
// sidebar and, large, as the app icon (TestIcon).
func logo(c *ui.Context, size, radius, glyph float32) {
	ui.Box(c).Size(size, size).Radius(radius).Center().LinearGradient(logoGradient).
		Children(func() { ui.Icon(c, icLoupe).FontSize(glyph).TextColor(ui.Hex("#ffffff")) })
}
