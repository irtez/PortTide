package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type coreProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	err     error
	dir     string
	cleanup func()
}

func (p *coreProcess) stop() {
	select {
	case <-p.done:
	default:
		_ = p.cmd.Process.Kill()
		<-p.done
	}
	if p.cleanup != nil {
		p.cleanup()
	}
	_ = os.RemoveAll(p.dir)
}
func (p *coreProcess) running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
func validateCore(ctx context.Context, core string, cfg []byte, root string) error {
	if !filepath.IsAbs(core) {
		return errors.New("Xray path must be absolute")
	}
	dir, e := os.MkdirTemp(root, "validate-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "config.json")
	if e = os.WriteFile(path, cfg, 0600); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, core, "run", "-test", "-c", path)
	prepareCommand(cmd)
	cmd.Dir = filepath.Dir(core)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(core))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Run(); e != nil {
		return errors.New("Xray could not validate the profile; check core compatibility and configuration")
	}
	return nil
}
func startCore(ctx context.Context, core string, cfg []byte, root string, ports ...int) (*coreProcess, error) {
	dir, e := os.MkdirTemp(root, "run-")
	if e != nil {
		return nil, e
	}
	path := filepath.Join(dir, "config.json")
	if e = os.WriteFile(path, cfg, 0600); e != nil {
		os.RemoveAll(dir)
		return nil, e
	}
	cmd := exec.Command(core, "run", "-c", path)
	prepareCommand(cmd)
	cmd.Dir = filepath.Dir(core)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(core))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		os.RemoveAll(dir)
		return nil, errors.New("cannot start Xray; check the core path")
	}
	cleanup, guardErr := guardProcess(cmd)
	if guardErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.RemoveAll(dir)
		return nil, errors.New("cannot attach Xray to the application process lifetime")
	}
	p := &coreProcess{cmd: cmd, done: make(chan struct{}), dir: dir, cleanup: cleanup}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	started := time.Now()
	for {
		select {
		case <-ctx.Done():
			p.stop()
			return nil, ctx.Err()
		case <-p.done:
			p.stop()
			return nil, errors.New("Xray exited during startup; the port may be occupied or the profile unsupported")
		case <-deadline.C:
			p.stop()
			return nil, errors.New("Xray listeners did not become ready")
		case <-tick.C:
			if time.Since(started) < 400*time.Millisecond {
				continue
			}
			ready := true
			for _, port := range ports {
				c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 80*time.Millisecond)
				if e != nil {
					ready = false
					break
				}
				c.Close()
			}
			if ready && p.running() {
				return p, nil
			}
		}
	}
}

type Runtime struct {
	mu         sync.Mutex
	process    *coreProcess
	core, root string
	config     []byte
	proxy      ProxySettings
	selected   string
}

func (r *Runtime) Apply(ctx context.Context, core string, p Profile, proxy ProxySettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, e := CompileProfile(p, proxy)
	if e != nil {
		return e
	}
	if e = validateCore(ctx, core, cfg, r.root); e != nil {
		return e
	}
	oldConfig, oldCore, oldProxy, oldSelected := r.config, r.core, r.proxy, r.selected
	if r.process != nil {
		r.process.stop()
		r.process = nil
	}
	ports := []int{proxy.SOCKSPort}
	if proxy.Mode == "separate" {
		ports = append(ports, proxy.HTTPPort)
	}
	next, e := startCore(ctx, core, cfg, r.root, ports...)
	if e != nil {
		if oldConfig != nil {
			rollbackPorts := []int{oldProxy.SOCKSPort}
			if oldProxy.Mode == "separate" {
				rollbackPorts = append(rollbackPorts, oldProxy.HTTPPort)
			}
			recovery, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			r.process, _ = startCore(recovery, oldCore, oldConfig, r.root, rollbackPorts...)
			r.config, r.core, r.proxy, r.selected = oldConfig, oldCore, oldProxy, oldSelected
			if r.process == nil {
				return errors.New("new profile failed and the previous process could not be restored; check ports and core")
			}
		}
		return e
	}
	r.process, r.config, r.core, r.proxy, r.selected = next, cfg, core, proxy, p.ID
	return nil
}
func (r *Runtime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.process != nil {
		r.process.stop()
		r.process = nil
	}
	r.config = nil
	r.selected = ""
}
func (r *Runtime) Status() (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.process != nil && r.process.running(), r.selected
}
func freePort() (int, error) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return 0, e
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
func resolveCore(configured string) string {
	if configured != "" {
		p, _ := filepath.Abs(configured)
		return p
	}
	name := "xray"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe, _ := os.Executable()
	p := filepath.Join(filepath.Dir(exe), "core", name)
	if _, e := os.Stat(p); e == nil {
		return p
	}
	p, _ = filepath.Abs(filepath.Join("core", name))
	return p
}
