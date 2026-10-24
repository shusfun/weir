package buildinfo

import "fmt"

var Version = "development"
var Revision = "unrecorded"

func ControlVersionLine() string {
	return fmt.Sprintf("Weir control %s (commit: %s)", Version, Revision)
}

func GateVersionLine() string {
	return fmt.Sprintf("Weir gate %s (commit: %s)", Version, Revision)
}
