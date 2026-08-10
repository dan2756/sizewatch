//go:build linux || darwin

package walk

import (
	"io/fs"
	"syscall"
)

// statOf extracts device, inode, link count and allocated blocks.
// The field types differ between Linux and macOS, hence the conversions.
func statOf(fi fs.FileInfo) (stat, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return stat{}, false
	}
	return stat{
		dev:    uint64(st.Dev),
		ino:    uint64(st.Ino),
		nlink:  uint64(st.Nlink),
		blocks: int64(st.Blocks),
	}, true
}
