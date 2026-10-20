package buildinfo

import "fmt"

var Version = "development"
var Revision = "unrecorded"

func ControlVersionLine() string {
	return fmt.Sprintf("MiGate control %s (commit: %s)", Version, Revision)
}

func GateVersionLine() string {
	return fmt.Sprintf("MiGate gate %s (commit: %s)", Version, Revision)
}
