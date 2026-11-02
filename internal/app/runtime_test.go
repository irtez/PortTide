package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func integrationCore(t *testing.T) string {
	t.Helper()
	core := os.Getenv("PORTTIDE_TEST_CORE")
	if core == "" {
		t.Skip("set PORTTIDE_TEST_CORE for Xray integration tests")
	}
	p, e := filepath.Abs(core)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func directConfig(port int) []byte {
	b, _ := json.Marshal(map[string]any{"log": map[string]any{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "noauth"}}}, "outbounds": []any{map[string]any{"protocol": "freedom"}}})
	return b
}
func localProfile(port int) Profile {
	raw := fmt.Sprintf(`{"remarks":"Local integration fixture","outbounds":[{"tag":"upstream","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":%d}]}}]}`, port)
	p, _ := ParseProfiles("local", []byte(raw))
	return p[0]
}
func TestXrayMixedAndSeparateAuthentication(t *testing.T) {
	core := integrationCore(t)
	root := t.TempDir()
	upPort, _ := freePort()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	up, e := startCore(ctx, core, directConfig(upPort), root, upPort)
	if e != nil {
		t.Fatal(e)
	}
	defer up.stop()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "porttide-ok") }))
	defer target.Close()
	for _, mode := range []string{"mixed", "separate"} {
		t.Run(mode, func(t *testing.T) {
			port, _ := freePort()
			httpPort, _ := freePort()
			p := ProxySettings{Mode: mode, SOCKSPort: port, HTTPPort: httpPort, Auth: true, Username: "alice", Password: "correct-password"}
			cfg, e := CompileProfile(localProfile(upPort), p)
			if e != nil {
				t.Fatal(e)
			}
			if e = validateCore(ctx, core, cfg, root); e != nil {
				t.Fatal(e)
			}
			ports := []int{port}
			if mode == "separate" {
				ports = append(ports, httpPort)
			}
			proc, e := startCore(ctx, core, cfg, root, ports...)
			if e != nil {
				t.Fatal(e)
			}
			defer proc.stop()
			for _, scheme := range []string{"http", "socks5"} {
				proxyPort := port
				if mode == "separate" && scheme == "http" {
					proxyPort = httpPort
				}
				for _, password := range []string{"correct-password", "wrong-password", ""} {
					u := &url.URL{Scheme: scheme, Host: fmt.Sprintf("127.0.0.1:%d", proxyPort)}
					if password != "" {
						u.User = url.UserPassword("alice", password)
					}
					client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}, Timeout: 3 * time.Second}
					resp, e := client.Get(target.URL)
					success := false
					if e == nil {
						b, _ := io.ReadAll(resp.Body)
						resp.Body.Close()
						success = resp.StatusCode == 200 && string(b) == "porttide-ok"
					}
					client.CloseIdleConnections()
					if success != (password == "correct-password") {
						t.Fatalf("%s auth %q success=%v error=%v", scheme, password, success, e)
					}
				}
			}
		})
	}
}
func TestXrayRuntimeRollbackAndProbeIsolation(t *testing.T) {
	core := integrationCore(t)
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	upPort, _ := freePort()
	up, e := startCore(ctx, core, directConfig(upPort), root, upPort)
	if e != nil {
		t.Fatal(e)
	}
	defer up.stop()
	p := localProfile(upPort)
	port, _ := freePort()
	proxy := ProxySettings{Mode: "mixed", SOCKSPort: port}
	r := Runtime{root: root}
	defer r.Stop()
	if e = r.Apply(ctx, core, p, proxy); e != nil {
		t.Fatal(e)
	}
	occupied, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer occupied.Close()
	bad := proxy
	bad.SOCKSPort = occupied.Addr().(*net.TCPAddr).Port
	if e = r.Apply(ctx, core, p, bad); e == nil {
		t.Fatal("occupied port unexpectedly started")
	}
	if running, selected := r.Status(); !running || selected != p.ID {
		t.Fatal("previous runtime not restored")
	}
	probeCtx, abort := context.WithCancel(ctx)
	abort()
	result := runDiagnostic(probeCtx, core, root, p, "latency")
	if result.Error != "diagnostic canceled or timed out" {
		t.Fatal("canceled probe did not report cancellation")
	}
	if running, selected := r.Status(); !running || selected != p.ID {
		t.Fatal("probe disturbed runtime")
	}
}

func TestXrayParentDeath(t *testing.T) {
	core := integrationCore(t)
	if file := os.Getenv("PORTTIDE_HELPER_READY"); file != "" {
		port, _ := freePort()
		proc, e := startCore(context.Background(), core, directConfig(port), filepath.Dir(file), port)
		if e != nil {
			t.Fatal(e)
		}
		defer proc.stop()
		if e = os.WriteFile(file, []byte(strconv.Itoa(port)), 0600); e != nil {
			t.Fatal(e)
		}
		time.Sleep(30 * time.Second)
		return
	}
	file := filepath.Join(t.TempDir(), "ready")
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "-test.run=^TestXrayParentDeath$")
	prepareCommand(cmd)
	cmd.Env = append(os.Environ(), "PORTTIDE_HELPER_READY="+file)
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	var port string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, e := os.ReadFile(file); e == nil {
			port = string(b)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if port == "" {
		t.Fatal("helper did not start")
	}
	if e := cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = cmd.Wait()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, e := net.DialTimeout("tcp", "127.0.0.1:"+port, 100*time.Millisecond)
		if e != nil {
			return
		}
		c.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("Xray survived its owning process")
}
