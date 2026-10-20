package contracts

// Host observations are unprivileged, read-only samples. Nil fields mean
// unavailable; NIC totals cover the host and are never used as user quota.
type HostObservation struct {
	CPUPercent      *float64  `json:"cpu_percent"`
	CPUInterval     *float64  `json:"cpu_interval_seconds"`
	Source          string    `json:"source"`
	ObservedAt      string    `json:"observed_at"`
	Validity        string    `json:"validity"`
	LogicalCPUs     *int      `json:"logical_cpus"`
	Load            []float64 `json:"load"`
	UptimeSeconds   *float64  `json:"uptime_seconds"`
	MemoryTotal     *uint64   `json:"memory_total"`
	MemoryAvailable *uint64   `json:"memory_available"`
	DiskTotal       *uint64   `json:"disk_total"`
	DiskAvailable   *uint64   `json:"disk_available"`
	NetworkRX       *string   `json:"network_rx"`
	NetworkTX       *string   `json:"network_tx"`
}
