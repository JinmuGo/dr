//go:build !darwin && !linux

package agent

import (
	"os"
	"time"
)

func logChangeTime(os.FileInfo) time.Time {
	return time.Time{}
}
