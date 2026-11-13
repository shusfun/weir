//go:build !unix && !windows

package center

import "errors"

func storageProcessCPU() (float64, error) {
	return 0, errors.New("process CPU measurements unavailable on this platform")
}
