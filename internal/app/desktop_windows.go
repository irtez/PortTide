//go:build windows

package app

import (
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

//go:embed tray.ico
var trayIcon []byte
var user32 = syscall.NewLazyDLL("user32.dll")
var shell32 = syscall.NewLazyDLL("shell32.dll")

func utf(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func wide(dst []uint16, s string) {
	v, _ := syscall.UTF16FromString(s)
	copy(dst, v)
	dst[len(dst)-1] = 0
}

type notifyData struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	Title               [64]uint16
	InfoFlags           uint32
	GUID                [16]byte
	BalloonIcon         uintptr
}
type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type winPoint struct{ X, Y int32 }
type winMessage struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          winPoint
	Private        uint32
}

func showStartupError(err error, background bool) {
	show := func() {
		user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(utf(err.Error()))), uintptr(unsafe.Pointer(utf("PortTide"))), 0x10)
	}
	if !background {
		show()
		return
	}
	// A failed logon launch remains discoverable without opening a browser/window.
	s := newServer(context.Background(), &Store{Settings: Defaults(), Catalogs: map[string]Catalog{}}, "")
	s.notice = err.Error()
	s.openPanel = func(string) error { show(); return nil }
	closeTray, trayErr := startDesktop(s)
	if trayErr != nil {
		return
	}
	<-s.ctx.Done()
	closeTray()
}

func startDesktop(s *Server) (func(), error) {
	ready := make(chan error, 1)
	done := make(chan struct{})
	var window uintptr
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		taskbar, _, _ := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(utf("TaskbarCreated"))))
		iconOffset := binary.LittleEndian.Uint32(trayIcon[18:22])
		icon, _, _ := user32.NewProc("CreateIconFromResourceEx").Call(uintptr(unsafe.Pointer(&trayIcon[iconOffset])), uintptr(len(trayIcon)-int(iconOffset)), 1, 0x30000, 32, 32, 0)
		if icon == 0 {
			ready <- errors.New("cannot create tray icon")
			return
		}
		defer user32.NewProc("DestroyIcon").Call(icon)
		data := notifyData{ID: 1, Flags: 7, Callback: 0x8001, Icon: icon}
		data.Size = uint32(unsafe.Sizeof(data))
		wide(data.Tip[:], "PortTide")
		add := func() { shell32.NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(&data))) }
		status := "Прокси остановлен"
		running := false
		lastNotice := ""
		lastUpdate := ""
		open := func(section string) { go func() { _ = s.openPanel(section) }() }
		callback := syscall.NewCallback(func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
			switch msg {
			case 0x8001:
				event := uint32(lparam) & 0xffff
				if event == 0x202 || event == 0x400 || event == 0x405 {
					open("")
				}
				if event == 0x205 || event == 0x7b {
					menu, _, _ := user32.NewProc("CreatePopupMenu").Call()
					defer user32.NewProc("DestroyMenu").Call(menu)
					appendItem := func(flags, id uintptr, label string) {
						user32.NewProc("AppendMenuW").Call(menu, flags, id, uintptr(unsafe.Pointer(utf(label))))
					}
					appendItem(0, 1, "Открыть PortTide")
					appendItem(1, 0, status)
					appendItem(0x800, 0, "")
					label := "Запустить прокси"
					if running {
						label = "Остановить прокси"
					}
					appendItem(0, 2, label)
					appendItem(0, 3, "Настройки")
					appendItem(0x800, 0, "")
					appendItem(0, 4, "Завершить PortTide")
					var pt winPoint
					user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&pt)))
					user32.NewProc("SetForegroundWindow").Call(hwnd)
					id, _, _ := user32.NewProc("TrackPopupMenu").Call(menu, 0x100|2, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
					user32.NewProc("PostMessageW").Call(hwnd, 0, 0, 0)
					switch id {
					case 1:
						open("")
					case 3:
						open("#settings")
					case 4:
						s.cancel()
					case 2:
						go func() {
							s.mu.Lock()
							defer s.mu.Unlock()
							r, _ := s.runtime.Status()
							if r {
								s.runtime.Stop()
							} else {
								_ = s.startSelected(s.ctx)
							}
						}()
					}
				}
			case 0x113: // WM_TIMER: keep shell calls on this thread
				if s.mu.TryLock() {
					var active string
					running, active = s.runtime.Status()
					status = "Прокси остановлен"
					if running {
						if p, ok := s.store.Profile(active); ok {
							status = "Прокси запущен: " + p.Name
						}
					}
					notice := s.notice
					if !running && active != "" && notice == "" {
						notice = "Ядро остановилось. Откройте PortTide для повторного запуска."
					}
					update := s.update.Version
					if s.update.Available && update != lastUpdate {
						lastUpdate = update
						if notice == "" {
							notice = "Доступна версия " + update + ". Обновление — в настройках PortTide."
						}
					}
					s.mu.Unlock()
					data.Flags = 4
					data.Tip = [128]uint16{}
					wide(data.Tip[:], "PortTide — "+status)
					if notice != "" && notice != lastNotice {
						lastNotice = notice
						data.Flags |= 0x10 | 0x40
						wide(data.Title[:], "PortTide")
						data.Info = [256]uint16{}
						wide(data.Info[:], notice)
						data.InfoFlags = 1 | 0x10 | 0x80
					}
					shell32.NewProc("Shell_NotifyIconW").Call(1, uintptr(unsafe.Pointer(&data)))
					data.Flags = 7
				}
			case 0x10:
				user32.NewProc("DestroyWindow").Call(hwnd)
				return 0
			case 0x2:
				shell32.NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(&data)))
				user32.NewProc("PostQuitMessage").Call(0)
				return 0
			}
			if msg == uint32(taskbar) {
				add()
				return 0
			}
			r, _, _ := user32.NewProc("DefWindowProcW").Call(hwnd, uintptr(msg), wparam, lparam)
			return r
		})
		inst, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
		cls := windowClass{Proc: callback, Instance: inst, Name: utf("PortTide-" + s.token[:12]), Icon: icon}
		cls.Size = uint32(unsafe.Sizeof(cls))
		atom, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&cls)))
		if atom == 0 {
			ready <- err
			return
		}
		defer user32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(cls.Name)), inst)
		window, _, err = user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(cls.Name)), uintptr(unsafe.Pointer(utf("PortTide"))), 0, 0, 0, 0, 0, 0, 0, inst, 0)
		if window == 0 {
			ready <- err
			return
		}
		data.Window = window
		add()
		user32.NewProc("SetTimer").Call(window, 1, 1000, 0)
		ready <- nil
		var msg winMessage
		for {
			r, _, _ := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
			user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
		}
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return func() { user32.NewProc("PostMessageW").Call(window, 0x10, 0, 0); <-done }, nil
}
