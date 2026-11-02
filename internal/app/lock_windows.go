//go:build windows

package app

import (
	"errors"
	"path/filepath"
	"syscall"
)

func lockInstance(dir string) (func(), error) {
	p, e := syscall.UTF16PtrFromString(filepath.Join(dir, "instance.lock"))
	if e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if errors.Is(e, syscall.Errno(32)) || errors.Is(e, syscall.Errno(33)) {
		return nil, errInstanceRunning
	}
	if e != nil {
		return nil, e
	}
	return func() { syscall.CloseHandle(h) }, nil
}
