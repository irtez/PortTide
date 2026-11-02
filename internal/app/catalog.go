package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func object(raw []byte) (map[string]any, error) {
	var m map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := d.Decode(&m); e != nil || m == nil {
		return nil, errors.New("invalid JSON object")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errors.New("trailing JSON content")
	}
	return m, nil
}
func str(v any) string         { s, _ := v.(string); return s }
func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func list(v any) []any         { a, _ := v.([]any); return a }
func digest(s string) string   { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:])[:24] }
func ParseProfiles(sourceID string, body []byte) ([]Profile, error) {
	if int64(len(body)) > MaxSubscriptionBytes {
		return nil, errors.New("subscription exceeds 4 MiB")
	}
	body = bytes.TrimPrefix(bytes.TrimSpace(body), []byte{0xef, 0xbb, 0xbf})
	var entries []json.RawMessage
	if len(body) == 0 {
		return nil, errors.New("subscription is empty")
	}
	if body[0] == '[' {
		if e := json.Unmarshal(body, &entries); e != nil {
			return nil, errors.New("invalid Xray JSON array")
		}
	} else if body[0] == '{' {
		entries = []json.RawMessage{append([]byte(nil), body...)}
	} else {
		return nil, errors.New("expected unencrypted Xray JSON; this subscription format is not supported")
	}
	if len(entries) == 0 || len(entries) > 512 {
		return nil, errors.New("subscription must contain 1–512 profiles")
	}
	names := map[string]int{}
	profiles := make([]Profile, 0, len(entries))
	for _, raw := range entries {
		m, e := object(raw)
		if e != nil {
			return nil, e
		}
		if len(list(m["outbounds"])) == 0 {
			return nil, errors.New("profile has no Xray outbounds")
		}
		name := str(m["remarks"])
		if name == "" {
			name = str(m["name"])
		}
		if name == "" {
			name = "Unnamed profile " + digest(string(raw))[:6]
		}
		names[name]++
		protocols := map[string]bool{}
		for _, v := range list(m["outbounds"]) {
			p := str(obj(v)["protocol"])
			if p != "freedom" && p != "blackhole" && p != "dns" {
				protocols[p] = true
			}
		}
		ps := []string{}
		for p := range protocols {
			ps = append(ps, p)
		}
		sort.Strings(ps)
		profiles = append(profiles, Profile{SourceID: sourceID, Name: name, Protocols: ps, Config: raw})
	}
	unique := map[string]bool{}
	for i := range profiles {
		p := &profiles[i]
		identity := sourceID + "\x00" + p.Name
		if names[p.Name] > 1 {
			m, _ := object(p.Config)
			canonical, _ := json.Marshal(m)
			identity += "\x00" + string(canonical)
		}
		p.ID = digest(identity)
		if unique[p.ID] {
			p.ID = digest(identity + fmt.Sprint(i))
		}
		unique[p.ID] = true
		_, e := CompileProfile(*p, ProxySettings{Mode: "mixed", SOCKSPort: 17890})
		p.Supported = e == nil
		if e != nil {
			p.Reason = e.Error()
		}
	}
	return profiles, nil
}
func sourceURL(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("subscription must be an HTTPS URL without userinfo or fragment")
	}
	return u, nil
}
func FetchSource(ctx context.Context, src Source) (Catalog, error) {
	if _, e := sourceURL(src.URL); e != nil {
		return Catalog{}, e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", src.URL, nil)
	if e != nil {
		return Catalog{}, errors.New("invalid subscription URL")
	}
	req.Header.Set("User-Agent", src.UserAgent)
	client := &http.Client{Timeout: 35 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("redirect refused")
		}
		return nil
	}}
	resp, e := client.Do(req)
	if e != nil {
		return Catalog{}, errors.New("subscription download failed (network, TLS or redirect); check the URL and connection")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Catalog{}, fmt.Errorf("subscription server returned HTTP %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, MaxSubscriptionBytes+1))
	if e != nil {
		return Catalog{}, errors.New("subscription download was interrupted")
	}
	p, e := ParseProfiles(src.ID, b)
	if e != nil {
		return Catalog{}, e
	}
	meta := map[string]string{}
	for _, key := range []string{"profile-title", "profile-description", "announce", "subscription-userinfo", "profile-update-interval", "support-url", "profile-web-page-url"} {
		v := resp.Header.Get(key)
		if strings.HasPrefix(v, "base64:") {
			if dec, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, "base64:")); e == nil {
				v = string(dec)
			}
		}
		if len(v) > 8192 {
			v = v[:8192]
		}
		if v != "" {
			meta[key] = v
		}
	}
	return Catalog{Profiles: p, UpdatedAt: time.Now().UTC(), LastAttempt: time.Now().UTC(), Metadata: meta}, nil
}
