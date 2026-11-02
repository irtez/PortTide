package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func shouldOpenPanel(o Options, settings Settings) bool {
	if o.NoBrowser || o.Resume {
		return false
	}
	if o.Open {
		return true
	}
	if !o.Background && len(settings.Sources) == 0 {
		return true
	}
	return !settings.Desktop.Background
}

// Startup selection follows source order, never map order or provider names.
func (s *Store) StartupProfile() (Profile, bool) {
	exists := func(id string) bool {
		for _, src := range s.Settings.Sources {
			if src.ID == id {
				return true
			}
		}
		return false
	}
	if p, ok := s.Profile(s.Settings.Selected); ok && p.Supported && exists(p.SourceID) {
		return p, true
	}
	first := func(id string) (Profile, bool) {
		for _, p := range s.Catalogs[id].Profiles {
			if p.Supported {
				return p, true
			}
		}
		return Profile{}, false
	}
	if exists(s.Settings.SelectedSource) {
		if p, ok := first(s.Settings.SelectedSource); ok {
			return p, true
		}
	}
	for _, src := range s.Settings.Sources {
		if p, ok := first(src.ID); ok {
			return p, true
		}
	}
	return Profile{}, false
}

// Caller holds s.mu, or invokes before the server accepts requests.
func (s *Server) startSelected(ctx context.Context) error {
	p, ok := s.store.StartupProfile()
	if !ok {
		if len(s.store.Settings.Sources) > 0 {
			s.notice = "Нет поддерживаемого профиля. Проверьте подписки."
		}
		return nil
	}
	old := s.store.Settings
	if err := s.runtime.Apply(ctx, resolveCore(old.CorePath), p, old.Proxy); err != nil {
		s.notice = "Не удалось запустить прокси: " + err.Error()
		return err
	}
	s.store.Settings.Selected, s.store.Settings.SelectedSource = p.ID, p.SourceID
	if err := s.store.SaveSettings(); err != nil {
		s.store.Settings = old
		s.runtime.Stop()
		s.notice = "Не удалось сохранить выбранный профиль."
		return err
	}
	s.notice = ""
	if old.Selected != "" && old.Selected != p.ID {
		s.notice = "Прежний профиль недоступен. Выбран: " + p.Name
	}
	return nil
}
func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startSelected(r.Context()); err != nil {
		fail(w, 400, err)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (s *Server) desktopSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Desktop        DesktopSettings `json:"desktop"`
		CorePath       string          `json:"corePath"`
		RefreshMinutes int             `json:"refreshMinutes"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.RefreshMinutes < 5 || in.RefreshMinutes > 10080 {
		fail(w, 400, errors.New("интервал должен быть от 5 до 10080 минут"))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.store.Settings
	if in.Desktop.StartAtLogin != old.Desktop.StartAtLogin {
		if s.login == nil {
			fail(w, 400, errors.New("автозапуск доступен в Windows"))
			return
		}
		if err := s.login(in.Desktop.StartAtLogin); err != nil {
			fail(w, 400, err)
			return
		}
	}
	rollbackLogin := func() {
		if in.Desktop.StartAtLogin != old.Desktop.StartAtLogin {
			_ = s.login(old.Desktop.StartAtLogin)
		}
	}
	running, active := s.runtime.Status()
	p, ok := s.store.Profile(active)
	if running && ok && in.CorePath != old.CorePath {
		if err := s.runtime.Apply(r.Context(), resolveCore(in.CorePath), p, old.Proxy); err != nil {
			rollbackLogin()
			fail(w, 400, err)
			return
		}
	}
	s.store.Settings.Desktop, s.store.Settings.CorePath, s.store.Settings.RefreshMinutes = in.Desktop, in.CorePath, in.RefreshMinutes
	if err := s.store.SaveSettings(); err != nil {
		s.store.Settings = old
		rollbackLogin()
		if running && ok && in.CorePath != old.CorePath {
			_ = s.runtime.Apply(s.ctx, resolveCore(old.CorePath), p, old.Proxy)
		}
		fail(w, 500, err)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}

type resumeState struct {
	Running bool      `json:"running"`
	Created time.Time `json:"created"`
}

func saveResume(store *Store, running bool) error {
	return writeJSON(filepath.Join(store.Dir, "update-resume.json"), resumeState{running, time.Now()})
}
func consumeResume(store *Store, fallback bool) bool {
	path := filepath.Join(store.Dir, "update-resume.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	_ = os.Remove(path)
	var v resumeState
	if json.Unmarshal(b, &v) != nil || time.Since(v.Created) > 24*time.Hour || time.Since(v.Created) < 0 {
		return fallback
	}
	return v.Running
}

func StartupError(err error, background bool) { showStartupError(err, background) }
