//go:build windows

package cmd

import (
	"os"
	"os/exec"
	"os/signal"
)

func setDetached(cmd *exec.Cmd) {}

func notifyStop(sigCh chan<- os.Signal) {
	signal.Notify(sigCh, os.Interrupt)
}

func sendStop(proc *os.Process) error {
	return proc.Signal(os.Interrupt)
}
