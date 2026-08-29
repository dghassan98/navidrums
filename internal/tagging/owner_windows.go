//go:build windows

package tagging

import "io/fs"

// fileOwner has no meaningful answer on Windows, where the container runtime
// synthesises ownership rather than storing it.
func fileOwner(fs.FileInfo) (uid, gid int, ok bool) { return 0, 0, false }
