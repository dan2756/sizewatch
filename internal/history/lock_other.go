//go:build !linux && !darwin

package history

import "os"

// lockFile is a no-op where flock isn't available; concurrent writers may
// then lose a sample during compaction.
func lockFile(*os.File) error { return nil }
