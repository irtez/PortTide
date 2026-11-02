package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacySettingsMigrationPreservesSecrets(t *testing.T) {
	dir := t.TempDir()
	old := map[string]any{"schema": 1, "autoStart": false, "selected": "p", "corePath": "custom-core", "proxy": Defaults().Proxy, "sources": []Source{{ID: "s", Name: "private", URL: "https://example.org/secret"}}}
	b, _ := json.Marshal(old)
	os.WriteFile(filepath.Join(dir, "settings.json"), b, 0600)
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if store.Settings.Schema != 2 || !store.Settings.Desktop.Background || !store.Settings.Desktop.ConnectOnLaunch || store.Settings.Desktop.StartAtLogin {
		t.Fatal("incorrect launch defaults")
	}
	if store.Settings.Selected != "p" || store.Settings.CorePath != "custom-core" || store.Settings.Sources[0].URL != "https://example.org/secret" {
		t.Fatal("migration lost settings")
	}
	backup, _ := os.ReadFile(filepath.Join(dir, "settings.v1.backup.json"))
	if string(backup) != string(b) {
		t.Fatal("original settings not backed up")
	}
	store.Settings.Desktop.ConnectOnLaunch = false
	store.SaveSettings()
	again, err := OpenStore(dir)
	if err != nil || again.Settings.Desktop.ConnectOnLaunch {
		t.Fatal("explicit preference lost")
	}
}
func TestStartupProfileOrderAndFallback(t *testing.T) {
	s := &Store{Settings: Defaults(), Catalogs: map[string]Catalog{"b": {Profiles: []Profile{{ID: "b1", SourceID: "b", Supported: true}}}, "a": {Profiles: []Profile{{ID: "bad", SourceID: "a"}, {ID: "a1", SourceID: "a", Supported: true}, {ID: "a2", SourceID: "a", Supported: true}}}}}
	s.Settings.Sources = []Source{{ID: "a"}, {ID: "b"}}
	for _, tc := range []struct{ id, source, want string }{{"", "", "a1"}, {"a2", "a", "a2"}, {"gone", "b", "b1"}, {"gone", "deleted", "a1"}, {"bad", "a", "a1"}} {
		s.Settings.Selected = tc.id
		s.Settings.SelectedSource = tc.source
		for i := 0; i < 20; i++ {
			p, ok := s.StartupProfile()
			if !ok || p.ID != tc.want {
				t.Fatalf("%+v got %s", tc, p.ID)
			}
		}
	}
	s.Settings.Sources = nil
	if _, ok := s.StartupProfile(); ok {
		t.Fatal("used orphaned catalog")
	}
}
func TestStopDoesNotDisableNextLaunch(t *testing.T) {
	s := testServer(t)
	if w := call(s, "POST", "/api/stop", map[string]bool{}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	again, err := OpenStore(s.store.Dir)
	if err != nil || !again.Settings.Desktop.ConnectOnLaunch {
		t.Fatal("Stop disabled startup")
	}
}
func TestPanelLaunchPolicy(t *testing.T) {
	settings := Defaults()
	settings.Sources = []Source{{ID: "s"}}
	for _, tc := range []struct {
		o    Options
		want bool
	}{{Options{}, false}, {Options{Open: true}, true}, {Options{Background: true}, false}, {Options{Open: true, NoBrowser: true}, false}, {Options{Resume: true, Open: true}, false}} {
		if got := shouldOpenPanel(tc.o, settings); got != tc.want {
			t.Fatalf("%+v: %v", tc, got)
		}
	}
	settings.Sources = nil
	if !shouldOpenPanel(Options{}, settings) {
		t.Fatal("first interactive launch must open setup")
	}
	if shouldOpenPanel(Options{Background: true}, settings) {
		t.Fatal("empty background startup opened browser")
	}
	settings.Desktop.Background = false
	if !shouldOpenPanel(Options{Background: true}, settings) {
		t.Fatal("explicit foreground preference ignored")
	}
}
func TestUpdateResumeConsumedOnce(t *testing.T) {
	s := testServer(t).store
	if err := saveResume(s, false); err != nil {
		t.Fatal(err)
	}
	if consumeResume(s, true) {
		t.Fatal("update started paused proxy")
	}
	if !consumeResume(s, true) {
		t.Fatal("pause leaked into later normal launch")
	}
}
func TestDesktopSettingsDoesNotChangeActiveProxySettings(t *testing.T) {
	s := testServer(t)
	old := s.store.Settings.Proxy
	d := DesktopSettings{Background: true, ConnectOnLaunch: false}
	w := call(s, "POST", "/api/desktop", map[string]any{"desktop": d, "refreshMinutes": 90, "corePath": ""})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if s.store.Settings.Proxy != old || s.store.Settings.Desktop.ConnectOnLaunch {
		t.Fatal("preferences not independent")
	}
}
func TestDesktopLoginFailureDoesNotSavePreference(t *testing.T) {
	s := testServer(t)
	w := call(s, "POST", "/api/desktop", map[string]any{"desktop": DesktopSettings{StartAtLogin: true}, "refreshMinutes": 90})
	if w.Code != 400 || s.store.Settings.Desktop.StartAtLogin {
		t.Fatal("saved failed registration")
	}
}
