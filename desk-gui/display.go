package main

// display.go is the display's settings: the resolution, density and video bit
// rate the phone is asked for when the virtual display starts.
//
// The first two matter, and they have to agree. scrcpy asks for a size like
// "1920x1080", and Android draws its interface for a density in dots per
// inch: too low and everything is enormous, too high and everything is tiny.
// A 1080p phone draws at about 220 dpi, so each resolution offered here
// carries the density that keeps the interface the same size on screen, and
// the user can then change the density alone.
//
// The bit rate is how much room scrcpy's video stream has. A desktop is much
// harder to compress than a phone screen: it is full of small, sharp text on
// flat backgrounds, which is where the stream shows its limits, so a higher
// bit rate is what keeps the text looking like text.

import (
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

var (
	// displaySizes are the resolutions offered, from a phone screen to a
	// small desktop.
	displaySizes = []string{"1280x720", "1600x900", "1920x1080", "2560x1440", "3840x2160"}
	// displayDPIs are the densities offered, in dots per inch. 220 is the
	// one a 1080p phone uses, and 440 the one a 2160p screen at the same
	// apparent size would.
	displayDPIs = []string{"120", "147", "160", "183", "220", "240", "293", "320", "440", "480"}

	defaultSize    = "1920x1080"
	defaultDPI     = "220"
	defaultBitrate = "Medium"
)

// bitrate is a video bit rate: what the popover calls it, what scrcpy is
// asked for, and what it is worth.
type bitrate struct {
	Name  string // the word the popover uses
	Value string // scrcpy's --video-bit-rate
	Mbps  int
}

// bitrates are the video bit rates offered. The picture the phone sends is
// compressed, and a desktop's small text is what shows it: from Low, which
// keeps a phone screen moving on a weak link, up to Crisp, which has room for
// 1080p of interface without smearing the letters. Medium is scrcpy's own
// default.
var bitrates = []bitrate{
	{"Low", "4M", 4},
	{"Medium", "8M", 8},
	{"High", "16M", 16},
	{"Crisp", "32M", 32},
}

// bitrateNames are the popover's items, in the order they are offered.
var bitrateNames = func() []string {
	names := make([]string, 0, len(bitrates))
	for _, b := range bitrates {
		names = append(names, b.Name)
	}
	return names
}()

// bitrateFor looks a bit rate up by name, falling back to the default, so a
// name the popover does not know never reaches scrcpy.
func bitrateFor(name string) bitrate {
	for _, b := range bitrates {
		if b.Name == name {
			return b
		}
	}
	for _, b := range bitrates {
		if b.Name == defaultBitrate {
			return b
		}
	}
	return bitrates[0]
}

// bitrateValue is what scrcpy is asked for.
func bitrateValue(name string) string { return bitrateFor(name).Value }

// bitrateMbps reads the bit rate out, for the popover to say beside the menu.
func bitrateMbps(name string) string { return strconv.Itoa(bitrateFor(name).Mbps) + " Mbps" }

// dpiFor is the density that keeps Android's interface the same size on
// screen at a resolution: the density a 1080p phone draws it at, scaled.
func dpiFor(size string) string {
	_, h, ok := parseSize(size)
	if !ok {
		return defaultDPI
	}
	return strconv.Itoa(int(math.Round(float64(h) * 220 / 1080)))
}

// parseSize reads scrcpy's display size, "1920x1080".
func parseSize(size string) (w, h int, ok bool) {
	ws, hs, found := strings.Cut(size, "x")
	if !found {
		return 0, 0, false
	}
	w, errW := strconv.Atoi(ws)
	h, errH := strconv.Atoi(hs)
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// sizeLabel is a resolution as it reads: "1920 × 1080".
func sizeLabel(size string) string {
	w, h, ok := parseSize(size)
	if !ok {
		return size
	}
	return strconv.Itoa(w) + " × " + strconv.Itoa(h)
}

// labelWidth is the room a setting's name takes, and unitWidth the room beside
// its control for what the value is worth, so the three controls line up.
const (
	labelWidth = 80
	unitWidth  = 66
)

// settingRow is one line of the display settings: the name of the setting, its
// control, and the room for a unit beside it. The room is taken even when there
// is nothing to put in it, so the controls below each other start and end in
// the same place.
func settingRow(c *ui.Context, label, unit string, control func()) {
	t := c.Theme()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(t.TextMuted).Width(labelWidth)
		control()
		ui.Row(c).Width(unitWidth).AlignItems(ui.Center).Children(func() {
			if unit != "" {
				ui.Text(c, unit).FontSize(11).TextColor(t.TextMuted)
			}
		})
	})
}

// displayView is the popover the Display button opens: what the phone is
// asked for when the display starts.
func (a *app) displayView(c *ui.Context) {
	t := c.Theme()
	a.syncDPI()

	panel := ui.Column(c).Fill()
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	} else {
		panel.Background(t.Background).Radius(t.Radius + 4).Clip()
	}

	panel.Padding(14).Gap(12).Children(func() {
		ui.Text(c, "Display").FontSize(13).Bold()

		settingRow(c, "Resolution", "", func() {
			ui.Select(c, &a.size, displaySizes).Label("Resolution").Grow(1)
		})
		settingRow(c, "Density", "", func() {
			ui.Select(c, &a.dpi, displayDPIs).Label("Density (dots per inch)").Grow(1)
		})
		settingRow(c, "Bit rate", bitrateMbps(a.bitrate), func() {
			ui.Select(c, &a.bitrate, bitrateNames).Label("Video bit rate").Grow(1)
		})

		ui.Text(c, a.displaySummary()).FontSize(11).TextColor(t.TextMuted)
	})
}

// syncDPI takes up a resolution the user just picked, with the density that
// goes with it; the density can then be changed on its own.
func (a *app) syncDPI() {
	if a.size != a.lastSize {
		a.lastSize = a.size
		a.dpi = dpiFor(a.size)
	}
}

// displaySummary says what the settings add up to, and when they apply.
func (a *app) displaySummary() string {
	s := sizeLabel(a.size) + " at " + a.dpi + " dpi · " + bitrateMbps(a.bitrate) + " video"
	if a.running {
		return s + " · stop and start the display to apply it"
	}
	return s + " · applied when the display starts"
}
