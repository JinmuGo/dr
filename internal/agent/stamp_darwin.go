package agent

import (
	"os"
	"syscall"
	"time"
)

func logChangeTime(info os.FileInfo) time.Time {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(stat.Ctimespec.Sec, stat.Ctimespec.Nsec)
	}
	return time.Time{}
}
