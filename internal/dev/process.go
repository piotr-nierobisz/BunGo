package dev

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/piotr-nierobisz/BunGo/internal/devreload"
)

// stopGracePeriod is how long Stop waits for the app to exit after asking it
// to stop before force-killing its process tree.
const stopGracePeriod = 3 * time.Second

// goRunInterruptLine is what `go run` prints to stderr when its child is interrupted.
const goRunInterruptLine = "signal: interrupt"

type processRunner struct {
	projectRoot string
	runTarget   string
	stdout      io.Writer
	stderr      io.Writer

	mu       sync.Mutex
	cmd      *exec.Cmd
	waitDone chan error
}

// newProcessRunner creates a process runner configured for repeated `go run` restarts.
// Inputs:
// - projectRoot: working directory where `go run` will execute.
// - runTarget: target passed as `go run <target>`.
// - stdout: destination for child process stdout stream.
// - stderr: destination for child process stderr stream.
// Outputs:
// - *processRunner: initialized process runner with no active command.
func newProcessRunner(projectRoot, runTarget string, stdout, stderr io.Writer) *processRunner {
	return &processRunner{
		projectRoot: projectRoot,
		runTarget:   runTarget,
		stdout:      stdout,
		stderr:      stderr,
	}
}

// Restart stops any running process and starts a fresh process instance.
// Inputs:
// - none
// Outputs:
// - error: non-nil when the new process cannot be started.
func (r *processRunner) Restart() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopLocked()
	return r.startLocked()
}

// Stop terminates the currently running process tree if one exists.
// Inputs:
// - none
// Outputs:
// - none
func (r *processRunner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
}

// CheckExited reports whether the currently running process has exited.
// Inputs:
// - none
// Outputs:
// - bool: true when the tracked process has exited since the previous check.
// - error: process wait error returned when an exited process is observed.
func (r *processRunner) CheckExited() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.waitDone == nil {
		return false, nil
	}

	select {
	case err := <-r.waitDone:
		r.cmd = nil
		r.waitDone = nil
		return true, err
	default:
		return false, nil
	}
}

// startLocked starts a new child process and records its wait channel.
// Inputs:
// - none
// Outputs:
// - error: non-nil when process creation or start fails.
func (r *processRunner) startLocked() error {
	cmd := exec.Command("go", "run", r.runTarget)
	cmd.Dir = r.projectRoot
	cmd.Stdout = r.stdout
	stderr := newLineSuppressingWriter(r.stderr, goRunInterruptLine)
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), devreload.EnvVar+"=1")
	configureProcessTree(cmd)

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		// Wait has finished copying output, so a final unterminated line can be flushed.
		if f, ok := stderr.(interface{ Flush() error }); ok {
			_ = f.Flush()
		}
		done <- err
	}()

	r.cmd = cmd
	r.waitDone = done
	return nil
}

// stopLocked asks the app's process tree to stop and force-kills it after stopGracePeriod.
// Inputs:
// - none
// Outputs:
// - none
func (r *processRunner) stopLocked() {
	if r.cmd == nil || r.waitDone == nil {
		return
	}

	proc := r.cmd.Process
	interruptProcessTree(proc)

	select {
	case <-r.waitDone:
	case <-time.After(stopGracePeriod):
		killProcessTree(proc)
		<-r.waitDone
	}

	r.cmd = nil
	r.waitDone = nil
}

type lineSuppressingWriter struct {
	dst          io.Writer
	suppressLine string

	mu      sync.Mutex
	pending []byte
}

// newLineSuppressingWriter wraps a writer and drops exact matching lines.
// Inputs:
// - dst: destination writer that receives non-suppressed lines.
// - suppressLine: exact line content that should be filtered out.
// Outputs:
// - io.Writer: suppressing writer wrapper, or dst when suppression is disabled.
func newLineSuppressingWriter(dst io.Writer, suppressLine string) io.Writer {
	if dst == nil || suppressLine == "" {
		return dst
	}
	return &lineSuppressingWriter{
		dst:          dst,
		suppressLine: suppressLine,
	}
}

// Write buffers bytes until newline boundaries and forwards only non-suppressed lines.
// Inputs:
// - p: byte slice chunk written by the process stderr/stdout pipeline.
// Outputs:
// - int: number of input bytes consumed from p.
// - error: non-nil when writing forwarded output to destination fails.
func (w *lineSuppressingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pending = append(w.pending, p...)
	for {
		idx := bytes.IndexByte(w.pending, '\n')
		if idx < 0 {
			break
		}

		line := w.pending[:idx]
		w.pending = w.pending[idx+1:]
		if string(line) == w.suppressLine {
			continue
		}
		if _, err := w.dst.Write(line); err != nil {
			return len(p), err
		}
		if _, err := w.dst.Write([]byte{'\n'}); err != nil {
			return len(p), err
		}
	}

	return len(p), nil
}

// Flush forwards a final line that was not newline-terminated, completing it
// with a newline unless it is the suppressed line.
// Inputs:
// - none
// Outputs:
// - error: non-nil when writing the flushed line to the destination fails.
func (w *lineSuppressingWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.pending) == 0 {
		return nil
	}
	line := w.pending
	w.pending = nil
	if string(line) == w.suppressLine {
		return nil
	}
	_, err := w.dst.Write(append(line, '\n'))
	return err
}
