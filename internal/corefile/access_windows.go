package corefile

import "os"

func access(path string, mode uint32) error {
	if mode == 2 {
		f, e := os.OpenFile(path, os.O_WRONLY, 0)
		if e != nil {
			return e
		}
		return f.Close()
	}
	_, e := os.Stat(path)
	return e
}
