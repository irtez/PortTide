package app

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// This test needs an explicitly supplied private cache, profile ID and core.
// It never runs in CI and prints no configuration or credential values.
func TestLiveDownloadOptional(t *testing.T) {
	file, id := os.Getenv("PORTTIDE_LIVE_CACHE"), os.Getenv("PORTTIDE_LIVE_PROFILE_ID")
	if file == "" || id == "" {
		t.Skip("explicit private opt-in")
	}
	core := integrationCore(t)
	b, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	var catalogs map[string]Catalog
	if e = json.Unmarshal(b, &catalogs); e != nil {
		t.Fatal(e)
	}
	var profile Profile
	for _, c := range catalogs {
		for _, p := range c.Profiles {
			if p.ID == id {
				profile = p
			}
		}
	}
	if profile.ID == "" {
		t.Fatal("profile not found")
	}
	port, _ := freePort()
	proxy := ProxySettings{Mode: "mixed", SOCKSPort: port, Auth: true, Username: "probe", Password: token()}
	cfg, e := CompileProfile(profile, proxy)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	proc, e := startCore(ctx, core, cfg, t.TempDir(), port)
	if e != nil {
		t.Fatal(e)
	}
	defer proc.stop()
	c := proxyClient(proxy)
	defer c.CloseIdleConnections()
	m := measureThroughput(ctx, c, false, defaultSpeedOptions(), nil)
	t.Logf("download_mbps=%.2f seconds=%.2f measured_bytes=%d payload_bytes=%d completed=%d failures=%d limit=%s error=%q", m.Value, m.Seconds, m.Bytes, m.TrafficBytes, m.Samples, m.Failures, m.Limit, m.Error)
	if m.Error != "" || m.Failures != 0 || m.Value <= 0 {
		t.Fatal("live download did not complete cleanly")
	}
}
