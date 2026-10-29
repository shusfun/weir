package buildinfo

import "fmt"

var Version = "development"
var Revision = "unrecorded"

// Release 表示这是 CI 发布的正式版本（版本号符合发布格式），不是开发构建。
func Release() bool {
	_, ok := ParseScheme(Version)
	return ok
}

func ControlVersionLine() string {
	return fmt.Sprintf("Weir control %s (commit: %s)", Version, Revision)
}

func GateVersionLine() string {
	return fmt.Sprintf("Weir gate %s (commit: %s)", Version, Revision)
}
