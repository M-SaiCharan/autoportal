// Package icon draws autoportal's icons in code (no binary assets in the
// repository) and encodes them as PNG, ICO and ICNS.
package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Shape reports whether point (x, y) of the unit square is inside.
type Shape func(x, y float64) bool

// Disc is a filled circle.
func Disc(cx, cy, r float64) Shape {
	return func(x, y float64) bool { return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r }
}

// Stroke is a line segment with round caps and half-width w.
func Stroke(x1, y1, x2, y2, w float64) Shape {
	return func(x, y float64) bool {
		dx, dy := x2-x1, y2-y1
		t := ((x-x1)*dx + (y-y1)*dy) / (dx*dx + dy*dy)
		t = math.Max(0, math.Min(1, t))
		px, py := x1+t*dx-x, y1+t*dy-y
		return px*px+py*py <= w*w
	}
}

// Arc is a circular arc around (cx, cy) with radius r and half-width w,
// spanning half degrees either side of straight up, with round caps.
func Arc(cx, cy, r, w, half float64) Shape {
	a := half * math.Pi / 180
	ex, ey := r*math.Sin(a), r*math.Cos(a)
	caps := Union(Disc(cx-ex, cy-ey, w), Disc(cx+ex, cy-ey, w))
	return func(x, y float64) bool {
		dx, dy := x-cx, y-cy
		d := math.Hypot(dx, dy)
		if d >= r-w && d <= r+w && math.Abs(math.Atan2(dx, -dy)) <= a {
			return true
		}
		return caps(x, y)
	}
}

// RoundRect is a rectangle with corner radius r.
func RoundRect(x0, y0, x1, y1, r float64) Shape {
	return func(x, y float64) bool {
		if x < x0 || x > x1 || y < y0 || y > y1 {
			return false
		}
		cx := math.Max(x0+r, math.Min(x, x1-r))
		cy := math.Max(y0+r, math.Min(y, y1-r))
		return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r
	}
}

// Union is inside any of ss.
func Union(ss ...Shape) Shape {
	return func(x, y float64) bool {
		for _, s := range ss {
			if s(x, y) {
				return true
			}
		}
		return false
	}
}

// Paint gives the colour at a point.
type Paint func(x, y float64) color.NRGBA

// Solid paints one colour.
func Solid(c color.NRGBA) Paint { return func(_, _ float64) color.NRGBA { return c } }

// Vertical is a top-to-bottom gradient.
func Vertical(top, bottom color.NRGBA) Paint {
	return func(_, y float64) color.NRGBA {
		m := func(a, b uint8) uint8 { return uint8(float64(a)*(1-y) + float64(b)*y + 0.5) }
		return color.NRGBA{m(top.R, bottom.R), m(top.G, bottom.G), m(top.B, bottom.B), 0xFF}
	}
}

// Layer is a shape filled with a paint. Later layers cover earlier ones.
type Layer struct {
	Shape Shape
	Paint Paint
}

// Render draws layers at size×size pixels with 4×4 supersampling.
func Render(size int, layers ...Layer) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b float64
			n := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					for i := len(layers) - 1; i >= 0; i-- {
						if layers[i].Shape(x, y) {
							c := layers[i].Paint(x, y)
							r, g, b = r+float64(c.R), g+float64(c.G), b+float64(c.B)
							n++
							break
						}
					}
				}
			}
			if n == 0 {
				continue
			}
			f := float64(n)
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(r/f + 0.5), G: uint8(g/f + 0.5), B: uint8(b/f + 0.5),
				A: uint8(255 * n / (ss * ss)),
			})
		}
	}
	return img
}

// PNG encodes img.
func PNG(img image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// ICO builds a .ico from images of different sizes. Images up to 128 px
// are stored as classic 32-bit DIBs, which every Windows version loads;
// larger ones as PNG, as Windows expects for 256 px.
func ICO(imgs ...*image.NRGBA) []byte {
	var data [][]byte
	for _, img := range imgs {
		if img.Rect.Dx() > 128 {
			data = append(data, PNG(img))
		} else {
			data = append(data, dib(img))
		}
	}
	var b bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&b, le, [3]uint16{0, 1, uint16(len(imgs))})
	offset := 6 + 16*len(imgs)
	for i, img := range imgs {
		s := img.Rect.Dx()
		if s >= 256 {
			s = 0 // 0 means 256
		}
		_ = binary.Write(&b, le, struct {
			W, H, Colors, Reserved uint8
			Planes, BitCount       uint16
			Size, Offset           uint32
		}{uint8(s), uint8(s), 0, 0, 1, 32, uint32(len(data[i])), uint32(offset)})
		offset += len(data[i])
	}
	for _, d := range data {
		b.Write(d)
	}
	return b.Bytes()
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

// icnsTypes maps each ICNS PNG slot to its pixel size.
var icnsTypes = []struct {
	tag  string
	size int
}{
	{"icp4", 16}, {"icp5", 32}, {"ic11", 32}, {"ic12", 64},
	{"ic07", 128}, {"ic13", 256}, {"ic08", 256}, {"ic14", 512}, {"ic09", 512}, {"ic10", 1024},
}

// ICNS builds a macOS .icns file, drawing each size with draw.
func ICNS(draw func(size int) *image.NRGBA) []byte {
	var body bytes.Buffer
	be := binary.BigEndian
	cache := map[int][]byte{}
	for _, t := range icnsTypes {
		d, ok := cache[t.size]
		if !ok {
			d = PNG(draw(t.size))
			cache[t.size] = d
		}
		body.WriteString(t.tag)
		_ = binary.Write(&body, be, uint32(8+len(d)))
		body.Write(d)
	}
	var b bytes.Buffer
	b.WriteString("icns")
	_ = binary.Write(&b, be, uint32(8+body.Len()))
	b.Write(body.Bytes())
	return b.Bytes()
}
