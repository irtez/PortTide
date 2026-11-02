package app

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type Job struct {
	ID        string       `json:"id"`
	ProfileID string       `json:"profileId"`
	Kind      string       `json:"kind"`
	Running   bool         `json:"running"`
	Result    *TestResult  `json:"result,omitempty"`
	StartedAt time.Time    `json:"startedAt"`
	Progress  TestProgress `json:"progress"`
}
type Server struct {
	mu          sync.Mutex
	store       *Store
	runtime     *Runtime
	token, host string
	ctx         context.Context
	cancel      context.CancelFunc
	job         Job
	jobCancel   context.CancelFunc
	jobs        sync.WaitGroup
	results     map[string]TestResult
	notice      string
}

func newServer(ctx context.Context, store *Store, host string) *Server {
	ctx, cancel := context.WithCancel(ctx)
	return &Server{store: store, runtime: &Runtime{root: store.Dir}, token: token(), host: host, ctx: ctx, cancel: cancel, results: map[string]TestResult{}}
}
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("POST /api/sources", s.saveSource)
	mux.HandleFunc("POST /api/sources/{id}/refresh", s.refreshSource)
	mux.HandleFunc("DELETE /api/sources/{id}", s.deleteSource)
	mux.HandleFunc("POST /api/select", s.selectProfile)
	mux.HandleFunc("POST /api/stop", s.stop)
	mux.HandleFunc("POST /api/settings", s.settings)
	mux.HandleFunc("POST /api/tests", s.test)
	mux.HandleFunc("POST /api/tests/cancel", s.cancelTest)
	mux.HandleFunc("POST /api/shutdown", func(w http.ResponseWriter, r *http.Request) { send(w, 200, map[string]bool{"ok": true}); s.cancel() })
	assets, _ := fs.Sub(webFiles, "web")
	static := http.FileServer(http.FS(assets))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			b, _ := webFiles.ReadFile("web/index.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, strings.ReplaceAll(string(b), "__PORTTIDE_TOKEN__", s.token))
			return
		}
		static.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if r.Host != s.host {
			fail(w, 403, errors.New("unexpected Host"))
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+s.host {
			fail(w, 403, errors.New("cross-origin access denied"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-PortTide-Token")), []byte(s.token)) != 1 {
				fail(w, 401, errors.New("reload the local PortTide page to authorize this request"))
				return
			}
			if r.Method != "GET" && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				fail(w, 415, errors.New("JSON required"))
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, e error) {
	send(w, status, map[string]string{"error": e.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 9<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, 400, errors.New("invalid request body"))
		return false
	}
	if e := d.Decode(new(any)); e != io.EOF {
		fail(w, 400, errors.New("unexpected trailing request content"))
		return false
	}
	return true
}
func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	running, active := s.runtime.Status()
	profiles := []Profile{}
	sources := []map[string]any{}
	for _, src := range s.store.Settings.Sources {
		c := s.store.Catalogs[src.ID]
		urlHint := "Local JSON file"
		if u, e := sourceURL(src.URL); e == nil {
			urlHint = u.Scheme + "://" + u.Host + "/…"
		}
		sources = append(sources, map[string]any{"id": src.ID, "name": src.Name, "urlHint": urlHint, "remote": src.URL != "", "userAgent": src.UserAgent, "updatedAt": c.UpdatedAt, "error": c.Error, "metadata": c.Metadata, "count": len(c.Profiles)})
		for _, p := range c.Profiles {
			p.Config = nil
			profiles = append(profiles, p)
		}
	}
	core := resolveCore(s.store.Settings.CorePath)
	_, ce := os.Stat(core)
	send(w, 200, map[string]any{"version": Version, "running": running, "active": active, "selected": s.store.Settings.Selected, "proxy": s.store.Settings.Proxy, "corePath": s.store.Settings.CorePath, "coreResolved": core, "coreAvailable": ce == nil, "refreshMinutes": s.store.Settings.RefreshMinutes, "sources": sources, "profiles": profiles, "job": s.job, "results": s.results, "notice": s.notice})
}
func (s *Server) sourceIndex(id string) int {
	for i, v := range s.store.Settings.Sources {
		if v.ID == id {
			return i
		}
	}
	return -1
}
func (s *Server) acceptCatalog(ctx context.Context, src Source, catalog Catalog) error {
	running, active := s.runtime.Status()
	oldCatalog, existed := s.store.Catalogs[src.ID]
	oldSelected := s.store.Settings.Selected
	oldProfile, oldFound := s.store.Profile(active)
	if running && oldFound && oldProfile.SourceID == src.ID {
		var replacement *Profile
		for i := range catalog.Profiles {
			if catalog.Profiles[i].ID == active {
				replacement = &catalog.Profiles[i]
				break
			}
		}
		if replacement == nil || !replacement.Supported {
			return errors.New("active profile is missing or unsupported in the update; stop it before accepting this subscription")
		}
		if string(replacement.Config) != string(oldProfile.Config) {
			if e := s.runtime.Apply(ctx, resolveCore(s.store.Settings.CorePath), *replacement, s.store.Settings.Proxy); e != nil {
				return e
			}
		}
	}
	s.store.Catalogs[src.ID] = catalog
	if e := s.store.SaveCatalogs(); e != nil {
		if existed {
			s.store.Catalogs[src.ID] = oldCatalog
		} else {
			delete(s.store.Catalogs, src.ID)
		}
		if running && oldFound && oldProfile.SourceID == src.ID {
			_ = s.runtime.Apply(context.Background(), resolveCore(s.store.Settings.CorePath), oldProfile, s.store.Settings.Proxy)
		}
		return errors.New("could not save subscription cache")
	}
	if _, ok := s.store.Profile(oldSelected); !ok && oldSelected != "" {
		s.store.Settings.Selected = ""
		s.store.Settings.AutoStart = false
		if e := s.store.SaveSettings(); e != nil {
			return errors.New("cache updated but selection could not be saved")
		}
	}
	return nil
}
func (s *Server) saveSource(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		URL       string `json:"url"`
		UserAgent string `json:"userAgent"`
		Content   string `json:"content"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.sourceIndex(in.ID)
	if in.ID != "" && idx < 0 {
		fail(w, 404, errors.New("subscription not found"))
		return
	}
	if idx < 0 && len(s.store.Settings.Sources) >= 32 {
		fail(w, 400, errors.New("maximum 32 subscriptions"))
		return
	}
	src := Source{ID: in.ID, Name: strings.TrimSpace(in.Name), URL: strings.TrimSpace(in.URL), UserAgent: strings.TrimSpace(in.UserAgent)}
	if src.ID == "" {
		src.ID = token()[:16]
	}
	if src.Name == "" || len(src.Name) > 200 {
		fail(w, 400, errors.New("subscription name is required (maximum 200 bytes)"))
		return
	}
	if src.UserAgent == "" {
		src.UserAgent = "Happ/2.0.0"
	}
	if len(src.UserAgent) > 256 || strings.ContainsAny(src.UserAgent, "\r\n") {
		fail(w, 400, errors.New("invalid User-Agent"))
		return
	}
	if src.URL == "" && idx >= 0 && in.Content == "" {
		src.URL = s.store.Settings.Sources[idx].URL
	}
	var c Catalog
	var e error
	if in.Content != "" {
		if src.URL != "" {
			fail(w, 400, errors.New("choose a subscription URL or a JSON file"))
			return
		}
		c.Profiles, e = ParseProfiles(src.ID, []byte(in.Content))
		c.UpdatedAt = time.Now().UTC()
		c.LastAttempt = c.UpdatedAt
		c.Metadata = map[string]string{}
	} else {
		if len(src.URL) > 8192 {
			fail(w, 400, errors.New("subscription URL is too long"))
			return
		}
		c, e = FetchSource(r.Context(), src)
	}
	if e != nil {
		fail(w, 400, e)
		return
	}
	oldSources := append([]Source(nil), s.store.Settings.Sources...)
	if idx < 0 {
		s.store.Settings.Sources = append(s.store.Settings.Sources, src)
	} else {
		s.store.Settings.Sources[idx] = src
	}
	if e = s.store.SaveSettings(); e != nil {
		s.store.Settings.Sources = oldSources
		fail(w, 500, errors.New("could not save subscription settings"))
		return
	}
	if e = s.acceptCatalog(r.Context(), src, c); e != nil {
		s.store.Settings.Sources = oldSources
		_ = s.store.SaveSettings()
		fail(w, 400, e)
		return
	}
	send(w, 200, map[string]string{"id": src.ID})
}
func (s *Server) refreshLocked(ctx context.Context, id string) error {
	idx := s.sourceIndex(id)
	if idx < 0 {
		return errors.New("subscription not found")
	}
	src := s.store.Settings.Sources[idx]
	if src.URL == "" {
		return errors.New("local JSON sources are updated by importing a new file")
	}
	c, e := FetchSource(ctx, src)
	if e == nil {
		e = s.acceptCatalog(ctx, src, c)
	}
	if e != nil {
		old := s.store.Catalogs[id]
		old.Error = e.Error()
		old.LastAttempt = time.Now().UTC()
		s.store.Catalogs[id] = old
		_ = s.store.SaveCatalogs()
	}
	return e
}
func (s *Server) refreshSource(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.refreshLocked(r.Context(), r.PathValue("id")); e != nil {
		fail(w, 400, e)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	idx := s.sourceIndex(id)
	if idx < 0 {
		fail(w, 404, errors.New("subscription not found"))
		return
	}
	running, active := s.runtime.Status()
	p, _ := s.store.Profile(active)
	if running && p.SourceID == id {
		fail(w, 409, errors.New("stop the active profile before deleting its subscription"))
		return
	}
	old := s.store.Settings
	s.store.Settings.Sources = append(append([]Source(nil), old.Sources[:idx]...), old.Sources[idx+1:]...)
	if p, ok := s.store.Profile(s.store.Settings.Selected); ok && p.SourceID == id {
		s.store.Settings.Selected = ""
		s.store.Settings.AutoStart = false
	}
	if e := s.store.SaveSettings(); e != nil {
		s.store.Settings = old
		fail(w, 500, errors.New("could not save settings"))
		return
	}
	delete(s.store.Catalogs, id)
	if e := s.store.SaveCatalogs(); e != nil {
		fail(w, 500, errors.New("source removed; old private cache could not be cleaned"))
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (s *Server) selectProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.store.Profile(in.ID)
	if !ok {
		fail(w, 404, errors.New("profile not found"))
		return
	}
	old := s.store.Settings
	wasRunning, active := s.runtime.Status()
	previous, hasPrevious := s.store.Profile(active)
	if e := s.runtime.Apply(r.Context(), resolveCore(old.CorePath), p, old.Proxy); e != nil {
		fail(w, 400, e)
		return
	}
	s.store.Settings.Selected = p.ID
	s.store.Settings.AutoStart = true
	if e := s.store.SaveSettings(); e != nil {
		s.store.Settings = old
		if wasRunning && hasPrevious {
			_ = s.runtime.Apply(context.Background(), resolveCore(old.CorePath), previous, old.Proxy)
		} else {
			s.runtime.Stop()
		}
		fail(w, 500, errors.New("selection could not be saved; previous selection restored"))
		return
	}
	s.notice = ""
	send(w, 200, map[string]bool{"ok": true})
}
func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtime.Stop()
	s.store.Settings.AutoStart = false
	if e := s.store.SaveSettings(); e != nil {
		fail(w, 500, errors.New("proxy stopped, but restart preference could not be saved"))
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Proxy          ProxySettings `json:"proxy"`
		CorePath       string        `json:"corePath"`
		RefreshMinutes int           `json:"refreshMinutes"`
	}
	if !decode(w, r, &in) {
		return
	}
	if e := in.Proxy.Validate(); e != nil {
		fail(w, 400, e)
		return
	}
	if in.RefreshMinutes < 5 || in.RefreshMinutes > 10080 {
		fail(w, 400, errors.New("refresh interval must be 5–10080 minutes"))
		return
	}
	_, uiPort, _ := net.SplitHostPort(s.host)
	if fmt.Sprint(in.Proxy.SOCKSPort) == uiPort || (in.Proxy.Mode == "separate" && fmt.Sprint(in.Proxy.HTTPPort) == uiPort) {
		fail(w, 400, errors.New("proxy ports must differ from the web interface port"))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.store.Settings
	running, active := s.runtime.Status()
	p, ok := s.store.Profile(active)
	if running && ok {
		if e := s.runtime.Apply(r.Context(), resolveCore(in.CorePath), p, in.Proxy); e != nil {
			fail(w, 400, e)
			return
		}
	}
	s.store.Settings.Proxy = in.Proxy
	s.store.Settings.CorePath = in.CorePath
	s.store.Settings.RefreshMinutes = in.RefreshMinutes
	if e := s.store.SaveSettings(); e != nil {
		s.store.Settings = old
		if running && ok {
			_ = s.runtime.Apply(context.Background(), resolveCore(old.CorePath), p, old.Proxy)
		}
		fail(w, 500, errors.New("could not save settings"))
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (s *Server) test(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job.Running {
		fail(w, 409, errors.New("another diagnostic is running; wait or cancel it"))
		return
	}
	p, ok := s.store.Profile(in.ID)
	if !ok || !p.Supported {
		fail(w, 400, errors.New("choose a supported profile"))
		return
	}
	if in.Kind != "latency" && in.Kind != "speed" && in.Kind != "geo" {
		fail(w, 400, errors.New("unknown diagnostic"))
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 180*time.Second)
	s.jobCancel = cancel
	s.job = Job{ID: token()[:16], ProfileID: p.ID, Kind: in.Kind, Running: true, StartedAt: time.Now().UTC()}
	core, root := resolveCore(s.store.Settings.CorePath), s.store.Dir
	s.jobs.Add(1)
	go func() {
		defer s.jobs.Done()
		defer cancel()
		result := runDiagnosticWithProgress(ctx, core, root, p, in.Kind, func(progress TestProgress) { s.mu.Lock(); s.job.Progress = progress; s.mu.Unlock() })
		s.mu.Lock()
		defer s.mu.Unlock()
		s.job.Running = false
		s.job.Result = &result
		s.results[p.ID+":"+in.Kind] = result
		s.jobCancel = nil
	}()
	send(w, 202, s.job)
}
func (s *Server) cancelTest(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobCancel != nil {
		s.jobCancel()
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (s *Server) refreshLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			for _, src := range s.store.Settings.Sources {
				if src.URL != "" && time.Since(s.store.Catalogs[src.ID].LastAttempt) > time.Duration(s.store.Settings.RefreshMinutes)*time.Minute {
					_ = s.refreshLocked(s.ctx, src.ID)
				}
			}
			s.mu.Unlock()
		}
	}
}

type Options struct {
	DataDir, Listen, Core string
	Open                  bool
}

func DefaultDataDir() string {
	if runtime.GOOS == "windows" && os.Getenv("LOCALAPPDATA") != "" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "PortTide")
	}
	p, _ := os.UserConfigDir()
	return filepath.Join(p, "PortTide")
}
func Run(o Options) error {
	if o.DataDir == "" {
		o.DataDir = DefaultDataDir()
	}
	dir, e := filepath.Abs(o.DataDir)
	if e != nil {
		return e
	}
	store, e := OpenStore(dir)
	if e != nil {
		return e
	}
	file, e := os.OpenFile(filepath.Join(dir, "porttide.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e == nil {
		defer file.Close()
		log.SetOutput(file)
	}
	if o.Core != "" {
		store.Settings.CorePath = o.Core
	}
	host, _, e := net.SplitHostPort(o.Listen)
	if e != nil || host != "127.0.0.1" {
		return errors.New("web interface must listen on 127.0.0.1")
	}
	listener, e := net.Listen("tcp", o.Listen)
	if e != nil {
		return errors.New("PortTide web port is occupied; an instance may already be running")
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	s := newServer(ctx, store, listener.Addr().String())
	defer s.cancel()
	server := &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	if store.Settings.AutoStart && store.Settings.Selected != "" {
		if p, ok := store.Profile(store.Settings.Selected); ok {
			if e := s.runtime.Apply(s.ctx, resolveCore(store.Settings.CorePath), p, store.Settings.Proxy); e != nil {
				s.notice = e.Error()
			}
		} else {
			s.notice = "previously selected profile is not in the local cache"
		}
	}
	url := "http://" + listener.Addr().String()
	log.Printf("PortTide %s listening on %s", Version, url)
	fmt.Println(url)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	go s.refreshLoop()
	if o.Open {
		_ = openBrowser(url)
	}
	select {
	case <-s.ctx.Done():
	case e = <-serveErr:
		if e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Printf("web server stopped: %v", e)
		}
	}
	s.cancel()
	s.jobs.Wait()
	s.runtime.Stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}
