package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Measurement struct {
	Kind         string  `json:"kind"`
	Name         string  `json:"name"`
	Target       string  `json:"target"`
	IP           string  `json:"ip,omitempty"`
	Country      string  `json:"country,omitempty"`
	Region       string  `json:"region,omitempty"`
	Organization string  `json:"organization,omitempty"`
	ASN          string  `json:"asn,omitempty"`
	Value        float64 `json:"value,omitempty"`
	Bytes        int64   `json:"bytes,omitempty"`
	Seconds      float64 `json:"seconds,omitempty"`
	Partial      bool    `json:"partial,omitempty"`
	Error        string  `json:"error,omitempty"`
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
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u), DisableKeepAlives: true, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 12 * time.Second}, Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
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
	target := "https://www.gstatic.com/generate_204"
	samples := []float64{}
	ms := []Measurement{}
	for i := 0; i < 3; i++ {
		start := time.Now()
		_, e := request(ctx, c, target)
		m := Measurement{Kind: "latency", Name: fmt.Sprintf("HTTPS sample %d", i+1), Target: target}
		if e != nil {
			m.Error = e.Error()
		} else {
			m.Value = float64(time.Since(start).Microseconds()) / 1000
			samples = append(samples, m.Value)
		}
		ms = append(ms, m)
		if ctx.Err() != nil {
			break
		}
	}
	if len(samples) > 0 {
		sort.Float64s(samples)
		ms = append(ms, Measurement{Kind: "latency", Name: "Median", Target: target, Value: median(samples)})
	}
	return ms
}

type countingReader struct {
	reader io.Reader
	bytes  atomic.Int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, e := r.reader.Read(p)
	r.bytes.Add(int64(n))
	return n, e
}
func speed(ctx context.Context, c *http.Client) []Measurement {
	result := []Measurement{}
	for _, upload := range []bool{false, true} {
		method, target, kind := "GET", "https://speed.cloudflare.com/__down?bytes=20000000", "download"
		var body io.Reader
		var counter *countingReader
		if upload {
			method, target, kind = "POST", "https://speed.cloudflare.com/__up", "upload"
			counter = &countingReader{reader: bytes.NewReader(make([]byte, 4<<20))}
			body = counter
		}
		testCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		req, _ := http.NewRequestWithContext(testCtx, method, target, body)
		if upload {
			req.Header.Set("Content-Type", "application/octet-stream")
			req.ContentLength = 4 << 20
		}
		start := time.Now()
		resp, e := c.Do(req)
		m := Measurement{Kind: kind, Name: "Cloudflare", Target: target}
		if e != nil {
			m.Error = "test failed or timed out"
		} else {
			if resp.StatusCode != 200 {
				m.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
			} else {
				limit := int64(20_000_000)
				if upload {
					limit = 65536
				}
				n, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, limit+1))
				m.Bytes = n
				if upload {
					m.Bytes = counter.bytes.Load()
				}
				m.Partial = !upload && m.Bytes > 0 && testCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil
				if readErr != nil && !m.Partial {
					m.Error = "transfer interrupted; result is incomplete"
				} else if n > limit {
					m.Error = "response exceeded the test limit"
				} else if m.Bytes == 0 || (upload && m.Bytes != 4<<20) {
					m.Error = "server did not accept the complete transfer"
				} else {
					m.Seconds = time.Since(start).Seconds()
					m.Value = float64(m.Bytes) * 8 / m.Seconds / 1e6
				}
			}
			resp.Body.Close()
		}
		cancel()
		result = append(result, m)
		if ctx.Err() != nil {
			break
		}
	}
	return result
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
func runDiagnostic(ctx context.Context, core, root string, p Profile, kind string) (result TestResult) {
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
		result.Measurements = latency(ctx, client)
	case "speed":
		result.Measurements = speed(ctx, client)
	case "geo":
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
