//go:build !windows

package launcher

import (
	"os"
	"os/exec"
)

// preparePipeConfig prepares Unix-specific pipe configuration.
// Returns a closure to configure the command with ExtraFiles for FD 3 and 4.
func (l *Launcher) preparePipeConfig(readPipe, writePipe *os.File) (func(cmd *exec.Cmd), error) {
	return func(cmd *exec.Cmd) {
		// Pass pipes to Chrome as extra file descriptors (become FD 3 and 4)
		cmd.ExtraFiles = []*os.File{readPipe, writePipe}
	}, nil
}
