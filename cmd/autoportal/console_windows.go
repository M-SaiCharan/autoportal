//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// The release build for Windows is a GUI program (no console window flashes
// at login). When it is started from a terminal with a command, attach to
// that terminal so output is visible. For interactive use, the
// autoportal-cli.exe build (a normal console program) works better.
func init() {
	if len(os.Args) < 2 || os.Args[1] == "tray" {
		return
	}
	const attachParentProcess = ^uint32(0)
	attach := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	if r, _, _ := attach.Call(uintptr(attachParentProcess)); r == 0 {
		return // already a console program, or no parent console
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = f
	}
}
