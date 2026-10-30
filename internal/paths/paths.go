package paths

var (
	ConfigDir       = "/etc/weir"
	CoreConfigDir   = "/etc/weir/cores"
	CertDir         = "/etc/weir/certs"
	PanelConfig     = "/etc/weir/panel.json"
	XrayConfig      = "/etc/weir/cores/xray.json"
	SingboxConfig   = "/etc/weir/cores/sing-box.json"
	DataDir         = "/var/lib/weir"
	Database        = "/var/lib/weir/weir.db"
	BackupDir       = "/var/lib/weir/backups"
	VersionsFile    = "/var/lib/weir/versions.json"
	LogDir          = "/var/log/weir"
	RunDir          = "/run/weir"
	BinDir          = "/usr/local/bin"
	XrayBinary      = "/usr/local/bin/xray"
	SingboxBinary   = "/usr/local/bin/sing-box"
	WeirBinary      = "/usr/local/bin/weir"
	Installer       = "/usr/local/bin/weir-install"
	DefaultHTTPPort = 9999
)

const (
	PanelService   = "weir"
	XrayService    = "weir-xray"
	SingboxService = "weir-sing-box"

	PanelServiceUnit   = "weir.service"
	XrayServiceUnit    = "weir-xray.service"
	SingboxServiceUnit = "weir-sing-box.service"
)

var (
	InstallLock = "/run/weir/install.lock"
	ApplyLock   = "/run/weir/apply.lock"
	SyncLock    = "/run/weir/sync.lock"
	UpgradeLock = "/run/weir/upgrade.lock"
)
