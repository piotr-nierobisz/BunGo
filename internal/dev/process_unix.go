//go:build unix

package dev

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// configureProcessTree starts the app in its own process group so the whole
// `go run` tree (go itself plus the compiled binary) can be signalled at once.
// Inputs:
// - cmd: command to configure before Start.
// Outputs:
// - none
func configureProcessTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// interruptProcessTree sends SIGINT to the app's process group, falling back
// to the process itself when the group is gone.
// Inputs:
// - proc: started app process (the process group leader).
// Outputs:
// - none
func interruptProcessTree(proc *os.Process) {
	if err := signalProcessGroup(proc.Pid, syscall.SIGINT); err != nil {
		_ = proc.Signal(os.Interrupt)
	}
}

// killProcessTree force-kills the app's process group and the process itself.
// Inputs:
// - proc: started app process (the process group leader).
// Outputs:
// - none
func killProcessTree(proc *os.Process) {
	_ = signalProcessGroup(proc.Pid, syscall.SIGKILL)
	_ = proc.Kill()
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
