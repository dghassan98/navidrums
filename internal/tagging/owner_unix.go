//go:build !windows

package tagging

import (
	"io/fs"
	"syscall"
)

// fileOwner reports the owning uid and gid where the platform exposes them.
func fileOwner(info fs.FileInfo) (uid, gid int, ok bool) {
	stat, valid := info.Sys().(*syscall.Stat_t)
	if !valid {
		return 0, 0, false
	}
	return int(stat.Uid), int(stat.Gid), true
}
