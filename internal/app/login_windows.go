//go:build windows

package app

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func desktopSupported() bool { return true }
func setLogin(enabled bool, o Options) error {
	name := "PortTide"
	if !sameDataDir(o.DataDir, DefaultDataDir()) {
		name = fmt.Sprintf("PortTide-%x", sha256.Sum256([]byte(o.DataDir)))[:21]
	}
	if !enabled {
		return setRunValue(name, "", false)
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	args := []string{exe, "--background", "--data-dir", o.DataDir, "--listen", o.Listen}
	command := ""
	for _, a := range args {
		if command != "" {
			command += " "
		}
		command += syscall.EscapeArg(a)
	}
	if len(command) > 260 {
		return errors.New("путь автозапуска слишком длинный; установите PortTide в обычную папку")
	}

	return setRunValue(name, command, enabled)
}
func setRunValue(name, command string, enabled bool) error {
	key, e := syscall.UTF16PtrFromString(runKey)
	if e != nil {
		return e
	}
	var h syscall.Handle
	adv := syscall.NewLazyDLL("advapi32.dll")
	r, _, _ := adv.NewProc("RegCreateKeyExW").Call(uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(key)), 0, 0, 0, 0x0003, 0, uintptr(unsafe.Pointer(&h)), 0)
	if r != 0 {
		return syscall.Errno(r)
	}
	defer syscall.RegCloseKey(h)
	n, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return e
	}
	if !enabled {
		r, _, _ = adv.NewProc("RegDeleteValueW").Call(uintptr(h), uintptr(unsafe.Pointer(n)))
		if r == 2 {
			return nil
		}
	} else {
		value, e := syscall.UTF16FromString(command)
		if e != nil {
			return e
		}
		r, _, _ = adv.NewProc("RegSetValueExW").Call(uintptr(h), uintptr(unsafe.Pointer(n)), 0, syscall.REG_SZ, uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)*2))
	}
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}
