//go:build !linux

package main

import "fmt"

type DiskStats struct {
	TotalBytes  uint64
	UsedBytes   uint64
	FreeBytes   uint64
	UsedPercent float64
	FreePercent float64
}

func getDiskStats(path string) (DiskStats, error) {
	return DiskStats{}, fmt.Errorf("disk stats are only supported on linux runtime")
}
