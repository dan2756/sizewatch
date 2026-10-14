//go:build !linux && !darwin

package trigger

import "os/exec"

// killGroupOnCancel leaves cmd as it is: without process groups, only the
// shell itself is killed when the context ends.
func killGroupOnCancel(*exec.Cmd) {}
