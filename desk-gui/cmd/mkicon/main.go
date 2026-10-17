// Command mkicon draws Desk's application icon: a macOS squircle in the
// app's colors, with the display it puts on the phone and the phone in
// front of it.
//
//	go run ./cmd/mkicon -o resources/icon.png
//
// It writes the PNG that `mygo build` turns into the app's .icns, so the
// icon lives in source, not in a binary nobody can edit.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func main() {
	out := flag.String("o", "resources/icon.png", "PNG to write")
	size := flag.Int("size", 1024, "side of the square icon, in pixels")
	flag.Parse()

	if err := write(*out, *size); err != nil {
		fmt.Fprintln(os.Stderr, "mkicon:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", *out)
}

// The icon's geometry, in fractions of its side, and its colors.
const (
	margin   = 0.090 // the transparent room around the squircle
	squircle = 5.0   // the exponent of the shape's superellipse
	radius   = 0.20  // of the squircle's side, as macOS rounds its icons

	panelX0, panelY0, panelX1, panelY1 = 0.165, 0.235, 0.835, 0.555
	panelR                             = 0.055

	phoneX0, phoneY0, phoneX1, phoneY1 = 0.395, 0.395, 0.665, 0.805
	phoneR                             = 0.075
	phoneBorder                        = 0.027
)

var (
	topLeft   = rgb(0x4b, 0x7b, 0xff)
	bottRight = rgb(0x8b, 0x3d, 0xff)
	white     = rgb(0xff, 0xff, 0xff)
)

func rgb(r, g, b uint8) [3]float64 {
	return [3]float64{float64(r) / 255, float64(g) / 255, float64(b) / 255}
}

// write renders the icon at size and writes it as a PNG.
func write(path string, size int) error {
	// 4×4 samples per pixel: enough for the shape's curves to look smooth
	// without a blur.
	const samples = 4
	var buf [4]float64

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			buf = [4]float64{}
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/samples) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/samples) / float64(size)
					c := sample(x, y)
					for i := range buf {
						buf[i] += c[i] / (samples * samples)
					}
				}
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: byte(buf[0]*255 + 0.5),
				G: byte(buf[1]*255 + 0.5),
				B: byte(buf[2]*255 + 0.5),
				A: byte(buf[3]*255 + 0.5),
			})
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// sample is the icon's color at x, y in 0..1 of its side, unmultiplied,
// with the background opaque: the phone in front of the display, on the
// squircle's gradient, on nothing.
func sample(x, y float64) [4]float64 {
	if !inSquircle(x, y) {
		return [4]float64{}
	}

	c := gradient(x, y)
	// The display, in white.
	if inRoundedRect(x, y, panelX0, panelY0, panelX1, panelY1, panelR) {
		c = mix(c, white, 0.94)
	}
	// The phone in front of it: its outline in white, its inside the
	// gradient again, which reads as a phone-shaped hole in the display.
	switch {
	case inRoundedRect(x, y, phoneX0, phoneY0, phoneX1, phoneY1, phoneR):
		c = white
		if inRoundedRect(x, y,
			phoneX0+phoneBorder, phoneY0+phoneBorder,
			phoneX1-phoneBorder, phoneY1-phoneBorder, phoneR-phoneBorder/2) {
			c = gradient(x, y)
			// A thin band reads as the phone's screen edge.
			if inRoundedRect(x, y,
				phoneX0+2*phoneBorder, phoneY0+2*phoneBorder,
				phoneX1-2*phoneBorder, phoneY1-2*phoneBorder, phoneR-phoneBorder) {
				c = mix(gradient(x, y), white, 0.18)
			}
		}
	}
	return [4]float64{c[0], c[1], c[2], 1}
}

// inSquircle reports whether x, y is inside the icon's shape: a
// superellipse, which is how macOS rounds its icons.
func inSquircle(x, y float64) bool {
	a := 0.5 - margin
	u, v := math.Abs(x-0.5)/a, math.Abs(y-0.5)/a
	return math.Pow(u, squircle)+math.Pow(v, squircle) <= 1
}

// gradient is the icon's background at x, y: blue at the top left to
// violet at the bottom right.
func gradient(x, y float64) [3]float64 {
	t := (x + y) / 2 // along the diagonal, before and after the shape
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	// A gentler middle than a straight mix: Oklab-ish, by eye.
	t = t * t * (3 - 2*t)
	return mix(topLeft, bottRight, t)
}

// inRoundedRect reports whether x, y is inside the rounded rectangle with
// those fractions of the icon's side: inside the rectangle shrunk by r on
// every side, or within r of it.
func inRoundedRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	if r <= 0 {
		return true
	}
	cx := clamp(x, x0+r, x1-r)
	cy := clamp(y, y0+r, y1-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func mix(a [3]float64, b [3]float64, t float64) [3]float64 {
	return [3]float64{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}
