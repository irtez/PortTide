//go:build windows

package app

import (
	"syscall"
	"testing"
	"unsafe"
)

func TestWindowsShellLayouts(t *testing.T) {
	if unsafe.Sizeof(notifyData{}) != 976 || unsafe.Sizeof(windowClass{}) != 80 || unsafe.Sizeof(winMessage{}) != 48 {
		t.Fatal("incorrect x64 Win32 layout")
	}
}
func TestRunKeyRoundTrip(t *testing.T) {
	name := "PortTide-Test-" + token()[:12]
	defer setRunValue(name, "", false)
	command := `"C:\Example With Spaces\PortTide.exe" --background`
	if err := setRunValue(name, command, true); err != nil {
		t.Fatal(err)
	}
	var h syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, utf(runKey), 0, syscall.KEY_QUERY_VALUE, &h); err != nil {
		t.Fatal(err)
	}
	defer syscall.RegCloseKey(h)
	b := make([]uint16, 512)
	size := uint32(len(b) * 2)
	var kind uint32
	if err := syscall.RegQueryValueEx(h, utf(name), nil, &kind, (*byte)(unsafe.Pointer(&b[0])), &size); err != nil {
		t.Fatal(err)
	}
	if syscall.UTF16ToString(b) != command {
		t.Fatal("quoted command changed")
	}
	if err := setRunValue(name, "", false); err != nil {
		t.Fatal(err)
	}
}
func TestNativeTrayLifecycle(t *testing.T) {
	s := testServer(t)
	s.openPanel = func(string) error { return nil }
	closeTray, err := startDesktop(s)
	if err != nil {
		t.Fatal(err)
	}
	closeTray()
}
