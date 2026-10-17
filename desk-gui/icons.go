package main

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// The icons are drawn the way macOS draws its own: 24×24, thin round
// strokes in the text color, as SF Symbols and the toolbar of Finder or
// Safari are. Parse them once here, not in a view.

// stroked builds a 24×24 icon of outlines, in the color of the text.
func stroked(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

// filled builds a 24×24 icon of solid shapes, in the color of the text.
func filled(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor">` + shapes + `</svg>`))
}

var (
	iconBack    = stroked(`<path d="M14.5 5.5 8 12l6.5 6.5"/>`)
	iconHome    = stroked(`<path d="M4 10.6 12 4.2l8 6.4V19a1.4 1.4 0 0 1-1.4 1.4H5.4A1.4 1.4 0 0 1 4 19z"/><path d="M9.6 20.4v-5.8h4.8v5.8"/>`)
	iconRecents = stroked(`<rect x="3.4" y="3.4" width="12.6" height="12.6" rx="3.4"/><path d="M8.6 20.6h7a4.6 4.6 0 0 0 4.6-4.6v-7"/>`)
	iconShades  = stroked(`<path d="M6.2 9.2a5.8 5.8 0 0 1 11.6 0c0 6.6 2.4 8.4 2.4 8.4H3.8s2.4-1.8 2.4-8.4"/><path d="M10.4 20.6a1.9 1.9 0 0 0 3.2 0"/>`)
	iconApps    = stroked(`<rect x="3.4" y="3.4" width="7.2" height="7.2" rx="2.2"/><rect x="13.4" y="3.4" width="7.2" height="7.2" rx="2.2"/><rect x="3.4" y="13.4" width="7.2" height="7.2" rx="2.2"/><rect x="13.4" y="13.4" width="7.2" height="7.2" rx="2.2"/>`)
	iconShot    = stroked(`<path d="M3.6 9.4A2.4 2.4 0 0 1 6 7h1.4l1.1-2.1h7l1.1 2.1H18a2.4 2.4 0 0 1 2.4 2.4v7.2A2.4 2.4 0 0 1 18 19H6a2.4 2.4 0 0 1-2.4-2.4z"/><circle cx="12" cy="12.8" r="3.1"/>`)
	iconPlay    = filled(`<path d="M8.2 5.4v13.2a1 1 0 0 0 1.5.9l9.6-6.6a1 1 0 0 0 0-1.8L9.7 4.5a1 1 0 0 0-1.5.9z"/>`)
	iconStop    = filled(`<rect x="6.6" y="6.6" width="10.8" height="10.8" rx="2.4"/>`)
	iconQuit    = stroked(`<path d="M6.6 6.6l10.8 10.8M17.4 6.6 6.6 17.4"/>`)
	iconSearch  = stroked(`<circle cx="11" cy="11" r="6.8"/><path d="m20 20-4.6-4.6"/>`)
	iconReload  = stroked(`<path d="M19.6 12a7.6 7.6 0 1 1-2.3-5.4"/><path d="M19.6 4.6v4.6h-4.6"/>`)
	iconList    = stroked(`<path d="M4.2 6h.01M4.2 12h.01M4.2 18h.01M8.6 6H20M8.6 12H20M8.6 18H20"/>`)
	iconPhone   = stroked(`<rect x="6.8" y="2.6" width="10.4" height="18.8" rx="3"/><path d="M10.8 5.4h2.4"/>`)
	iconDisplay = stroked(`<rect x="2.6" y="4.4" width="18.8" height="12.8" rx="2.6"/><path d="M8.4 20.4h7.2M12 17.2v3.2"/>`)
)

// iconGlyph is the size of an icon in a toolbar button, and in a row of
// the drawer.
const iconGlyph = 16

// toolbarIcon is an icon-only button, as a macOS toolbar draws them: a
// glyph, no text, a tooltip that names it and a label for assistive
// technology, which is also how tests find it.
func toolbarIcon(c *ui.Context, ic *ui.SVG, name, tip string, disabled bool, onClick func()) {
	ui.Button(c, "").Label(name).Tooltip(tip).Disabled(disabled).OnClick(onClick).
		Children(func() {
			ui.Icon(c, ic).Size(iconGlyph, iconGlyph)
		})
}

// toolbarButton is an icon and a word, for the actions that deserve to be
// readable at a glance. An empty label leaves the button's face to the
// children below.
func toolbarButton(c *ui.Context, ic *ui.SVG, name, tip string, primary, disabled bool, onClick func()) {
	t := c.Theme()
	b := ui.Button(c, "")
	if primary {
		// The prominent button is a pane of Liquid Glass tinted with the
		// accent, which grows a little while it is held, as macOS 26 draws
		// one. The glass plugin draws it on every platform, on the GPU or
		// the CPU, so it is there whether or not the window has a material.
		b.Material(glass.Glass{Tint: t.Accent, Interactive: true})
	}
	b.Label(name).Tooltip(tip).Disabled(disabled).OnClick(onClick).
		Children(func() {
			row := ui.Row(c).Gap(6).AlignItems(ui.Center)
			if primary {
				row.TextColor(t.AccentText)
			}
			row.Children(func() {
				ui.Icon(c, ic).Size(iconGlyph-2, iconGlyph-2)
				ui.Text(c, name).SingleLine()
			})
		})
}

// anchoredIcon and anchoredButton are the bar's popover buttons: they hand
// their own box to open, which is what the flyout hangs from.
func anchoredIcon(c *ui.Context, ic *ui.SVG, name, tip string, open func(ui.Rect)) {
	b := ui.Button(c, "").Label(name).Tooltip(tip)
	b.OnClick(func() { open(b.Bounds()) })
	b.Children(func() { ui.Icon(c, ic).Size(iconGlyph, iconGlyph) })
}

func anchoredButton(c *ui.Context, ic *ui.SVG, name, tip string, open func(ui.Rect)) {
	b := ui.Button(c, "")
	b.Label(name).Tooltip(tip)
	b.OnClick(func() { open(b.Bounds()) })
	b.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, ic).Size(iconGlyph-2, iconGlyph-2)
			ui.Text(c, name).SingleLine()
		})
	})
}
