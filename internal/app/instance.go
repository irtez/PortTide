package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var errInstanceRunning = errors.New("PortTide already running")

type instanceInfo struct {
	Host  string `json:"host"`
	Token string `json:"token"`
}

func instanceCommand(dir, action string) error {
	deadline := time.Now().Add(8 * time.Second)
	for {
		b, err := os.ReadFile(filepath.Join(dir, "instance.json"))
		var info instanceInfo
		if err == nil && json.Unmarshal(b, &info) == nil {
			host, _, err := net.SplitHostPort(info.Host)
			if err != nil || host != "127.0.0.1" || len(info.Token) != 48 {
				return errors.New("invalid local instance record")
			}
			req, _ := http.NewRequest("POST", "http://"+info.Host+"/api/instance/"+action, bytes.NewBufferString("{}"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-PortTide-Token", info.Token)
			client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := client.Do(req)
			if err == nil {
				io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
				client.CloseIdleConnections()
				if resp.StatusCode == 200 {
					return nil
				}
				return fmt.Errorf("instance command rejected (%d)", resp.StatusCode)
			}
			client.CloseIdleConnections()
		}
		if time.Now().After(deadline) {
			return errors.New("не удалось связаться с работающим PortTide")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func (s *Server) instanceAction(w http.ResponseWriter, r *http.Request) {
	switch r.PathValue("action") {
	case "open", "settings":
		section := ""
		if r.PathValue("action") == "settings" {
			section = "#settings"
		}
		if s.openPanel != nil {
			if err := s.openPanel(section); err != nil {
				fail(w, 500, err)
				return
			}
		}
	case "prepare-update":
		s.mu.Lock()
		running, _ := s.runtime.Status()
		err := prepareUpdate(s.store, running)
		s.mu.Unlock()
		if err != nil {
			fail(w, 500, err)
			return
		}
		s.cancel()
	case "shutdown":
		s.cancel()
	default:
		fail(w, 404, errors.New("unknown instance command"))
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}

func prepareUpdate(store *Store, running bool) error {
	b, err := os.ReadFile(filepath.Join(store.Dir, "settings.json"))
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(store.Dir, "settings.before-update.json"), b); err != nil {
		return err
	}
	return saveResume(store, running)
}
func runCommand(o Options) error {
	switch o.Command {
	case "open", "settings":
		return instanceCommand(o.DataDir, o.Command)
	case "prepare-update", "shutdown", "enable-login", "disable-login", "reconcile-login":
	default:
		return errors.New("unknown command")
	}
	if err := os.MkdirAll(o.DataDir, 0700); err != nil {
		return err
	}
	unlock, err := lockInstance(o.DataDir)
	if errors.Is(err, errInstanceRunning) {
		if o.Command != "prepare-update" && o.Command != "shutdown" {
			return errors.New("close PortTide before changing installer settings")
		}
		if err = instanceCommand(o.DataDir, o.Command); err != nil {
			return err
		}
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			unlock, err = lockInstance(o.DataDir)
			if err == nil {
				unlock()
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return errors.New("PortTide did not finish shutting down")
	}
	if err != nil {
		return err
	}
	defer unlock()
	if o.Command == "shutdown" {
		return nil
	}
	if o.Command == "prepare-update" {
		if c, err := net.DialTimeout("tcp", o.Listen, 300*time.Millisecond); err == nil {
			c.Close()
			return errors.New("порт панели занят другим или старым экземпляром; закройте его перед установкой")
		}
	}
	if o.Command == "prepare-update" {
		// Do not migrate settings with the new helper before installation succeeds.
		b, err := os.ReadFile(filepath.Join(o.DataDir, "settings.json"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			if err = atomicWrite(filepath.Join(o.DataDir, "settings.before-update.json"), b); err != nil {
				return err
			}
		}
		return writeJSON(filepath.Join(o.DataDir, "update-resume.json"), resumeState{false, time.Now()})
	}
	store, err := OpenStore(o.DataDir)
	if err != nil {
		return err
	}
	enabled := store.Settings.Desktop.StartAtLogin
	if o.Command == "enable-login" {
		enabled = true
	}
	if o.Command == "disable-login" {
		enabled = false
	}
	if err = setLogin(enabled, o); err != nil {
		return err
	}
	store.Settings.Desktop.StartAtLogin = enabled
	return store.SaveSettings()
}
