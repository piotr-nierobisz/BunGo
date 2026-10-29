//go:build windows

package dev

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// configureProcessTree starts the app in a new process group so a Ctrl+C in
// the bungo console is not delivered to it directly; the runner stops it.
// Inputs:
// - cmd: command to configure before Start.
// Outputs:
// - none
func configureProcessTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// interruptProcessTree stops the app. Windows has no SIGINT equivalent that
// can be sent to a child console process group without extra Win32 calls, so
// the tree is terminated right away.
// Inputs:
// - proc: started app process (`go run`).
// Outputs:
// - none
func interruptProcessTree(proc *os.Process) {
	killProcessTree(proc)
}

// killProcessTree terminates the app and everything it started. `go run`
// launches the compiled binary as a child process, so killing only go.exe
// would leave the app running; taskkill /T walks the whole tree.
// Inputs:
// - proc: started app process (`go run`).
// Outputs:
// - none
func killProcessTree(proc *os.Process) {
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(proc.Pid))
	if err := kill.Run(); err != nil {
		_ = proc.Kill()
	}
}
