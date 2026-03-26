//go:build !windows

package cmd

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func notifyStop(sigCh chan<- os.Signal) {
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
}

func sendStop(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}
