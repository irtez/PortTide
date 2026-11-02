package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestVersionOrdering(t *testing.T) {
	for _, tc := range [][2]string{{"0.1.0-alpha.10", "0.1.0-alpha.2"}, {"0.1.0", "0.1.0-rc.1"}, {"0.2.0-alpha.1", "0.1.9"}, {"v1.0.0", "0.9.9"}, {"0.1.0-beta", "0.1.0-alpha"}} {
		if compareVersions(tc[0], tc[1]) != 1 || compareVersions(tc[1], tc[0]) != -1 {
			t.Fatal(tc)
		}
	}
	if compareVersions("0.1.0", "v0.1.0") != 0 {
		t.Fatal("version normalization")
	}
}
func TestReleaseSelectionAndAssetTrust(t *testing.T) {
	good := release{Tag: "v0.2.0", Assets: []releaseAsset{{Name: "PortTide-0.2.0-windows-x64-setup.exe", URL: releaseRoot + "download/v0.2.0/PortTide-0.2.0-windows-x64-setup.exe", Digest: "sha256:" + strings.Repeat("a", 64), Size: 100}}}
	rs := []release{{Tag: "v9.0.0", Draft: true}, {Tag: "v0.3.0-beta.1", Prerelease: true}, good, {Tag: "v0.1.1"}}
	u := chooseRelease(rs, "0.1.0")
	if u.Version != "0.2.0" || u.Asset.URL == "" {
		t.Fatal(u)
	}
	u = chooseRelease(rs, "0.1.0-alpha.2")
	if u.Version != "0.3.0-beta.1" {
		t.Fatal("alpha channel did not see prerelease")
	}
	good.Assets[0].URL = "https://evil.example/installer.exe"
	u = chooseRelease([]release{good}, "0.1.0")
	if u.Asset.URL != "" {
		t.Fatal("untrusted download URL accepted")
	}
	good.Assets[0].URL = releaseRoot + "download/v0.2.0/PortTide-0.2.0-windows-x64-setup.exe"
	good.Assets[0].Digest = ""
	u = chooseRelease([]release{good}, "0.1.0")
	if u.Asset.URL != "" {
		t.Fatal("missing checksum accepted")
	}
}
func TestInstallerDownloadValidation(t *testing.T) {
	body := []byte("test installer content")
	hash := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(hash[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	for _, tc := range []struct {
		digest string
		size   int64
		ok     bool
	}{{digest, int64(len(body)), true}, {"sha256:" + strings.Repeat("0", 64), int64(len(body)), false}, {digest, int64(len(body) + 1), false}} {
		dir := t.TempDir()
		path, err := downloadInstaller(context.Background(), srv.Client(), releaseAsset{URL: srv.URL, Digest: tc.digest, Size: tc.size}, dir)
		if (err == nil) != tc.ok {
			t.Fatal(tc, err)
		}
		files, _ := os.ReadDir(dir)
		if !tc.ok && len(files) != 0 {
			t.Fatal("invalid executable left behind")
		}
		if tc.ok {
			b, _ := os.ReadFile(path)
			if string(b) != string(body) {
				t.Fatal("download changed")
			}
		}
	}
}
func TestUpdateHTTPFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer srv.Close()
	if _, err := fetchUpdates(context.Background(), srv.Client(), srv.URL, Version); err == nil {
		t.Fatal("rate limit hidden")
	}
}
