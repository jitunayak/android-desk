package main

// Desk — a desktop-mode wrapper for Android phones that have no desktop mode.
//
// It is one control surface around a second display:
//
//   - the bar: a frameless, always-on-top strip with the navigation buttons
//     that the virtual display itself lacks, which sits directly above the
//     scrcpy window it launches;
//   - the app drawer: every launchable app on the phone, searchable, in a
//     flyout under the bar's Apps button, which starts apps on the virtual
//     display as freeform windows.
//
// The video is scrcpy's own window, placed under the bar by the Start button,
// so the whole thing reads as one window.

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Capture mode renders the windows to PNGs and exits, which checks the layout
// on a machine where the screen cannot be photographed:
//
//	go run . -capture bar.png -capture-drawer drawer.png
var (
	flagCapture        = flag.String("capture", "", "write a PNG of the control bar to this path and exit")
	flagCaptureDrawer  = flag.String("capture-drawer", "", "write a PNG of the app drawer, filled with the phone's apps, and exit")
	flagCaptureDisplay = flag.String("capture-display", "", "write a PNG of the display settings popover and exit")
	flagAutoStart      = flag.Bool("auto-start", false, "start the virtual display as soon as the device is found")
)

const (
	barWidth     = 940
	barHeight    = 60
	drawerWidth  = 580
	drawerHeight = 680
	gap          = 6
)

type app struct {
	dev *ctl

	// wmu guards the bar, which goroutines read.
	wmu sync.Mutex
	bar *mygo.Window

	// dmu guards the popover the bar has open, which goroutines read too.
	dmu     sync.Mutex
	pop     *popover
	opening bool // a popover is being built, so a second click must wait

	serial string
	model  string

	running bool
	busy    bool
	status  string // transient message: progress or error
	top     string // component resumed on the display
	display int

	apps      []appInfo
	query     string
	lastQuery string
	filtered  []int
	freeform  bool
	loading   atomic.Bool

	// The virtual display to ask the phone for, and the density and video
	// bit rate that go with it: see display.go.
	size     string
	dpi      string
	bitrate  string
	lastSize string

	quitting bool
}

// popover is the flyout the bar has open, under the button that opened it:
// the app drawer or the display's settings. A focusable flyout is closed, and
// destroyed, by the system the moment the user clicks elsewhere, so a new one
// is built every time and its state goes with its window.
type popover struct {
	kind popoverKind
	win  *mygo.Window
	list ui.ListState // the drawer's list, and nothing else
}

type popoverKind int

const (
	popDrawer popoverKind = iota
	popDisplay
)

// size is the size of a popover of this kind, in DIPs.
func (k popoverKind) size() (width, height int) {
	if k == popDisplay {
		return 460, 200
	}
	return drawerWidth, drawerHeight
}

// newApp is the app at its defaults: no device yet, and a 1080p display to ask
// the phone for.
func newApp() *app {
	return &app{
		dev:      &ctl{},
		size:     defaultSize,
		dpi:      defaultDPI,
		bitrate:  defaultBitrate,
		lastSize: defaultSize,
	}
}

// capturing reports whether this run only renders windows to PNGs, which the
// flags ask for and which then exits.
func capturing() bool {
	return *flagCapture != "" || *flagCaptureDrawer != "" || *flagCaptureDisplay != ""
}

// barMaterial is the material the bar's window is a pane of, where the system
// has one: macOS and the other desktops that draw materials blur what is
// behind the window and show it through. A capture renders what the views
// paint, and the material is not part of that, so it is left out of one and
// the views paint their own surfaces instead.
func barMaterial() mygo.Vibrancy {
	if capturing() {
		return mygo.VibrancyNone
	}
	return mygo.VibrancyPopover
}

// ---------------------------------------------------------------- startup

func main() {
	flag.Parse()
	a := newApp()

	mygo.App.WhenReady(func() {
		wa := mygo.Screen.PrimaryDisplay().WorkArea
		x := wa.X + (wa.Width-barWidth)/2
		y := wa.Y + 48

		bar := mygo.NewWindow(mygo.WindowOptions{
			Title:         "Desk",
			X:             x,
			Y:             y,
			Width:         barWidth,
			Height:        barHeight,
			MinWidth:      barWidth,
			MinHeight:     barHeight,
			Frameless:     true,
			DisableResize: true,
			AlwaysOnTop:   true,
			// Where the system has a material, the bar is a pane of it and
			// the desktop shows through, blurred, as macOS draws its own
			// floating bars; the view paints no background then. Where it
			// has none, the view paints the theme's.
			Transparent: true,
			Vibrancy:    barMaterial(),
			Content:     ui.View(a.barView),
		})

		a.wmu.Lock()
		a.bar = bar
		a.wmu.Unlock()

		if *flagCapture != "" || *flagCaptureDrawer != "" || *flagCaptureDisplay != "" {
			go a.captureAndExit()
			return
		}

		// Closing the bar ends the session, whichever way it was closed.
		bar.OnClosed(func() { mygo.App.Quit() })

		go a.boot()
	})

	// Stop scrcpy before the app goes, so no orphaned mirror is left behind.
	mygo.App.OnWillQuit(func(e *mygo.QuitEvent) {
		if a.quitting {
			return
		}
		a.quitting = true
		e.PreventDefault()
		go func() {
			_ = a.dev.stop()
			mygo.RunOnMain(func() { mygo.App.Exit(0) })
		}()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// boot probes the device and reads the app list, then watches the display.
func (a *app) boot() {
	a.publish(func() { a.status = "looking for the device…" })

	serial, model, err := a.dev.device()
	if err != nil {
		a.publish(func() { a.status = err.Error() })
		return
	}
	a.publish(func() {
		a.serial, a.model = serial, model
		a.status = ""
		if *flagAutoStart {
			a.toggleRun()
		}
	})

	a.loadApps()
	go a.watch()
}

// watch keeps the bar honest: it notices scrcpy exiting on its own and shows
// what is currently on the display.
func (a *app) watch() {
	for {
		time.Sleep(4 * time.Second)

		a.wmu.Lock()
		live := a.bar != nil
		a.wmu.Unlock()
		if !live {
			return
		}

		running := a.dev.isAlive()
		top, display := "", 0
		if running {
			top = a.dev.topActivity()
			display = a.dev.displayID()
		}
		a.publish(func() {
			a.running = running
			a.top = top
			if display != 0 {
				a.display = display
			}
			if !a.busy && running {
				a.status = "" // settled: show what is running instead
			}
		})
	}
}

// publish runs fn on the main thread, where the views read this state, and
// redraws the windows.
func (a *app) publish(fn func()) {
	a.wmu.Lock()
	bar := a.bar
	a.wmu.Unlock()

	if bar == nil {
		fn()
		return
	}
	bar.Update(fn)

	// The popover shows the same state, so it has to redraw too.
	a.dmu.Lock()
	p := a.pop
	a.dmu.Unlock()
	if p != nil {
		p.win.Update(func() {})
	}
}

// ---------------------------------------------------------------- the bar

func (a *app) barView(c *ui.Context) {
	t := c.Theme()

	// Where the system has a material, the bar is a pane of it and what is
	// behind the window shows through, blurred, so the bar paints no
	// background of its own. Where it has none, the theme's background
	// stands in for the material and the bar is opaque.
	bar := ui.Row(c).Fill().PaddingX(12).Gap(10).AlignItems(ui.Center)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	} else {
		bar.Background(t.Background)
	}

	bar.Children(func() {
		ui.Column(c).Gap(0).Width(165).Justify(ui.Center).DragWindow().Children(func() {
			ui.Text(c, a.titleText()).FontSize(13).Bold().SingleLine()
			ui.Text(c, a.subtitleText()).FontSize(10.5).TextColor(t.TextMuted).SingleLine()
		})

		// A toolbar takes no face of its own until hovered, as macOS does,
		// and parks what does not fit in an overflow menu at its end.
		ui.Toolbar(c, func() {
			a.navButton(c, "Back", iconBack, "Back", keyBack)
			a.navButton(c, "Home", iconHome, "Home", keyHome)
			a.navButton(c, "Recents", iconRecents, "Recent apps", keyRecents)
			a.navButton(c, "Shade", iconShades, "Pull down the notification shade", keyShades)

			ui.Spacer(c).DragWindow()

			ui.Text(c, a.statusText()).FontSize(11).TextColor(t.TextMuted).SingleLine().MaxWidth(200)

			// Each of these hangs its flyout under itself, so each hands
			// out its own box.
			anchoredButton(c, iconApps, "Apps", "Every app on the phone", func(r ui.Rect) {
				a.openPopover(popDrawer, r)
			})
			toolbarIcon(c, iconList, "Phone apps", "The phone's own list of apps, on the display",
				!a.running || a.busy, a.openPhoneDrawer)
			toolbarIcon(c, iconShot, "Shot", "Save a screenshot of the display", !a.running, a.saveShot)
			anchoredIcon(c, iconDisplay, "Display", "The resolution and density of the display",
				func(r ui.Rect) { a.openPopover(popDisplay, r) })

			if a.running {
				toolbarButton(c, iconStop, "Stop", "Stop the display", false, a.busy, a.toggleRun)
			} else {
				toolbarButton(c, iconPlay, "Start", "Start the display under this bar", true, a.busy, a.toggleRun)
			}

			toolbarIcon(c, iconQuit, "Quit", "Stop the display and quit", false, mygo.App.Quit)
		}).Label("Desk controls").Grow(1)
	})
}

// navButton sends one navigation key to the virtual display, which has no
// navigation bar of its own.
func (a *app) navButton(c *ui.Context, name string, ic *ui.SVG, tip string, code int) {
	toolbarIcon(c, ic, name, tip, !a.running || a.busy, func() {
		go func() {
			if err := a.dev.pressKey(code); err != nil {
				a.publish(func() { a.status = err.Error() })
			}
		}()
	})
}

func (a *app) titleText() string {
	if a.model != "" {
		return a.model
	}
	return "Desk"
}

func (a *app) subtitleText() string {
	if a.serial == "" {
		return "no device — connect it over USB"
	}
	if a.running {
		return fmt.Sprintf("%s · display %d", a.serial, a.display)
	}
	return a.serial + " · display stopped"
}

func (a *app) statusText() string {
	switch {
	case a.status != "":
		return a.status
	case a.running:
		if name := a.nameFor(a.top); name != "" {
			return "▶ " + name
		}
		return "▶ running"
	default:
		return "display stopped"
	}
}

// nameFor turns a component like "com.android.chrome/…Main" into "Chrome".
func (a *app) nameFor(component string) string {
	if component == "" {
		return ""
	}
	pkg, _, _ := strings.Cut(component, "/")
	for _, ap := range a.apps {
		if ap.Pkg == pkg {
			return ap.Name
		}
	}
	// The list is not loaded yet: "com.android.chrome" reads better as "Chrome".
	parts := strings.Split(pkg, ".")
	last := parts[len(parts)-1]
	if last == "" {
		return pkg
	}
	return strings.ToUpper(last[:1]) + last[1:]
}

// ---------------------------------------------------------------- the drawer

// drawerView is the app drawer: every launchable app on the phone, with a
// search field to narrow it. Over the system's popover material it draws no
// panel of its own; where there is none, it draws one, as a flyout brings
// none itself.
func (a *app) drawerView(c *ui.Context, p *popover) {
	t := c.Theme()
	if a.query != a.lastQuery {
		a.filterApps()
	}

	panel := ui.Column(c).Fill()
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	} else {
		panel.Background(t.Background).Radius(t.Radius + 4).Clip()
	}

	panel.Padding(12).Gap(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.SearchField(c, &a.query).Label("Search apps").Grow(1)
			ui.Checkbox(c, &a.freeform, "Freeform").Tooltip("Open apps as resizable windows")
		})

		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "%d of %d apps", len(a.filtered), len(a.apps)).FontSize(11).TextColor(t.TextMuted)
			ui.Spacer(c)
			ui.Button(c, "").Label("Reload").Tooltip("Read the app list again").Disabled(a.loading.Load()).
				OnClick(func() { a.loadApps() }).
				Children(func() { ui.Icon(c, iconReload).Size(14, 14) })
			ui.Button(c, "").Label("Phone drawer").Tooltip("Open the phone's own app drawer").
				Disabled(!a.running).OnClick(a.openPhoneDrawer).
				Children(func() { ui.Icon(c, iconApps).Size(14, 14) })
			ui.Button(c, "").Label("Phone home").Tooltip("Go to the phone's own home screen").
				Disabled(!a.running).OnClick(a.phoneHome).
				Children(func() { ui.Icon(c, iconPhone).Size(14, 14) })
		})

		ui.List(c, &p.list, len(a.filtered), func(i int) { a.appRow(c, i) }).Grow(1)
	})
}

func (a *app) appRow(c *ui.Context, i int) {
	t := c.Theme()
	ap := a.apps[a.filtered[i]]

	ui.ButtonBase(c).Disabled(!a.running || a.busy).OnClick(func() { a.launchApp(ap) }).
		Children(func() {
			ui.Row(c).FillWidth().Padding(4, 8).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Avatar(c, ap.Name, nil)
				ui.Column(c).Gap(0).Grow(1).Children(func() {
					ui.Text(c, ap.Name).FontSize(13).SingleLine()
					ui.Text(c, ap.Pkg).FontSize(10).TextColor(t.TextMuted).SingleLine()
				})
				ui.Text(c, "Launch").FontSize(11).TextColor(t.TextMuted)
			})
		})
}

func (a *app) filterApps() {
	a.lastQuery = a.query
	q := strings.ToLower(strings.TrimSpace(a.query))
	a.filtered = a.filtered[:0]
	for i, ap := range a.apps {
		if q == "" ||
			strings.Contains(strings.ToLower(ap.Name), q) ||
			strings.Contains(strings.ToLower(ap.Pkg), q) {
			a.filtered = append(a.filtered, i)
		}
	}
}

// ---------------------------------------------------------------- actions

func (a *app) loadApps() {
	if !a.loading.CompareAndSwap(false, true) {
		return
	}
	a.publish(func() { a.status = "reading the app list…" })

	go func() {
		apps, err := a.dev.listApps()
		a.publish(func() {
			a.loading.Store(false)
			if err != nil {
				a.status = err.Error()
				return
			}
			a.apps = apps
			a.filterApps()
			a.status = ""
		})
	}()
}

// openPopover shows the flyout under the button that asked for it, unless one
// is already open: a focusable flyout closes itself when its anchor is
// clicked, and returning here is what keeps that click from opening it again.
func (a *app) openPopover(kind popoverKind, anchor ui.Rect) {
	a.dmu.Lock()
	busy := a.pop != nil || a.opening
	if !busy {
		a.opening = true
	}
	a.dmu.Unlock()
	if busy {
		return
	}
	go a.openPopoverNow(kind, anchor)
}

// openPopoverNow creates and shows the flyout, and returns it, for callers
// that need to wait for it, as the capture does.
func (a *app) openPopoverNow(kind popoverKind, anchor ui.Rect) *popover {
	defer func() {
		a.dmu.Lock()
		a.opening = false
		a.dmu.Unlock()
	}()

	a.wmu.Lock()
	bar := a.bar
	a.wmu.Unlock()
	if bar == nil {
		return nil // no window to hang it from: before startup, or in a test
	}

	width, height := kind.size()
	p := &popover{kind: kind}
	p.win = mygo.NewFlyout(mygo.FlyoutOptions{
		Parent:    bar,
		Anchor:    rectOf(anchor),
		Placement: mygo.PlacementBottomStart,
		Gap:       gap,
		Width:     width,
		Height:    height,
		Shadow:    true,
		// On macOS the flyout becomes a real popover, which the system
		// draws with its own material, corners, shadow and arrow; its
		// content paints no background there (drawerView asks). A capture
		// has no material, and the content paints its own panel.
		Popover:   !capturing(),
		Focusable: true,
		Content:   ui.View(func(c *ui.Context) { a.popoverView(c, p) }),
	})
	p.win.OnClosed(func() {
		a.dmu.Lock()
		a.pop = nil
		a.dmu.Unlock()
	})

	a.dmu.Lock()
	a.pop = p
	a.dmu.Unlock()

	p.win.Show()
	p.win.Focus()
	return p
}

// popoverView is the body of the bar's flyouts, one per kind.
func (a *app) popoverView(c *ui.Context, p *popover) {
	if p.kind == popDisplay {
		a.displayView(c)
		return
	}
	a.drawerView(c, p)
}

// hidePopover closes the open flyout, as after launching an app from the
// drawer.
func (a *app) hidePopover() {
	a.dmu.Lock()
	p := a.pop
	a.dmu.Unlock()
	if p != nil {
		p.win.Close()
	}
}

// rectOf turns a box of the bar's content into the rectangle a flyout
// anchors to.
func rectOf(r ui.Rect) mygo.Rectangle {
	return mygo.Rectangle{X: int(r.X), Y: int(r.Y), Width: int(r.W), Height: int(r.H)}
}

func (a *app) toggleRun() {
	if a.busy {
		return
	}
	wasRunning := a.running
	needApps := len(a.apps) == 0
	var o startOpts
	if !wasRunning {
		o = a.startOpts()
	}

	a.busy = true
	if wasRunning {
		a.status = "stopping the display…"
	} else {
		a.status = "starting the display…"
	}

	go func() {
		var err error
		if wasRunning {
			err = a.dev.stop()
		} else {
			err = a.dev.start(o)
		}

		a.publish(func() {
			a.busy = false
			switch {
			case err != nil:
				a.status = err.Error()
				a.running = false
			case wasRunning:
				a.status = ""
				a.running = false
				a.top = ""
			default:
				a.status = ""
				a.running = true
				a.display = a.dev.displayID()
			}
		})

		// The status line names the running app, so the list has to exist.
		if needApps && err == nil {
			a.loadApps()
		}
	}()
}

// startOpts puts scrcpy's window directly under the bar, at its width, so the
// two read as one window, and asks the phone for the display size, density and
// video bit rate the user chose.
func (a *app) startOpts() startOpts {
	o := startOpts{Size: a.size, DPI: a.dpi, Bitrate: bitrateValue(a.bitrate), Title: "Desk Display"}

	a.wmu.Lock()
	bar := a.bar
	a.wmu.Unlock()
	if bar != nil {
		b := bar.Bounds()
		o.WindowX = b.X
		o.WindowY = b.Y + barHeight + gap
		o.WindowWidth = b.Width
	}
	return o
}

func (a *app) launchApp(ap appInfo) {
	freeform := a.freeform
	a.hidePopover()
	go func() {
		err := a.dev.launch(ap.Pkg, freeform)
		a.publish(func() {
			if err != nil {
				a.status = err.Error()
				return
			}
			if freeform {
				a.status = ap.Name + " opened in a window"
			} else {
				a.status = ap.Name + " opened"
			}
		})
	}()
}

func (a *app) openPhoneDrawer() {
	go func() {
		if err := a.dev.openAppDrawer(); err != nil {
			a.publish(func() { a.status = err.Error() })
		}
	}()
}

func (a *app) phoneHome() {
	go func() {
		if err := a.dev.goHome(); err != nil {
			a.publish(func() { a.status = err.Error() })
		}
	}()
}

func (a *app) saveShot() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	dir := filepath.Join(home, "Pictures", "Desk Shots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.status = err.Error()
		return
	}
	path := filepath.Join(dir, "desk-"+time.Now().Format("20060102-150405")+".png")
	a.status = "saving a screenshot…"

	go func() {
		err := a.dev.screenshot(path)
		a.publish(func() {
			if err != nil {
				a.status = err.Error()
				return
			}
			a.status = "saved " + filepath.Base(path)
		})
		if err == nil {
			mygo.Shell.ShowItemInFolder(path)
		}
	}()
}

// ---------------------------------------------------------------- capture

// captureAndExit renders the windows to the paths given on the command line,
// which is how the layout is checked where the screen cannot be photographed.
func (a *app) captureAndExit() {
	defer mygo.RunOnMain(func() { mygo.App.Exit(0) })

	if serial, model, err := a.dev.device(); err == nil {
		a.publish(func() {
			a.serial, a.model = serial, model
			a.running = true
			a.display = 260
			a.top = "com.android.chrome/com.google.android.apps.chrome.Main"
		})
	}

	if *flagCapture != "" {
		time.Sleep(700 * time.Millisecond)
		a.writeCapture(a.barWindow(), *flagCapture)
	}

	if *flagCaptureDrawer != "" {
		if apps, err := a.dev.listApps(); err == nil {
			a.publish(func() {
				a.apps = apps
				a.filterApps()
			})
		}

		// Anchor it to the middle of the bar's buttons, as its own button
		// would, since there is no pointer to click one in a capture.
		if p := a.openPopoverNow(popDrawer, ui.Rect{X: barWidth - 300, Y: barHeight - 8, W: 60, H: 16}); p != nil {
			time.Sleep(1800 * time.Millisecond)
			a.writeCapture(p.win, *flagCaptureDrawer)
		}
	}

	if *flagCaptureDisplay != "" {
		if p := a.openPopoverNow(popDisplay, ui.Rect{X: barWidth - 250, Y: barHeight - 8, W: 60, H: 16}); p != nil {
			time.Sleep(900 * time.Millisecond)
			a.writeCapture(p.win, *flagCaptureDisplay)
		}
	}
}

func (a *app) barWindow() *mygo.Window {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	return a.bar
}

func (a *app) writeCapture(w *mygo.Window, path string) {
	if w == nil {
		return
	}
	png, err := w.CapturePage()
	if err != nil {
		log.Println("capture:", err)
		return
	}
	if err := os.WriteFile(path, png, 0o644); err != nil {
		log.Println("capture:", err)
		return
	}
	log.Println("wrote", path)
}
