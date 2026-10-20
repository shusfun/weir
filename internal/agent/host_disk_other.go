//go:build !linux

package agent

func hostDisk(string) (*uint64, *uint64) { return nil, nil }
