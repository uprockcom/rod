package launcher

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"

	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/defaults"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/utils"
)

// NewPipeMode returns a Launcher configured for pipe-based CDP communication.
// This mode provides automatic zombie process prevention since Chrome dies when the pipe closes.
// It removes RemoteDebuggingPort and Leakless flags, and sets RemoteDebuggingPipe.
func NewPipeMode() *Launcher {
	l := New()
	l.Delete(flags.RemoteDebuggingPort)
	l.Delete(flags.Leakless)
	l.Set(flags.RemoteDebuggingPipe)
	// Mark parser as done so it discards all input (not needed in pipe mode)
	l.parser.lock.Lock()
	l.parser.done = true
	l.parser.lock.Unlock()
	return l
}

// LaunchPipe launches browser with --remote-debugging-pipe and returns a CDP client.
// Use NewPipeMode() to create a properly configured Launcher for this method.
func (l *Launcher) LaunchPipe() (*cdp.Client, error) {
	if l.hasLaunched() {
		return nil, ErrAlreadyLaunched
	}

	defer l.ctxCancel()

	bin, err := l.getBin()
	if err != nil {
		return nil, err
	}

	l.setupUserPreferences()

	// Pipe for us to write, Chrome to read
	chromeReadPipe, ourWritePipe, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create write pipe: %w", err)
	}

	// Pipe for Chrome to write, us to read
	ourReadPipe, chromeWritePipe, err := os.Pipe()
	if err != nil {
		chromeReadPipe.Close()
		ourWritePipe.Close()
		return nil, fmt.Errorf("failed to create read pipe: %w", err)
	}

	// Prepare platform-specific pipe config (may modify launcher flags on Windows)
	// Returns a closure to configure the command
	configureCmd, err := l.preparePipeConfig(chromeReadPipe, chromeWritePipe)
	if err != nil {
		chromeReadPipe.Close()
		ourWritePipe.Close()
		ourReadPipe.Close()
		chromeWritePipe.Close()
		return nil, err
	}

	// Format args AFTER preparePipeConfig since Windows adds --remote-debugging-io-pipes
	args := l.FormatArgs()
	cmd := exec.Command(bin, args...)

	l.setupCmd(cmd)
	configureCmd(cmd)

	err = cmd.Start()
	if err != nil {
		chromeReadPipe.Close()
		ourWritePipe.Close()
		ourReadPipe.Close()
		chromeWritePipe.Close()
		return nil, err
	}

	// Close the Chrome-side of the pipes in parent process
	chromeReadPipe.Close()
	chromeWritePipe.Close()

	l.pid = cmd.Process.Pid

	go func() {
		_ = cmd.Wait()
		close(l.exit)
	}()

	// Create pipe-based WebSocket implementation
	pipeWS := NewPipeWebSocket(ourReadPipe, ourWritePipe)

	// Create and start CDP client
	client := cdp.New().Logger(defaults.CDP).Start(pipeWS)

	return client, nil
}

// MustLaunchPipe is similar to LaunchPipe.
func (l *Launcher) MustLaunchPipe() *cdp.Client {
	client, err := l.LaunchPipe()
	utils.E(err)
	return client
}

// PipeWebSocket implements cdp.WebSocketable using pipes.
// Messages are null-byte delimited per Chrome's pipe protocol.
type PipeWebSocket struct {
	in     *os.File
	out    *os.File
	reader *bufio.Reader
}

// NewPipeWebSocket creates a new PipeWebSocket from the given file descriptors.
// in is for reading CDP messages from Chrome, out is for sending to Chrome.
func NewPipeWebSocket(in, out *os.File) *PipeWebSocket {
	return &PipeWebSocket{
		in:     in,
		out:    out,
		reader: bufio.NewReader(in),
	}
}

// Send sends a CDP message to the browser.
func (p *PipeWebSocket) Send(data []byte) error {
	_, err := p.out.Write(append(data, '\x00'))
	if err != nil {
		_ = p.Close()
	}
	return err
}

// Read reads a CDP message from the browser.
func (p *PipeWebSocket) Read() ([]byte, error) {
	data, err := p.reader.ReadBytes('\x00')
	if err != nil {
		_ = p.Close()
		return nil, err
	}
	// Remove the trailing null byte
	if len(data) > 0 {
		data = data[:len(data)-1]
	}
	return data, nil
}

// Close closes both pipe file descriptors.
func (p *PipeWebSocket) Close() error {
	err1 := p.in.Close()
	err2 := p.out.Close()
	if err1 != nil {
		return err1
	}
	return err2
}
