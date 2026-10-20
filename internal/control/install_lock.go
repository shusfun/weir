package control

import "path/filepath"

func dirOf(path string) string {
	dir := filepath.Dir(path)
	if dir == "." {
		return ""
	}
	return dir
}
