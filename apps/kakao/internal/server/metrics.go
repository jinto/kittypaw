package server

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

func getRSSBytes() uint64 {
	raw, err := os.ReadFile("/proc/self/statm")
	if err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) > 1 {
			pages, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil {
				return pages * uint64(os.Getpagesize())
			}
		}
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return mem.Sys
}

func getFDCount() uint64 {
	path := "/proc/self/fd"
	if _, err := os.Stat(path); err != nil {
		path = "/dev/fd"
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	return uint64(len(entries))
}
