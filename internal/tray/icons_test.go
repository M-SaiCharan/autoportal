package tray

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/M-SaiCharan/autoportal/internal/agent"
)

func TestPNGIcons(t *testing.T) {
	for k := agent.NeedsSetup; k <= agent.Paused; k++ {
		b := encodePNG(render(64, lookFor(k)))
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("%v: %v", k, err)
		}
		if img.Bounds().Dx() != 64 {
			t.Fatalf("%v: size %v", k, img.Bounds())
		}
		// Corners transparent, centre opaque.
		if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
			t.Errorf("%v: corner not transparent", k)
		}
		if _, _, _, a := img.At(32, 20).RGBA(); a != 0xffff {
			t.Errorf("%v: body not opaque", k)
		}
	}
	if dir := os.Getenv("AUTOPORTAL_ICON_DUMP"); dir != "" { // for eyeballing
		for k := agent.NeedsSetup; k <= agent.Paused; k++ {
			os.WriteFile(filepath.Join(dir, k.String()+".png"), encodePNG(render(64, lookFor(k))), 0o644)
		}
	}
}

func TestICOStructure(t *testing.T) {
	sizes := []int{16, 32}
	b := encodeICO(sizes...)(lookFor(agent.SignedIn))
	le := binary.LittleEndian
	if le.Uint16(b[0:]) != 0 || le.Uint16(b[2:]) != 1 || le.Uint16(b[4:]) != 2 {
		t.Fatalf("bad ICO header % x", b[:6])
	}
	for i, s := range sizes {
		e := b[6+16*i:]
		size, off := le.Uint32(e[8:]), le.Uint32(e[12:])
		if int(e[0]) != s || int(off+size) > len(b) {
			t.Fatalf("entry %d: w=%d off=%d size=%d len=%d", i, e[0], off, size, len(b))
		}
		hdr := b[off:]
		if le.Uint32(hdr) != 40 || int32(le.Uint32(hdr[8:])) != int32(2*s) {
			t.Fatalf("entry %d: bad BITMAPINFOHEADER", i)
		}
		want := 40 + s*s*4 + ((s+31)/32)*4*s
		if int(size) != want {
			t.Fatalf("entry %d: size %d, want %d", i, size, want)
		}
	}
}
