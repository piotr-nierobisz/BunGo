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

func newProcessRunner(projectRoot, runTarget string, stdout, stderr io.Writer) *processRunner {
	return &processRunner{
		projectRoot: projectRoot,
		runTarget:   runTarget,
		stdout:      stdout,
		stderr:      stderr,
	}
}

func (r *processRunner) Restart() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.stopLocked(); err != nil {
		return err
	}
	return r.startLocked()
}

func (r *processRunner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopLocked()
}

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

func newLineSuppressingWriter(dst io.Writer, suppressLine string) io.Writer {
	if dst == nil || suppressLine == "" {
		return dst
	}
	return &lineSuppressingWriter{
		dst:          dst,
		suppressLine: suppressLine,
	}
}

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
