package tray

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/M-SaiCharan/autoportal/internal/agent"
	"github.com/M-SaiCharan/autoportal/internal/icon"
)

func TestPNGIcons(t *testing.T) {
	for k := agent.NeedsSetup; k <= agent.Paused; k++ {
		b := icon.PNG(render(64, lookFor(k)))
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
			os.WriteFile(filepath.Join(dir, k.String()+".png"), icon.PNG(render(64, lookFor(k))), 0o644)
		}
	}
}
