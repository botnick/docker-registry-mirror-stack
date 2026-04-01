//go:build linux

package main

import "golang.org/x/sys/unix"

type DiskStats struct {
	TotalBytes  uint64
	UsedBytes   uint64
	FreeBytes   uint64
	UsedPercent float64
	FreePercent float64
}

func getDiskStats(path string) (DiskStats, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return DiskStats{}, err
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free
	if total == 0 {
		return DiskStats{}, nil
	}
	return DiskStats{
		TotalBytes:  total,
		UsedBytes:   used,
		FreeBytes:   free,
		UsedPercent: (float64(used) / float64(total)) * 100,
		FreePercent: (float64(free) / float64(total)) * 100,
	}, nil
}
