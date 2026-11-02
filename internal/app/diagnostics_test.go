package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestGeographySeparatesObserversAndDatabases(t *testing.T) {
	requests := []string{}
	c := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.String())
		switch r.URL.Host {
		case "www.cloudflare.com":
			return response("ip=203.0.113.10\nloc=FI\n"), nil
		case "api.ipify.org":
			return response(`{"ip":"198.51.100.20"}`), nil
		case "ipwho.is":
			return response(`{"success":true,"country_code":"FI","city":"Helsinki","connection":{"asn":64512,"org":"Fixture ISP"}}`), nil
		case "ipapi.co":
			return response(`{"country_code":"DE","city":"Berlin","asn":"AS64512","org":"Fixture ISP"}`), nil
		}
		return nil, fmt.Errorf("unexpected host")
	})}
	ms := geography(context.Background(), c)
	if len(ms) != 6 {
		t.Fatalf("want two observers and four explicit lookups, got %d", len(ms))
	}
	for _, m := range ms {
		if m.Error != "" {
			t.Fatal(m.Error)
		}
		if m.Kind == "database" && !strings.Contains(m.Target, m.IP) {
			t.Fatal("lookup has no explicit IP")
		}
	}
	if ms[2].Country == ms[3].Country {
		t.Fatal("conflicting providers were silently merged")
	}
}
func TestSpeedCountsActualUploadedBytesAndRejectsIncompleteResponse(t *testing.T) {
	var upload int64
	c := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			upload, _ = io.Copy(io.Discard, r.Body)
			return response("ok"), nil
		}
		return response(strings.Repeat("a", 100000)), nil
	})}
	m := speed(context.Background(), c)
	if m[0].Bytes != 100000 || m[1].Bytes != 4<<20 || upload != 4<<20 || m[0].Error != "" || m[1].Error != "" {
		t.Fatalf("bad counters: %+v", m)
	}
	c.Transport = transportFunc(func(r *http.Request) (*http.Response, error) { return response(""), nil })
	m = speed(context.Background(), c)
	if m[0].Error == "" || m[1].Error == "" {
		t.Fatal("empty download or unread upload reported as speed")
	}
}
func TestSpeedRefusesOversizedDownload(t *testing.T) {
	c := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return response(strings.Repeat("x", 20_000_001)), nil
		}
		io.Copy(io.Discard, r.Body)
		return response("ok"), nil
	})}
	ms := speed(context.Background(), c)
	if ms[0].Error == "" || ms[0].Value != 0 {
		t.Fatal("oversized response accepted")
	}
}
func TestProxyClientDoesNotFallBackToDirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("request escaped directly") }))
	defer target.Close()
	port, _ := freePort()
	c := proxyClient(ProxySettings{SOCKSPort: port})
	defer c.CloseIdleConnections()
	if _, e := c.Get(target.URL); e == nil {
		t.Fatal("closed proxy did not fail")
	}
}
func TestGeographyProviderErrorsAreVisible(t *testing.T) {
	c := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("rate limit")), Header: make(http.Header)}, nil
	})}
	ms := geography(context.Background(), c)
	if len(ms) != 2 || ms[0].Error != "HTTP 429" || ms[1].Error != "HTTP 429" {
		t.Fatalf("errors lost: %+v", ms)
	}
}
