package app

import (
	"encoding/json"
	"strings"
	"testing"
)

const fixture = `{"remarks":"🇳🇱 Netherlands 🤖","inbounds":[{"listen":"0.0.0.0","port":1080,"protocol":"socks"}],"outbounds":[{"tag":"node-a","protocol":"vless","settings":{"vnext":[{"address":"example.invalid","port":443,"users":[{"id":"00000000-0000-4000-8000-000000000001","encryption":"none"}]}]},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"example.invalid"}}},{"tag":"direct","protocol":"freedom"}],"routing":{"rules":[{"type":"field","domain":["example.org"],"outboundTag":"direct"},{"type":"field","network":"tcp,udp","outboundTag":"node-a"}]}}`

func TestCatalogStableIdentityAndUnicode(t *testing.T) {
	a, err := ParseProfiles("source-1", []byte("["+fixture+","+strings.ReplaceAll(fixture, "Netherlands", "Finland")+"]"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseProfiles("source-1", []byte("["+strings.ReplaceAll(fixture, "Netherlands", "Finland")+","+fixture+"]"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 || a[0].ID != b[1].ID || a[0].Name != "🇳🇱 Netherlands 🤖" || !a[0].Supported {
		t.Fatalf("unexpected catalog: %+v", a)
	}
}
func TestCatalogRejectsUnsupportedEnvelope(t *testing.T) {
	for _, v := range []string{"not json", "[]", "{}", "{\"outbounds\":[]}", "null", fixture + " trailing"} {
		if _, e := ParseProfiles("s", []byte(v)); e == nil {
			t.Errorf("accepted %s", v)
		}
	}
}
func TestDuplicateNamesRemainDistinctAndStable(t *testing.T) {
	other := strings.ReplaceAll(fixture, "example.invalid", "other.invalid")
	a, _ := ParseProfiles("s", []byte("["+fixture+","+other+"]"))
	b, _ := ParseProfiles("s", []byte("["+other+","+fixture+"]"))
	if a[0].ID == a[1].ID || a[0].ID != b[1].ID {
		t.Fatal("duplicate identity is order dependent")
	}
}
func TestCompilerOwnsListenersAndRemovesUserDirectRules(t *testing.T) {
	p, _ := ParseProfiles("s", []byte(fixture))
	cfg, err := CompileProfile(p[0], ProxySettings{Mode: "mixed", SOCKSPort: 17890, HTTPPort: 17891, Auth: true, Username: "test", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	json.Unmarshal(cfg, &v)
	ins := v["inbounds"].([]any)
	in := ins[0].(map[string]any)
	if len(ins) != 1 || in["listen"] != "127.0.0.1" || in["port"] != float64(17890) {
		t.Fatalf("bad listeners %s", cfg)
	}
	settings := in["settings"].(map[string]any)
	if settings["auth"] != "password" {
		t.Fatal("authentication missing")
	}
	rules := v["routing"].(map[string]any)["rules"].([]any)
	for _, raw := range rules {
		rule := raw.(map[string]any)
		if rule["outboundTag"] == "direct" && rule["inboundTag"] == nil {
			t.Fatal("unscoped direct route")
		}
	}
	last := rules[len(rules)-1].(map[string]any)
	if last["outboundTag"] != "node-a" {
		t.Fatal("lost chosen route")
	}
}
func TestCompilerRejectsDirectFallbackAndUnsafeTLS(t *testing.T) {
	raw := strings.Replace(fixture, `"rules":[`, `"balancers":[{"tag":"auto","selector":["node"],"fallbackTag":"direct"}],"rules":[`, 1)
	p, err := ParseProfiles("s", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if p[0].Supported {
		t.Fatal("direct fallback accepted")
	}
	raw = strings.Replace(fixture, `"serverName":"example.invalid"`, `"serverName":"example.invalid","allowInsecure":true`, 1)
	p, _ = ParseProfiles("s", []byte(raw))
	if p[0].Supported {
		t.Fatal("insecure TLS accepted")
	}
}
func TestProxySettingsValidatePortsAndCredentials(t *testing.T) {
	for _, p := range []ProxySettings{{Mode: "mixed", SOCKSPort: 0}, {Mode: "separate", SOCKSPort: 17890, HTTPPort: 17890}, {Mode: "mixed", SOCKSPort: 17890, Auth: true}, {Mode: "bogus", SOCKSPort: 17890}} {
		if p.Validate() == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
}
