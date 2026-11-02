//go:build !windows && !linux

package app

import "os/exec"

func prepareCommand(cmd *exec.Cmd)               {}
func guardProcess(cmd *exec.Cmd) (func(), error) { return func() {}, nil }
func openBrowser(url string) error               { return exec.Command("xdg-open", url).Start() }
