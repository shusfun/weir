package center

import (
	"golang.org/x/sys/windows"
)

func storageProcessCPU() (float64, error) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user); err != nil {
		return 0, err
	}
	ticks := func(t windows.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }
	return float64(ticks(kernel)+ticks(user)) / 1e7, nil
}
