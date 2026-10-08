package doctor

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func osName() string {
	v := windows.RtlGetVersion()
	name := "Windows 10"
	if v.MajorVersion == 10 && v.BuildNumber >= 22000 {
		name = "Windows 11"
	} else if v.MajorVersion < 10 {
		name = "Windows"
	}
	return fmt.Sprintf("%s (build %d)", name, v.BuildNumber)
}
