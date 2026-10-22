//go:build windows

package center

import "os"

func init() {
	ownerOf = func(os.FileInfo) (int, int, bool) { return 0, 0, false }
	chownFile = func(string, int, int) error { return nil }
}

func defaultIsRoot() bool { return false }
