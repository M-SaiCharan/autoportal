package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestICOStructure(t *testing.T) {
	sizes := []int{16, 32, 256}
	b := AppICO()
	le := binary.LittleEndian
	if le.Uint16(b[0:]) != 0 || le.Uint16(b[2:]) != 1 || le.Uint16(b[4:]) != 6 {
		t.Fatalf("bad ICO header % x", b[:6])
	}
	entries := map[int]int{16: 0, 32: 2, 256: 5} // size → index in AppICO
	for _, s := range sizes {
		e := b[6+16*entries[s]:]
		size, off := le.Uint32(e[8:]), le.Uint32(e[12:])
		if w := int(e[0]); (s < 256 && w != s) || (s == 256 && w != 0) || int(off+size) > len(b) {
			t.Fatalf("%d px: w=%d off=%d size=%d len=%d", s, e[0], off, size, len(b))
		}
		data := b[off : off+size]
		if s == 256 {
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil || img.Bounds().Dx() != 256 {
				t.Fatalf("256 px entry is not a 256 px PNG: %v", err)
			}
			continue
		}
		if le.Uint32(data) != 40 || int32(le.Uint32(data[8:])) != int32(2*s) {
			t.Fatalf("%d px: bad BITMAPINFOHEADER", s)
		}
		if want := 40 + s*s*4 + ((s+31)/32)*4*s; int(size) != want {
			t.Fatalf("%d px: size %d, want %d", s, size, want)
		}
	}
}

func TestICNSStructure(t *testing.T) {
	b := ICNS(func(s int) *image.NRGBA { return image.NewNRGBA(image.Rect(0, 0, s, s)) })
	be := binary.BigEndian
	if string(b[:4]) != "icns" || int(be.Uint32(b[4:])) != len(b) {
		t.Fatalf("bad header")
	}
	n := 0
	for off := 8; off < len(b); n++ {
		l := int(be.Uint32(b[off+4:]))
		img, err := png.Decode(bytes.NewReader(b[off+8 : off+l]))
		if err != nil {
			t.Fatalf("%s: %v", b[off:off+4], err)
		}
		if img.Bounds().Dx() != icnsTypes[n].size {
			t.Fatalf("%s: size %d", b[off:off+4], img.Bounds().Dx())
		}
		off += l
	}
	if n != len(icnsTypes) {
		t.Fatalf("%d entries", n)
	}
}

func TestAppIcon(t *testing.T) {
	img := App(128)
	if _, _, _, a := img.At(2, 2).RGBA(); a != 0 {
		t.Error("corner not transparent")
	}
	if c := img.NRGBAAt(64, 20); c.A != 0xFF || c.B < 0xC0 {
		t.Errorf("top of the tile is not opaque blue: %v", c)
	}
	if dir := os.Getenv("AUTOPORTAL_ICON_DUMP"); dir != "" {
		os.WriteFile(filepath.Join(dir, "app.png"), PNG(App(512)), 0o644)
		os.WriteFile(filepath.Join(dir, "app-32.png"), PNG(App(32)), 0o644)
	}
}
