//go:build linux

package app

import (
	"os/exec"
	"syscall"
)

func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
func guardProcess(cmd *exec.Cmd) (func(), error) { return func() {}, nil }
func openBrowser(url string) error               { return exec.Command("xdg-open", url).Start() }
