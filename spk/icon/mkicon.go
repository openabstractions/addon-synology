// mkicon writes the two PNG icons a Synology package archive expects
// (PACKAGE_ICON.PNG, PACKAGE_ICON_256.PNG) using nothing but the standard
// library's image/png encoder.
//
// This exists for the same reason deploy/mkimage writes a Docker image
// directly: PNG is a documented format, not a black box, so there is no
// reason to depend on an image-editing tool that may not be on a developer's
// machine, or to check a binary blob into the repository. build.sh runs
// this with `go run` and throws the output away between builds.
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
	size := flag.Int("size", 256, "icon width/height in pixels")
	out := flag.String("out", "", "output PNG path")
	flag.Parse()
	if *out == "" || *size <= 0 {
		flag.Usage()
		os.Exit(2)
	}

	if err := writeIcon(*out, *size); err != nil {
		fmt.Fprintln(os.Stderr, "mkicon:", err)
		os.Exit(1)
	}
}

func writeIcon(path string, n int) error {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))

	// A plain rounded square with a lighter dot in the middle: a store, and
	// one job in it. Nothing more is claimed than that.
	bg := color.NRGBA{0x24, 0x3b, 0x53, 0xff} // slate blue
	dot := color.NRGBA{0xf2, 0xf5, 0xf7, 0xff} // near-white
	fn := float64(n)
	radius := fn * 0.18
	cx, cy := fn/2, fn/2
	dotRadius := fn * 0.26

	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			if !insideRoundedSquare(px, py, fn, radius) {
				continue // left transparent
			}
			c := bg
			if math.Hypot(px-cx, py-cy) <= dotRadius {
				c = dot
			}
			img.SetNRGBA(x, y, c)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// insideRoundedSquare is the standard rounded-box signed-distance test: true
// for points no further than r from the n x n square's core rectangle.
func insideRoundedSquare(x, y, n, r float64) bool {
	half := n / 2
	rx := math.Abs(x-half) - (half - r)
	ry := math.Abs(y-half) - (half - r)
	qx, qy := math.Max(rx, 0), math.Max(ry, 0)
	return math.Hypot(qx, qy) <= r
}
