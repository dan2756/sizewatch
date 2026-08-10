//go:build !linux && !darwin

package walk

import "io/fs"

// statOf has no portable inode or block information on this platform, so
// sizes fall back to apparent sizes and hard links are not deduplicated.
func statOf(fs.FileInfo) (stat, bool) { return stat{}, false }
