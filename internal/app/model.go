package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const Version = "0.1.0-alpha.2"
const MaxSubscriptionBytes int64 = 4 << 20

type ProxySettings struct {
	Mode      string `json:"mode"`
	SOCKSPort int    `json:"socksPort"`
	HTTPPort  int    `json:"httpPort"`
	Auth      bool   `json:"auth"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	UDP       bool   `json:"udp"`
}

func (p ProxySettings) Validate() error {
	if p.Mode != "mixed" && p.Mode != "separate" {
		return errors.New("proxy mode must be mixed or separate")
	}
	if p.SOCKSPort < 1024 || p.SOCKSPort > 65535 {
		return errors.New("SOCKS/mixed port must be between 1024 and 65535")
	}
	if p.Mode == "separate" && (p.HTTPPort < 1024 || p.HTTPPort > 65535 || p.HTTPPort == p.SOCKSPort) {
		return errors.New("HTTP port must be valid and different from the SOCKS port")
	}
	if p.Auth && (len(p.Username) == 0 || len(p.Username) > 255 || len(p.Password) == 0 || len(p.Password) > 255 || strings.Contains(p.Username, ":")) {
		return errors.New("authentication requires a username (without colon) and password, each 1–255 bytes")
	}
	return nil
}

type Source struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	UserAgent string `json:"userAgent"`
}
type DesktopSettings struct {
	StartAtLogin    bool `json:"startAtLogin"`
	ConnectOnLaunch bool `json:"connectOnLaunch"`
	Background      bool `json:"background"`
}

type Settings struct {
	Schema         int             `json:"schema"`
	CorePath       string          `json:"corePath"`
	Proxy          ProxySettings   `json:"proxy"`
	Sources        []Source        `json:"sources"`
	Selected       string          `json:"selected"`
	SelectedSource string          `json:"selectedSource,omitempty"`
	Desktop        DesktopSettings `json:"desktop"`
	RefreshMinutes int             `json:"refreshMinutes"`
}
type Profile struct {
	ID        string          `json:"id"`
	SourceID  string          `json:"sourceId"`
	Name      string          `json:"name"`
	Protocols []string        `json:"protocols"`
	Supported bool            `json:"supported"`
	Reason    string          `json:"reason,omitempty"`
	Config    json.RawMessage `json:"config,omitempty"`
}
type Catalog struct {
	Profiles    []Profile         `json:"profiles"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	LastAttempt time.Time         `json:"lastAttempt"`
	Error       string            `json:"error,omitempty"`
	Metadata    map[string]string `json:"metadata"`
}
type Store struct {
	Dir      string
	Settings Settings
	Catalogs map[string]Catalog
}

func token() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func Defaults() Settings {
	return Settings{Schema: 2, Desktop: DesktopSettings{ConnectOnLaunch: true, Background: true}, Proxy: ProxySettings{Mode: "mixed", SOCKSPort: 17890, HTTPPort: 0, Auth: true, Username: "porttide", Password: token()[:24], UDP: true}, Sources: []Source{}, RefreshMinutes: 180}
}
func OpenStore(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	s := &Store{Dir: dir, Settings: Defaults(), Catalogs: map[string]Catalog{}}
	if b, e := os.ReadFile(filepath.Join(dir, "settings.json")); e == nil {
		if e = json.Unmarshal(b, &s.Settings); e != nil {
			return nil, errors.New("settings.json is invalid; original file was preserved")
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if s.Settings.Schema == 1 {
		// Legacy autoStart was toggled by Stop, not a durable launch preference.
		if b, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
			if _, err = os.Stat(filepath.Join(dir, "settings.v1.backup.json")); os.IsNotExist(err) {
				if err = atomicWrite(filepath.Join(dir, "settings.v1.backup.json"), b); err != nil {
					return nil, err
				}
			}
		}
		s.Settings.Schema = 2
	}
	if s.Settings.Schema != 2 {
		return nil, errors.New("unsupported settings schema")
	}
	if e := s.Settings.Proxy.Validate(); e != nil {
		return nil, e
	}
	if s.Settings.RefreshMinutes < 5 {
		s.Settings.RefreshMinutes = 180
	}
	seen := map[string]bool{}
	for i := range s.Settings.Sources {
		src := &s.Settings.Sources[i]
		if src.ID == "" {
			src.ID = token()[:16]
		}
		if seen[src.ID] {
			return nil, errors.New("duplicate subscription ID in settings")
		}
		seen[src.ID] = true
		if src.UserAgent == "" {
			src.UserAgent = "Happ/2.0.0"
		}
	}
	if b, e := os.ReadFile(filepath.Join(dir, "cache.json")); e == nil {
		if e = json.Unmarshal(b, &s.Catalogs); e != nil {
			return nil, errors.New("cache.json is invalid; original file was preserved")
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if s.Catalogs == nil {
		s.Catalogs = map[string]Catalog{}
	}
	if s.Settings.SelectedSource == "" {
		if p, ok := s.Profile(s.Settings.Selected); ok {
			s.Settings.SelectedSource = p.SourceID
		}
	}
	if e := s.SaveSettings(); e != nil {
		return nil, e
	}
	return s, nil
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(path, append(b, '\n'))
}
func atomicWrite(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".porttide-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(tmp, path)
}
func (s *Store) SaveSettings() error {
	return writeJSON(filepath.Join(s.Dir, "settings.json"), s.Settings)
}
func (s *Store) SaveCatalogs() error {
	return writeJSON(filepath.Join(s.Dir, "cache.json"), s.Catalogs)
}
func (s *Store) Profile(id string) (Profile, bool) {
	for _, c := range s.Catalogs {
		for _, p := range c.Profiles {
			if p.ID == id {
				return p, true
			}
		}
	}
	return Profile{}, false
}
