//go:build !windows

package app

import "errors"

func desktopSupported() bool                      { return false }
func setLogin(enabled bool, o Options) error      { return errors.New("Windows startup is unavailable") }
func startDesktop(s *Server) (func(), error)      { return func() {}, nil }
func showStartupError(err error, background bool) {}
