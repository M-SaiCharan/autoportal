package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"runtime"

	"github.com/M-SaiCharan/autoportal/internal/agent"
)

// Icons are drawn in code: a coloured disc with a white glyph. That keeps
// the repository free of binary assets and stays legible on light and dark
// menu bars alike.

var (
	green = color.NRGBA{0x2E, 0x9E, 0x44, 0xFF}
	amber = color.NRGBA{0xE0, 0x9A, 0x00, 0xFF}
	red   = color.NRGBA{0xD9, 0x3B, 0x30, 0xFF}
	gray  = color.NRGBA{0x8E, 0x8E, 0x93, 0xFF}
	blue  = color.NRGBA{0x2F, 0x6F, 0xD1, 0xFF}
	slate = color.NRGBA{0x5B, 0x6B, 0x85, 0xFF}
)

// shape reports whether point (x, y) in the unit square is inside.
type shape func(x, y float64) bool

func disc(cx, cy, r float64) shape {
	return func(x, y float64) bool { return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r }
}

// stroke is a line segment with round caps and half-width w.
func stroke(x1, y1, x2, y2, w float64) shape {
	return func(x, y float64) bool {
		dx, dy := x2-x1, y2-y1
		t := ((x-x1)*dx + (y-y1)*dy) / (dx*dx + dy*dy)
		t = math.Max(0, math.Min(1, t))
		px, py := x1+t*dx-x, y1+t*dy-y
		return px*px+py*py <= w*w
	}
}

func union(ss ...shape) shape {
	return func(x, y float64) bool {
		for _, s := range ss {
			if s(x, y) {
				return true
			}
		}
		return false
	}
}

var glyphs = map[string]shape{
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

// render draws the icon at size×size with 4×4 supersampling.
func render(size int, l look) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	bg := disc(0.5, 0.5, 0.47)
	fg := glyphs[l.glyph]
	const ss = 4
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var inBg, inFg int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					if bg(x, y) {
						inBg++
						if fg(x, y) {
							inFg++
						}
					}
				}
			}
			if inBg == 0 {
				continue
			}
			// Blend white glyph over the fill, then apply coverage as alpha.
			f := float64(inFg) / float64(inBg)
			mix := func(c uint8) uint8 { return uint8(float64(c)*(1-f) + 255*f + 0.5) }
			img.SetNRGBA(px, py, color.NRGBA{
				R: mix(l.fill.R), G: mix(l.fill.G), B: mix(l.fill.B),
				A: uint8(255*inBg/(ss*ss) + 0),
			})
		}
	}
	return img
}

func encodePNG(img image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// encodeICO builds a .ico with classic 32-bit DIB entries, which every
// Windows version loads (PNG-compressed entries are not universal).
func encodeICO(sizes ...int) func(look) []byte {
	return func(l look) []byte {
		var imgs [][]byte
		for _, s := range sizes {
			imgs = append(imgs, dib(render(s, l)))
		}
		var b bytes.Buffer
		le := binary.LittleEndian
		_ = binary.Write(&b, le, [3]uint16{0, 1, uint16(len(sizes))})
		offset := 6 + 16*len(sizes)
		for i, s := range sizes {
			_ = binary.Write(&b, le, struct {
				W, H, Colors, Reserved uint8
				Planes, BitCount       uint16
				Size, Offset           uint32
			}{uint8(s), uint8(s), 0, 0, 1, 32, uint32(len(imgs[i])), uint32(offset)})
			offset += len(imgs[i])
		}
		for _, d := range imgs {
			b.Write(d)
		}
		return b.Bytes()
	}
}

func dib(img *image.NRGBA) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	maskRow := ((w + 31) / 32) * 4
	var b bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&b, le, struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, ImageSize uint32
		XPPM, YPPM             int32
		ClrUsed, ClrImportant  uint32
	}{40, int32(w), int32(2 * h), 1, 32, 0, uint32(w*h*4 + maskRow*h), 0, 0, 0, 0})
	for y := h - 1; y >= 0; y-- { // bottom-up rows, BGRA
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	b.Write(make([]byte, maskRow*h)) // AND mask unused: alpha channel wins
	return b.Bytes()
}

// iconBytes returns the icon for k in the format this OS's tray expects.
func iconBytes(k agent.Kind) []byte {
	l := lookFor(k)
	if runtime.GOOS == "windows" {
		return encodeICO(16, 24, 32, 48)(l)
	}
	return encodePNG(render(64, l))
}
