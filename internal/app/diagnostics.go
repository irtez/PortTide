package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Measurement struct {
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	Target        string  `json:"target"`
	IP            string  `json:"ip,omitempty"`
	Country       string  `json:"country,omitempty"`
	Region        string  `json:"region,omitempty"`
	Organization  string  `json:"organization,omitempty"`
	ASN           string  `json:"asn,omitempty"`
	Value         float64 `json:"value,omitempty"`
	Bytes         int64   `json:"bytes,omitempty"`
	Seconds       float64 `json:"seconds,omitempty"`
	Partial       bool    `json:"partial,omitempty"`
	Error         string  `json:"error,omitempty"`
	Warning       string  `json:"warning,omitempty"`
	Streams       int     `json:"streams,omitempty"`
	Samples       int     `json:"samples,omitempty"`
	Failures      int     `json:"failures,omitempty"`
	Min           float64 `json:"min,omitempty"`
	Max           float64 `json:"max,omitempty"`
	P95           float64 `json:"p95,omitempty"`
	Jitter        float64 `json:"jitter,omitempty"`
	WarmupBytes   int64   `json:"warmupBytes,omitempty"`
	WarmupSeconds float64 `json:"warmupSeconds,omitempty"`
	TrafficBytes  int64   `json:"trafficBytes,omitempty"`
	Limit         string  `json:"limit,omitempty"`
}
type TestResult struct {
	ProfileID    string        `json:"profileId"`
	Kind         string        `json:"kind"`
	At           time.Time     `json:"at"`
	Measurements []Measurement `json:"measurements"`
	Error        string        `json:"error,omitempty"`
}

func proxyClient(p ProxySettings) *http.Client {
	u := &url.URL{Scheme: "socks5", Host: fmt.Sprintf("127.0.0.1:%d", p.SOCKSPort)}
	if p.Auth {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u), MaxIdleConns: 8, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 4, DisableCompression: true, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 12 * time.Second}, Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || req.URL.Scheme != "https" {
			return errors.New("redirect refused")
		}
		return nil
	}}
}
func request(ctx context.Context, c *http.Client, target string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
	resp, e := c.Do(req)
	if e != nil {
		return nil, errors.New("request failed or timed out")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if e != nil {
		return nil, errors.New("incomplete response")
	}
	if len(b) > 65536 {
		return nil, errors.New("response is too large")
	}
	return b, nil
}
func latency(ctx context.Context, c *http.Client) []Measurement {
	return latencyWithProgress(ctx, c, nil)
}
func latencyWithProgress(ctx context.Context, c *http.Client, report progressReporter) []Measurement {
	target := "https://www.gstatic.com/generate_204"
	reportStage(report, "latency-warmup", 0, 0, 0)
	warm, finish := context.WithTimeout(ctx, 5*time.Second)
	_, _ = request(warm, c, target)
	finish()
	samples := []float64{}
	ms := []Measurement{}
	failures := 0
	for i := 0; i < 7 && ctx.Err() == nil; i++ {
		reportStage(report, "latency", 0, i, 7)
		probe, done := context.WithTimeout(ctx, 5*time.Second)
		start := time.Now()
		_, e := request(probe, c, target)
		done()
		m := Measurement{Kind: "latency", Name: fmt.Sprintf("HTTPS sample %d", i+1), Target: target}
		if e != nil {
			m.Error = e.Error()
			failures++
		} else {
			m.Value = float64(time.Since(start).Microseconds()) / 1000
			samples = append(samples, m.Value)
		}
		ms = append(ms, m)
		if i < 6 {
			select {
			case <-ctx.Done():
			case <-time.After(150 * time.Millisecond):
			}
		}
	}
	summary := Measurement{Kind: "latency", Name: "Median", Target: target, Samples: len(samples), Failures: failures}
	if len(samples) > 0 {
		for i := 1; i < len(samples); i++ {
			summary.Jitter += math.Abs(samples[i] - samples[i-1])
		}
		if len(samples) > 1 {
			summary.Jitter /= float64(len(samples) - 1)
		}
		sort.Float64s(samples)
		summary.Value = median(samples)
		summary.Min = samples[0]
		summary.Max = samples[len(samples)-1]
		summary.P95 = samples[int(math.Ceil(float64(len(samples))*.95))-1]
	} else {
		summary.Error = "no successful HTTPS samples"
	}
	ms = append(ms, summary)
	reportStage(report, "latency", 0, len(ms)-1, 7)
	return ms
}
func geography(ctx context.Context, c *http.Client) []Measurement {
	result := []Measurement{}
	ips := map[string]bool{}
	for _, entry := range []struct{ name, url string }{{"Cloudflare", "https://www.cloudflare.com/cdn-cgi/trace"}, {"ipify", "https://api.ipify.org?format=json"}} {
		m := Measurement{Kind: "observer", Name: entry.name, Target: entry.url}
		b, e := request(ctx, c, entry.url)
		if e != nil {
			m.Error = e.Error()
		} else if entry.name == "Cloudflare" {
			for _, line := range strings.Split(string(b), "\n") {
				kv := strings.SplitN(line, "=", 2)
				if len(kv) == 2 {
					switch kv[0] {
					case "ip":
						m.IP = kv[1]
					case "loc":
						m.Country = kv[1]
					}
				}
			}
		} else {
			var data struct {
				IP string `json:"ip"`
			}
			if json.Unmarshal(b, &data) == nil {
				m.IP = data.IP
			}
		}
		if m.Error == "" {
			if net.ParseIP(m.IP) == nil {
				m.Error = "invalid observer response"
			} else {
				ips[m.IP] = true
			}
		}
		result = append(result, m)
	}
	ordered := []string{}
	for ip := range ips {
		ordered = append(ordered, ip)
	}
	sort.Strings(ordered)
	for _, ip := range ordered {
		for _, provider := range []string{"ipwho.is", "ipapi.co"} {
			target := "https://ipwho.is/" + url.PathEscape(ip)
			if provider == "ipapi.co" {
				target = "https://ipapi.co/" + url.PathEscape(ip) + "/json/"
			}
			m := Measurement{Kind: "database", Name: provider, Target: target, IP: ip}
			b, e := request(ctx, c, target)
			if e != nil {
				m.Error = e.Error()
			} else {
				var data map[string]any
				if json.Unmarshal(b, &data) != nil {
					m.Error = "invalid geolocation response"
				} else if data["success"] == false || data["error"] == true {
					m.Error = "geolocation provider rejected the request"
				} else {
					m.Country = str(data["country_code"])
					m.Region = str(data["city"])
					if provider == "ipwho.is" {
						conn := obj(data["connection"])
						m.Organization = str(conn["org"])
						if m.Organization == "" {
							m.Organization = str(conn["isp"])
						}
						if n, ok := conn["asn"].(float64); ok {
							m.ASN = "AS" + strconv.FormatFloat(n, 'f', 0, 64)
						}
					} else {
						m.Organization = str(data["org"])
						m.ASN = str(data["asn"])
					}
					if m.Country == "" {
						m.Error = "country is missing in provider response"
					}
				}
			}
			result = append(result, m)
			if ctx.Err() != nil {
				return result
			}
		}
	}
	return result
}
func runDiagnostic(ctx context.Context, core, root string, p Profile, kind string) TestResult {
	return runDiagnosticWithProgress(ctx, core, root, p, kind, nil)
}
func runDiagnosticWithProgress(ctx context.Context, core, root string, p Profile, kind string, report progressReporter) (result TestResult) {
	reportStage(report, "preparing", 0, 0, 0)
	result = TestResult{ProfileID: p.ID, Kind: kind, At: time.Now().UTC(), Measurements: []Measurement{}}
	defer func() {
		if ctx.Err() != nil {
			result.Error = "diagnostic canceled or timed out"
		}
	}()
	if kind != "latency" && kind != "speed" && kind != "geo" {
		result.Error = "unknown diagnostic"
		return
	}
	port, e := freePort()
	if e != nil {
		result.Error = "cannot allocate a test port"
		return
	}
	proxy := ProxySettings{Mode: "mixed", SOCKSPort: port, Auth: true, Username: "probe", Password: token()}
	cfg, e := CompileProfile(p, proxy)
	if e != nil {
		result.Error = e.Error()
		return
	}
	if e = validateCore(ctx, core, cfg, root); e != nil {
		result.Error = e.Error()
		return
	}
	proc, e := startCore(ctx, core, cfg, root, port)
	if e != nil {
		result.Error = e.Error()
		return
	}
	defer proc.stop()
	client := proxyClient(proxy)
	defer client.CloseIdleConnections()
	switch kind {
	case "latency":
		result.Measurements = latencyWithProgress(ctx, client, report)
	case "speed":
		result.Measurements = speedWithProgress(ctx, client, report)
	case "geo":
		reportStage(report, "geo", 0, 0, 0)
		result.Measurements = geography(ctx, client)
	}
	if ctx.Err() != nil {
		result.Error = "diagnostic canceled or timed out"
	}
	return
}

func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}
