//go:build windows

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/go-rod/rod/lib/launcher/flags"
)

// preparePipeConfig prepares Windows-specific pipe configuration.
// It sets the --remote-debugging-io-pipes flag and returns a closure to configure the command.
func (l *Launcher) preparePipeConfig(readPipe, writePipe *os.File) (func(cmd *exec.Cmd), error) {
	// Get the Windows handles
	readHandle := syscall.Handle(readPipe.Fd())
	writeHandle := syscall.Handle(writePipe.Fd())

	// Make handles inheritable
	if err := setHandleInheritable(readHandle); err != nil {
		return nil, fmt.Errorf("failed to make read handle inheritable: %w", err)
	}
	if err := setHandleInheritable(writeHandle); err != nil {
		return nil, fmt.Errorf("failed to make write handle inheritable: %w", err)
	}

	// Add the Windows-specific flag with handle values
	// Chrome expects: --remote-debugging-io-pipes=<read_handle>,<write_handle>
	l.Set(flags.RemoteDebuggingIoPipes, fmt.Sprintf("%d,%d", readHandle, writeHandle))

	// Return closure to configure the command with inheritable handles
	return func(cmd *exec.Cmd) {
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.AdditionalInheritedHandles = []syscall.Handle{
			readHandle,
			writeHandle,
		}
	}, nil
}

// setHandleInheritable marks a Windows handle as inheritable
func setHandleInheritable(handle syscall.Handle) error {
	return syscall.SetHandleInformation(handle, syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT)
}
