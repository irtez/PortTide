package app

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstanceLockOwnership(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockInstance(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lockInstance(dir)
	if !errors.Is(err, errInstanceRunning) {
		unlock()
		t.Fatal("second owner accepted", err)
	}
	unlock()
	unlock, err = lockInstance(dir)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}
func TestAuthenticatedInstanceHandoff(t *testing.T) {
	s := testServer(t)
	opened := make(chan string, 1)
	s.openPanel = func(section string) error { opened <- section; return nil }
	srv := httptest.NewServer(s.handler())
	defer srv.Close()
	s.host = strings.TrimPrefix(srv.URL, "http://")
	writeJSON(filepath.Join(s.store.Dir, "instance.json"), instanceInfo{Host: s.host, Token: s.token})
	if err := instanceCommand(s.store.Dir, "settings"); err != nil {
		t.Fatal(err)
	}
	if <-opened != "#settings" {
		t.Fatal("wrong destination")
	}
	writeJSON(filepath.Join(s.store.Dir, "instance.json"), instanceInfo{Host: s.host, Token: strings.Repeat("0", 48)})
	if err := instanceCommand(s.store.Dir, "open"); err == nil {
		t.Fatal("accepted incorrect token")
	}
}
func TestInstanceRejectsNonLocalRecord(t *testing.T) {
	dir := t.TempDir()
	writeJSON(filepath.Join(dir, "instance.json"), instanceInfo{Host: "example.org:80", Token: strings.Repeat("a", 48)})
	if err := instanceCommand(dir, "open"); err == nil {
		t.Fatal("followed nonlocal record")
	}
}

func TestPrepareUpdateDoesNotMigrateStoppedLegacySettings(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte(`{"schema":1,"autoStart":false}`)
	os.WriteFile(filepath.Join(dir, "settings.json"), legacy, 0600)
	if err := runCommand(Options{DataDir: dir, Listen: "127.0.0.1:0", Command: "prepare-update"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	backup, _ := os.ReadFile(filepath.Join(dir, "settings.before-update.json"))
	if string(after) != string(legacy) || string(backup) != string(legacy) {
		t.Fatal("installer helper changed settings before successful install")
	}
}
