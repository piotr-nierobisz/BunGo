package dev

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

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
// - error: non-nil when stop or start sequence fails.
func (r *processRunner) Restart() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.stopLocked(); err != nil {
		return err
	}
	return r.startLocked()
}

// Stop terminates the currently running process if one exists.
// Inputs:
// - none
// Outputs:
// - error: non-nil when process termination fails.
func (r *processRunner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopLocked()
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
	cmd.Stderr = newLineSuppressingWriter(r.stderr, "signal: interrupt")
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=1", "BUNGO_DEV_ENABLED"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	r.cmd = cmd
	r.waitDone = done
	return nil
}

// stopLocked attempts graceful process-group stop and escalates to force kill on timeout.
// Inputs:
// - none
// Outputs:
// - error: non-nil when termination signaling fails in unrecoverable paths.
func (r *processRunner) stopLocked() error {
	if r.cmd == nil {
		return nil
	}

	done := r.waitDone
	if done == nil {
		return nil
	}

	pid := r.cmd.Process.Pid
	if err := signalProcessGroup(pid, syscall.SIGINT); err != nil {
		_ = r.cmd.Process.Signal(os.Interrupt)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = signalProcessGroup(pid, syscall.SIGKILL)
		_ = r.cmd.Process.Kill()
		<-done
	}

	r.cmd = nil
	r.waitDone = nil
	return nil
}

// signalProcessGroup sends a Unix signal to the process group identified by pid.
// Inputs:
// - pid: process identifier whose process group will receive the signal.
// - signal: OS signal delivered to the process group.
// Outputs:
// - error: non-nil when pid is invalid or signal delivery fails.
func signalProcessGroup(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process id: %d", pid)
	}
	// Negative pid targets the process group.
	return syscall.Kill(-pid, signal)
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
