//go:build windows

package tray

import (
	"bytes"
	"encoding/binary"
	"os/exec"
	"syscall"
	"unicode/utf16"
)

// copyText puts text on the clipboard with clip.exe, which reads UTF-16
// when the input starts with a byte-order mark. CREATE_NO_WINDOW keeps a
// console window from flashing up.
func copyText(text string) error {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xFE})
	_ = binary.Write(&b, binary.LittleEndian, utf16.Encode([]rune(text)))
	cmd := exec.Command("clip")
	cmd.Stdin = &b
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Run()
}
