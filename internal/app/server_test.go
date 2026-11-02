package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	store, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s := newServer(context.Background(), store, "127.0.0.1:17790")
	t.Cleanup(func() { s.cancel(); s.jobs.Wait(); s.runtime.Stop() })
	return s
}
func call(s *Server, method, path string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "http://"+s.host+path, bytes.NewReader(b))
	r.Header.Set("X-PortTide-Token", s.token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	return w
}
func TestAPIBoundary(t *testing.T) {
	s := testServer(t)
	for _, tc := range []struct {
		host, origin, token string
		status              int
	}{{"attacker.invalid", "", s.token, 403}, {s.host, "https://attacker.invalid", s.token, 403}, {s.host, "", "", 401}, {s.host, "http://" + s.host, s.token, 200}} {
		r := httptest.NewRequest("GET", "http://"+tc.host+"/api/state", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-PortTide-Token", tc.token)
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d", w.Code, tc.status)
		}
	}
}
func TestImportFailureKeepsCacheAndSettings(t *testing.T) {
	s := testServer(t)
	w := call(s, "POST", "/api/sources", map[string]string{"name": "Original", "content": fixture})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	id := s.store.Settings.Sources[0].ID
	before := string(s.store.Catalogs[id].Profiles[0].Config)
	w = call(s, "POST", "/api/sources", map[string]string{"id": id, "name": "Changed", "content": "encrypted-not-supported"})
	if w.Code != 400 || s.store.Settings.Sources[0].Name != "Original" || string(s.store.Catalogs[id].Profiles[0].Config) != before {
		t.Fatal("failed update changed existing data")
	}
	reloaded, e := OpenStore(s.store.Dir)
	if e != nil || reloaded.Settings.Sources[0].Name != "Original" {
		t.Fatal("failed update changed disk state")
	}
}
func TestStateOmitsSubscriptionCredentialsAndRawConfigs(t *testing.T) {
	s := testServer(t)
	ps, _ := ParseProfiles("s", []byte(fixture))
	s.store.Settings.Sources = []Source{{ID: "s", Name: "test", URL: "https://example.org/private-subscription-token"}}
	s.store.Catalogs["s"] = Catalog{Profiles: ps}
	w := call(s, "GET", "/api/state", nil)
	if strings.Contains(w.Body.String(), "private-subscription-token") || strings.Contains(w.Body.String(), "00000000-0000-4000") {
		t.Fatal("state exposes subscription credentials")
	}
}
func TestInvalidRequestAndPortCollision(t *testing.T) {
	s := testServer(t)
	w := call(s, "POST", "/api/settings", map[string]any{"proxy": ProxySettings{Mode: "mixed", SOCKSPort: 17790}, "refreshMinutes": 180})
	if w.Code != 400 {
		t.Fatal("accepted UI port for proxy")
	}
	w = call(s, "POST", "/api/sources", map[string]any{"name": "test", "content": fixture, "unknown": true})
	if w.Code != 400 {
		t.Fatal("accepted unknown body field")
	}
}
func TestPrivateSnapshotOptional(t *testing.T) {
	path := os.Getenv("PORTTIDE_PRIVATE_SNAPSHOT")
	if path == "" {
		t.Skip("private compatibility check is opt-in")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	ps, e := ParseProfiles("private", b)
	if e != nil {
		t.Fatal(e)
	}
	supported := 0
	reasons := map[string]int{}
	for _, p := range ps {
		if p.Supported {
			supported++
			if core := os.Getenv("PORTTIDE_TEST_CORE"); core != "" {
				cfg, e := CompileProfile(p, ProxySettings{Mode: "mixed", SOCKSPort: 17890})
				if e == nil {
					e = validateCore(context.Background(), core, cfg, t.TempDir())
				}
				if e != nil {
					t.Errorf("core rejected profile %q: %v", p.Name, e)
				}
			}
		} else {
			reasons[p.Reason]++
		}
	}
	t.Logf("profiles=%d supported=%d unsupported_reasons=%v", len(ps), supported, reasons)
}
