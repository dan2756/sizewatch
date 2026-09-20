//go:build linux || darwin

package history

import (
	"os"
	"syscall"
)

// lockFile takes an exclusive flock on f; closing f releases it.
func lockFile(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			return err
		}
	}
}
