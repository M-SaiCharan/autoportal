package tray

import (
	"image"
	"image/color"
	"runtime"

	"github.com/M-SaiCharan/autoportal/internal/agent"
	"github.com/M-SaiCharan/autoportal/internal/icon"
)

// Status icons: a coloured disc with a white glyph, legible on light and
// dark menu bars alike.

var (
	green = color.NRGBA{0x2E, 0x9E, 0x44, 0xFF}
	amber = color.NRGBA{0xE0, 0x9A, 0x00, 0xFF}
	red   = color.NRGBA{0xD9, 0x3B, 0x30, 0xFF}
	gray  = color.NRGBA{0x8E, 0x8E, 0x93, 0xFF}
	blue  = color.NRGBA{0x2F, 0x6F, 0xD1, 0xFF}
	slate = color.NRGBA{0x5B, 0x6B, 0x85, 0xFF}
	white = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

var (
	disc   = icon.Disc
	stroke = icon.Stroke
	union  = icon.Union
)

var glyphs = map[string]icon.Shape{
	"check": union(stroke(0.28, 0.52, 0.43, 0.67, 0.075), stroke(0.43, 0.67, 0.72, 0.36, 0.075)),
	"dots":  union(disc(0.29, 0.5, 0.075), disc(0.5, 0.5, 0.075), disc(0.71, 0.5, 0.075)),
	"bang":  union(stroke(0.5, 0.26, 0.5, 0.56, 0.07), disc(0.5, 0.73, 0.075)),
	"pause": union(stroke(0.40, 0.32, 0.40, 0.68, 0.07), stroke(0.60, 0.32, 0.60, 0.68, 0.07)),
	"dash":  stroke(0.30, 0.5, 0.70, 0.5, 0.07),
	"cross": union(stroke(0.34, 0.34, 0.66, 0.66, 0.07), stroke(0.34, 0.66, 0.66, 0.34, 0.07)),
	"plus":  union(stroke(0.5, 0.30, 0.5, 0.70, 0.07), stroke(0.30, 0.5, 0.70, 0.5, 0.07)),
}

type look struct {
	fill  color.NRGBA
	glyph string
}

func lookFor(k agent.Kind) look {
	switch k {
	case agent.SignedIn:
		return look{green, "check"}
	case agent.SigningIn, agent.Checking:
		return look{amber, "dots"}
	case agent.PortalError:
		return look{amber, "bang"}
	case agent.Rejected, agent.Challenge, agent.CertChanged:
		return look{red, "bang"}
	case agent.Paused:
		return look{slate, "pause"}
	case agent.OffCampus:
		return look{gray, "dash"}
	case agent.NoNetwork:
		return look{gray, "cross"}
	default: // NeedsSetup
		return look{blue, "plus"}
	}
}

func render(size int, l look) *image.NRGBA {
	return icon.Render(size,
		icon.Layer{Shape: disc(0.5, 0.5, 0.47), Paint: icon.Solid(l.fill)},
		icon.Layer{Shape: glyphs[l.glyph], Paint: icon.Solid(white)},
	)
}

// iconBytes returns the icon for k in the format this OS's tray expects.
func iconBytes(k agent.Kind) []byte {
	l := lookFor(k)
	if runtime.GOOS == "windows" {
		var imgs []*image.NRGBA
		for _, s := range []int{16, 24, 32, 48} {
			imgs = append(imgs, render(s, l))
		}
		return icon.ICO(imgs...)
	}
	return icon.PNG(render(64, l))
}
