package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const releasesAPI = "https://api.github.com/repos/irtez/PortTide/releases?per_page=100"
const releaseRoot = "https://github.com/irtez/PortTide/releases/"
const maxInstallerBytes = int64(128 << 20)

type UpdateState struct {
	Checking   bool         `json:"checking"`
	Installing bool         `json:"installing"`
	Available  bool         `json:"available"`
	Version    string       `json:"version,omitempty"`
	Page       string       `json:"page,omitempty"`
	Error      string       `json:"error,omitempty"`
	Phase      string       `json:"phase,omitempty"`
	CheckedAt  time.Time    `json:"checkedAt"`
	CanInstall bool         `json:"canInstall"`
	Asset      releaseAsset `json:"-"`
}
type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}
type release struct {
	Tag        string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?$`)

func compareVersions(a, b string) int {
	aa, bb := versionPattern.FindStringSubmatch(a), versionPattern.FindStringSubmatch(b)
	if aa == nil || bb == nil {
		return 0
	}
	for i := 1; i <= 3; i++ {
		av, _ := strconv.ParseUint(aa[i], 10, 64)
		bv, _ := strconv.ParseUint(bb[i], 10, 64)
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	if aa[4] == bb[4] {
		return 0
	}
	if aa[4] == "" {
		return 1
	}
	if bb[4] == "" {
		return -1
	}
	ap, bp := strings.Split(aa[4], "."), strings.Split(bb[4], ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] == bp[i] {
			continue
		}
		av, ae := strconv.ParseUint(ap[i], 10, 64)
		bv, be := strconv.ParseUint(bp[i], 10, 64)
		if ae == nil && be == nil {
			if av > bv {
				return 1
			}
			return -1
		}
		if ae == nil {
			return -1
		}
		if be == nil {
			return 1
		}
		if ap[i] > bp[i] {
			return 1
		}
		return -1
	}
	if len(ap) > len(bp) {
		return 1
	}
	return -1
}
func chooseRelease(rs []release, current string) UpdateState {
	u := UpdateState{CheckedAt: time.Now().UTC()}
	for _, r := range rs {
		if r.Draft || versionPattern.FindStringSubmatch(r.Tag) == nil || compareVersions(r.Tag, current) <= 0 {
			continue
		}
		if !strings.Contains(current, "-") && (r.Prerelease || strings.Contains(r.Tag, "-")) {
			continue
		}
		if u.Available && compareVersions(r.Tag, u.Version) <= 0 {
			continue
		}
		version := strings.TrimPrefix(r.Tag, "v")
		next := UpdateState{Available: true, Version: version, Page: releaseRoot + "tag/" + r.Tag, CheckedAt: u.CheckedAt}
		name := "PortTide-" + version + "-windows-x64-setup.exe"
		for _, asset := range r.Assets {
			if asset.Name == name && asset.URL == releaseRoot+"download/"+r.Tag+"/"+name && asset.Size > 0 && asset.Size <= maxInstallerBytes {
				digest := strings.TrimPrefix(asset.Digest, "sha256:")
				raw, err := hex.DecodeString(digest)
				if strings.HasPrefix(asset.Digest, "sha256:") && err == nil && len(raw) == 32 {
					next.Asset = asset
				}
			}
		}
		next.CanInstall = runtime.GOOS == "windows" && next.Asset.URL != ""
		u = next
	}
	return u
}
func fetchUpdates(ctx context.Context, client *http.Client, endpoint, current string) (UpdateState, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return UpdateState{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PortTide/"+Version)
	resp, err := client.Do(req)
	if err != nil {
		return UpdateState{}, errors.New("не удалось связаться с GitHub")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return UpdateState{}, fmt.Errorf("GitHub вернул HTTP %d; попробуйте позже", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if err != nil || len(b) > 4<<20 {
		return UpdateState{}, errors.New("некорректный ответ GitHub")
	}
	var rs []release
	if json.Unmarshal(b, &rs) != nil {
		return UpdateState{}, errors.New("некорректный список версий")
	}
	return chooseRelease(rs, current), nil
}

// The caller holds s.mu. Prefer the working proxy while downloading the update.
func (s *Server) updateClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if running, _ := s.runtime.Status(); running {
		p := s.store.Settings.Proxy
		u := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", p.SOCKSPort)}
		if p.Auth {
			u.User = url.UserPassword(p.Username, p.Password)
		}
		t.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: t, Timeout: 5 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many redirects")
		}
		host := req.URL.Hostname()
		if req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" || (host != "github.com" && host != "api.github.com" && host != "release-assets.githubusercontent.com" && host != "objects.githubusercontent.com") {
			return errors.New("unexpected update redirect")
		}
		return nil
	}}
}
func (s *Server) beginUpdateCheck() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil || s.update.Checking || s.update.Installing {
		return false
	}
	s.update.Checking = true
	s.update.Error = ""
	client := s.updateClient()
	s.jobs.Add(1)
	go func() {
		defer s.jobs.Done()
		defer client.CloseIdleConnections()
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		defer cancel()
		u, err := fetchUpdates(ctx, client, releasesAPI, Version)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err != nil {
			s.update.Checking = false
			s.update.Error = err.Error()
			s.update.CheckedAt = time.Now().UTC()
		} else {
			u.CanInstall = u.CanInstall && sameDataDir(s.store.Dir, DefaultDataDir())
			s.update = u
		}
	}()
	return true
}
func (s *Server) checkUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.beginUpdateCheck() {
		fail(w, 409, errors.New("проверка или установка уже выполняется"))
		return
	}
	send(w, 202, map[string]bool{"ok": true})
}
func (s *Server) updateLoop() {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			s.beginUpdateCheck()
			timer.Reset(24 * time.Hour)
		}
	}
}
func downloadInstaller(ctx context.Context, client *http.Client, asset releaseAsset, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", asset.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "PortTide/"+Version)
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("не удалось скачать установщик")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("скачивание: HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp(dir, "porttide-update-*.exe")
	if err != nil {
		return "", err
	}
	path := f.Name()
	keep := false
	defer func() {
		f.Close()
		if !keep {
			os.Remove(path)
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, maxInstallerBytes+1))
	if err != nil {
		return "", errors.New("скачивание прервано")
	}
	if n != asset.Size || n > maxInstallerBytes || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != asset.Digest {
		return "", errors.New("проверка установщика не пройдена; текущая версия сохранена")
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	keep = true
	return path, nil
}
func (s *Server) installUpdate(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.update.Available || !s.update.CanInstall || s.update.Checking || s.update.Installing {
		fail(w, 409, errors.New("нет проверенного установщика для обновления"))
		return
	}
	if s.job.Running {
		fail(w, 409, errors.New("дождитесь завершения проверки или отмените её"))
		return
	}
	// A custom data directory is a separate developer/portable instance, not the installed app.
	if !sameDataDir(s.store.Dir, DefaultDataDir()) {
		fail(w, 409, errors.New("этот экземпляр использует отдельную папку данных; обновите его вручную"))
		return
	}
	asset := s.update.Asset
	client := s.updateClient()
	s.update.Installing = true
	s.update.Error = ""
	s.update.Phase = "Скачивание обновления…"
	s.jobs.Add(1)
	go func() {
		defer s.jobs.Done()
		defer client.CloseIdleConnections()
		phaseReady := make(chan struct{})
		defer close(phaseReady)
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
		defer cancel()
		dir := filepath.Join(s.store.Dir, "updates")
		err := os.MkdirAll(dir, 0700)
		path := ""
		if err == nil {
			path, err = downloadInstaller(ctx, client, asset, dir)
		}
		if err == nil {
			args := []string{"/SP-", "/SILENT", "/NORESTART", "/UPDATE=1"}
			exe, _ := os.Executable()
			if _, e := os.Stat(filepath.Join(filepath.Dir(exe), "installed.txt")); e == nil {
				args = append(args, "/DIR="+filepath.Dir(exe))
			}
			cmd := exec.Command(path, args...)
			prepareCommand(cmd)
			err = cmd.Start()
			if err == nil {
				go func() {
					waitErr := cmd.Wait()
					<-phaseReady
					s.mu.Lock()
					defer s.mu.Unlock()
					if s.ctx.Err() == nil {
						s.update.Installing = false
						s.update.Phase = ""
						if waitErr != nil {
							s.update.Error = "Установка не завершена. Текущая версия продолжает работать."
						}
					}
				}()
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err != nil {
			s.update.Installing = false
			s.update.Error = "Обновление не установлено: " + err.Error()
			s.update.Phase = ""
			if path != "" {
				os.Remove(path)
			}
		} else {
			s.update.Phase = "Установщик запущен. Ожидаем перезапуска…"
		}
	}()
	send(w, 202, map[string]bool{"ok": true})
}
func sameDataDir(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
