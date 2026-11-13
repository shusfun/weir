//go:build unix

package center

import "golang.org/x/sys/unix"

func storageProcessCPU() (float64, error) {
	var r unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &r); err != nil {
		return 0, err
	}
	return float64(r.Utime.Sec+r.Stime.Sec) + float64(r.Utime.Usec+r.Stime.Usec)/1e6, nil
}
