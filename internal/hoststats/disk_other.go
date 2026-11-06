//go:build !linux

package hoststats

func diskUsage(string) (*uint64, *uint64) { return nil, nil }
