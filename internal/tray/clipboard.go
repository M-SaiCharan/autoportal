//go:build !windows

package tray

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// copyText puts text on the clipboard with the system's own tool. On
// Linux that tool may be missing; the caller also saves the text to a file.
func copyText(text string) error {
	var cmds [][]string
	if runtime.GOOS == "darwin" {
		cmds = [][]string{{"pbcopy"}}
	} else {
		cmds = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	}
	for _, c := range cmds {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return errors.New("no clipboard tool found")
}
