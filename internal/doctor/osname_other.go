//go:build !linux && !darwin && !windows

package doctor

import "runtime"

func osName() string { return runtime.GOOS }
