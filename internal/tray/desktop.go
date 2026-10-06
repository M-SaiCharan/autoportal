package tray

import (
	"os/exec"
	"runtime"

	"github.com/ncruces/zenity"
)

// Notify shows a desktop notification. Failures are ignored: a missing
// notification must never break logging in.
func Notify(title, text string) {
	if runtime.GOOS == "linux" && !zenity.IsAvailable() {
		_ = exec.Command("notify-send", "--app-name=autoportal", title, text).Run()
		return
	}
	_ = zenity.Notify(text, zenity.Title(title), zenity.InfoIcon)
}

// openFile opens path with the default application.
func openFile(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}
