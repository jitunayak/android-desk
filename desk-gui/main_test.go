package main

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The views run without a window, which is how the layout is checked here.

// TestBarView checks the control bar as it is before a display starts: the
// navigation buttons are there but disabled, and Start is offered.
func TestBarView(t *testing.T) {
	a := newApp()
	bar := ui.NewTester(a.barView, barWidth, barHeight)

	// The navigation buttons are icons: their labels are what names them, to
	// assistive technology and to these tests alike.
	for _, name := range []string{
		"Back", "Home", "Recents", "Shade",
		"Apps", "Phone apps", "Shot", "Display", "Start", "Quit",
	} {
		if !bar.HasText(name) {
			t.Errorf("the bar has no %q control; texts %q", name, bar.Texts())
		}
	}
	if !bar.HasText("display stopped") {
		t.Errorf("texts %q", bar.Texts())
	}

	// Clicking Apps while no window exists must not reach for a flyout.
	if err := bar.Click("Apps"); err != nil {
		t.Fatalf("clicking Apps: %v", err)
	}
	if a.pop != nil {
		t.Error("a popover was opened without a window to hang it from")
	}
}

// TestBarViewWhileRunning checks the bar with a display up: it names the app
// on it and offers Stop instead of Start.
func TestBarViewWhileRunning(t *testing.T) {
	a := newApp()
	a.serial, a.model, a.running = "ZA223G5NQ2", "motorola edge 70", true
	a.display = 260
	a.top = "com.android.chrome/com.google.android.apps.chrome.Main"
	a.apps = []appInfo{{Name: "Chrome", Pkg: "com.android.chrome"}}

	bar := ui.NewTester(a.barView, barWidth, barHeight)
	if !bar.HasText("Stop") {
		t.Errorf("a running display offers Stop; texts %q", bar.Texts())
	}
	if !bar.HasText("▶ Chrome") {
		t.Errorf("the status should name the running app; texts %q", bar.Texts())
	}
	if !bar.HasText("display 260") {
		t.Errorf("texts %q", bar.Texts())
	}
}

// TestBarFits checks that the bar's controls fit the window they were given,
// in order, instead of spilling out of it or being eaten by the toolbar's
// overflow menu.
func TestBarFits(t *testing.T) {
	a := newApp()
	a.serial, a.model, a.running = "ZA223G5NQ2", "motorola edge 70", true

	bar := ui.NewTester(a.barView, barWidth, barHeight)
	var last float32
	for _, name := range []string{"Back", "Shade", "Apps", "Phone apps", "Shot", "Display", "Stop", "Quit"} {
		r, ok := bar.Find(name)
		if !ok {
			t.Fatalf("%q is not in the bar; texts %q", name, bar.Texts())
		}
		if r.X < 0 || r.X+r.W > barWidth || r.Y < 0 || r.Y+r.H > barHeight {
			t.Errorf("%q is outside the bar: %+v", name, r)
		}
		if r.X < last {
			t.Errorf("%q is out of order: it starts at %v, after %v", name, r.X, last)
		}
		last = r.X
	}
}

func TestDrawerView(t *testing.T) {
	a := newApp()
	a.apps = []appInfo{
		{Name: "Chrome", Pkg: "com.android.chrome"},
		{Name: "Settings", Pkg: "com.android.settings"},
	}
	a.filterApps()

	p := &popover{}
	view := func(c *ui.Context) { a.drawerView(c, p) }
	tester := ui.NewTester(view, drawerWidth, drawerHeight)
	for _, want := range []string{"Chrome", "Settings", "2 of 2 apps", "Reload", "Phone drawer", "Phone home"} {
		if !tester.HasText(want) {
			t.Errorf("the drawer should show %q; texts %q", want, tester.Texts())
		}
	}

	// Typing filters the list down. A fresh app and popover: a ListState
	// belongs to one list, and a second Tester would be a second one.
	b := newApp()
	b.apps, b.query = a.apps, "chrom"
	b.filterApps()
	tester = ui.NewTester(func(c *ui.Context) { b.drawerView(c, &popover{}) }, drawerWidth, drawerHeight)
	if !tester.HasText("Chrome") || tester.HasText("Settings") {
		t.Errorf("filtering by %q gave %q", b.query, tester.Texts())
	}
	if !tester.HasText("1 of 2 apps") {
		t.Errorf("texts %q", tester.Texts())
	}
}

// TestDisplayView checks the display's settings, which decide what the phone
// is asked for.
func TestDisplayView(t *testing.T) {
	a := newApp()
	w, h := popDisplay.size()
	tester := ui.NewTester(a.displayView, w, h)

	for _, want := range []string{"Display", "Resolution", "Density", "Bit rate", "1920x1080", "220", "Medium"} {
		if !tester.HasText(want) {
			t.Errorf("the display settings should show %q; texts %q", want, tester.Texts())
		}
	}
	if !tester.HasText("applied when the display starts") {
		t.Errorf("texts %q", tester.Texts())
	}

	// With a display up, the settings say that they wait for the next start.
	a.running = true
	tester = ui.NewTester(a.displayView, w, h)
	if !tester.HasText("stop and start the display") {
		t.Errorf("texts %q", tester.Texts())
	}
}

// TestDPIFor checks that a resolution carries the density that keeps
// Android's interface the same size on screen.
func TestDPIFor(t *testing.T) {
	for _, c := range []struct{ size, want string }{
		{"1920x1080", "220"},
		{"1280x720", "147"},
		{"1600x900", "183"},
		{"2560x1440", "293"},
		{"3840x2160", "440"},
		{"", "220"},
		{"nonsense", "220"},
	} {
		if got := dpiFor(c.size); got != c.want {
			t.Errorf("dpiFor(%q) = %q, want %q", c.size, got, c.want)
		}
	}
}

// TestDisplaySettings checks that what the user picks is what scrcpy is asked
// for, and that a resolution takes its density along.
func TestDisplaySettings(t *testing.T) {
	a := newApp()
	a.size = "2560x1440"
	a.syncDPI()
	if a.lastSize != "2560x1440" {
		t.Errorf("the resolution was not taken up: lastSize %q", a.lastSize)
	}
	if a.dpi != "293" {
		t.Errorf("a new resolution should bring its density: dpi %q", a.dpi)
	}

	o := a.startOpts()
	if o.Size != "2560x1440" || o.DPI != "293" {
		t.Errorf("startOpts asked for %s at %s dpi", o.Size, o.DPI)
	}
	if o.Bitrate != "8M" {
		t.Errorf("startOpts asked for bit rate %q, want the default 8M", o.Bitrate)
	}

	a.bitrate = "Crisp"
	if o := a.startOpts(); o.Bitrate != "32M" {
		t.Errorf("startOpts asked for bit rate %q, want 32M", o.Bitrate)
	}
}

// TestBitrate checks the video bit rates offered, and that a name the popover
// does not know never reaches scrcpy.
func TestBitrate(t *testing.T) {
	for _, c := range []struct{ name, value, mbps string }{
		{"Low", "4M", "4 Mbps"},
		{"Medium", "8M", "8 Mbps"},
		{"High", "16M", "16 Mbps"},
		{"Crisp", "32M", "32 Mbps"},
		{"", "8M", "8 Mbps"},
		{"nonsense", "8M", "8 Mbps"},
	} {
		if got := bitrateValue(c.name); got != c.value {
			t.Errorf("bitrateValue(%q) = %q, want %q", c.name, got, c.value)
		}
		if got := bitrateMbps(c.name); got != c.mbps {
			t.Errorf("bitrateMbps(%q) = %q, want %q", c.name, got, c.mbps)
		}
	}

	if len(bitrateNames) != len(bitrates) {
		t.Fatalf("the menu offers %q, the model has %d rates", bitrateNames, len(bitrates))
	}
	if bitrateNames[0] != "Low" || bitrateNames[len(bitrateNames)-1] != "Crisp" {
		t.Errorf("the bit rates should run from Low to Crisp: %q", bitrateNames)
	}
	for _, name := range bitrateNames {
		if got := bitrateFor(name); got.Name != name {
			t.Errorf("%q is in the menu but not in the model: it resolved to %q", name, got.Name)
		}
	}
}

// TestSizeLabel checks how a resolution reads.
func TestSizeLabel(t *testing.T) {
	if got := sizeLabel("1920x1080"); got != "1920 × 1080" {
		t.Errorf("sizeLabel = %q", got)
	}
	if got := sizeLabel("nonsense"); got != "nonsense" {
		t.Errorf("sizeLabel = %q", got)
	}
}

// TestNameFor checks the names the status line shows: the app's own name when
// the list has it, and a readable guess when it does not yet.
func TestNameFor(t *testing.T) {
	a := newApp()
	a.apps = []appInfo{{Name: "Chrome", Pkg: "com.android.chrome"}}

	for _, c := range []struct{ component, want string }{
		{"com.android.chrome/com.google.android.apps.chrome.Main", "Chrome"},
		{"com.android.settings/.Settings", "Settings"},
		{"", ""},
	} {
		if got := a.nameFor(c.component); got != c.want {
			t.Errorf("nameFor(%q) = %q, want %q", c.component, got, c.want)
		}
	}
}

// TestBarMaterial checks the bar over a material, as macOS gives the window
// one: the bar paints no background of its own then, so what is behind the
// window shows through, and lays its controls out exactly as it does over
// the theme's background, where there is no material.
func TestBarMaterial(t *testing.T) {
	a := newApp()
	a.serial, a.model, a.running = "ZA223G5NQ2", "motorola edge 70", true

	for _, vibrancy := range []bool{false, true} {
		bar := ui.NewTester(a.barView, barWidth, barHeight)
		bar.SetVibrancy(vibrancy)

		for _, name := range []string{"Back", "Home", "Recents", "Shade", "Apps", "Stop", "Quit"} {
			if !bar.HasText(name) {
				t.Errorf("material %v: the bar lost %q; texts %q", vibrancy, name, bar.Texts())
			}
		}

		// The padding around the controls: the theme's background where
		// there is no material, and nothing at all over one, so that the
		// material, which the system draws behind the window, is what shows.
		got := opacity(bar.Image().At(2, barHeight-1))
		if want := !vibrancy; (got != 0) != want {
			t.Errorf("material %v: the bar's background is %v opaque, want %v", vibrancy, got, want)
		}
	}
}

// TestPanelMaterial checks the bar's panels the same way: over the material
// the system gives a popover, they too paint no panel of their own.
func TestPanelMaterial(t *testing.T) {
	a := newApp()
	a.apps = []appInfo{{Name: "Chrome", Pkg: "com.android.chrome"}}
	a.filterApps()

	w, h := popDrawer.size()
	drawer := ui.NewTester(func(c *ui.Context) { a.drawerView(c, &popover{}) }, w, h)
	drawer.SetVibrancy(true)
	for _, want := range []string{"Chrome", "Search apps"} {
		if !drawer.HasText(want) {
			t.Errorf("the drawer lost %q; texts %q", want, drawer.Texts())
		}
	}
	if got := opacity(drawer.Image().At(2, h-1)); got != 0 {
		t.Errorf("the drawer painted a panel over the material: %v", got)
	}

	dw, dh := popDisplay.size()
	display := ui.NewTester(a.displayView, dw, dh)
	display.SetVibrancy(true)
	if !display.HasText("Resolution") {
		t.Errorf("the display settings lost Resolution; texts %q", display.Texts())
	}
	if got := opacity(display.Image().At(2, dh-1)); got != 0 {
		t.Errorf("the display settings painted a panel over the material: %v", got)
	}
}

// opacity reports the alpha of a pixel, from 0 for transparent to 1.
func opacity(c color.Color) float32 {
	_, _, _, a := c.RGBA()
	return float32(a) / 0xffff
}

// TestRectOf checks the anchor a flyout hangs from.
func TestRectOf(t *testing.T) {
	got := rectOf(ui.Rect{X: 10.6, Y: 4.2, W: 60.5, H: 16.9})
	want := mygo.Rectangle{X: 10, Y: 4, Width: 60, Height: 16}
	if got != want {
		t.Errorf("rectOf = %+v, want %+v", got, want)
	}
}
