package icon

import (
	"image"
	"image/color"
)

var (
	appTop    = color.NRGBA{0x3B, 0x82, 0xF6, 0xFF}
	appBottom = color.NRGBA{0x1D, 0x4E, 0xD8, 0xFF}
	white     = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	badge     = color.NRGBA{0x22, 0xA3, 0x4A, 0xFF}
)

// App draws the application icon: a Wi-Fi symbol on a blue rounded
// square, with a green check badge.
func App(size int) *image.NRGBA {
	bg := Vertical(appTop, appBottom)
	const cx, cy = 0.45, 0.69
	return Render(size,
		Layer{RoundRect(0.08, 0.08, 0.92, 0.92, 0.19), bg},
		Layer{Union(
			Arc(cx, cy, 0.15, 0.045, 45),
			Arc(cx, cy, 0.27, 0.045, 45),
			Arc(cx, cy, 0.39, 0.045, 45),
			Disc(cx, cy, 0.058),
		), Solid(white)},
		Layer{Disc(0.71, 0.71, 0.175), bg}, // gap between the arcs and the badge
		Layer{Disc(0.71, 0.71, 0.14), Solid(badge)},
		Layer{Union(
			Stroke(0.645, 0.715, 0.69, 0.76, 0.024),
			Stroke(0.69, 0.76, 0.78, 0.665, 0.024),
		), Solid(white)},
	)
}

// AppICO is the Windows application icon.
func AppICO() []byte {
	var imgs []*image.NRGBA
	for _, s := range []int{16, 24, 32, 48, 64, 256} {
		imgs = append(imgs, App(s))
	}
	return ICO(imgs...)
}
