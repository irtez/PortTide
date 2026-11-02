package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func CompileProfile(p Profile, proxy ProxySettings) ([]byte, error) {
	if e := proxy.Validate(); e != nil {
		return nil, e
	}
	m, e := object(p.Config)
	if e != nil {
		return nil, e
	}
	outbounds := list(m["outbounds"])
	tags := map[string]string{}
	proxyCount := 0
	allowed := map[string]bool{"vless": true, "vmess": true, "trojan": true, "shadowsocks": true, "socks": true, "http": true, "hysteria": true, "freedom": true, "blackhole": true, "dns": true}
	for _, v := range outbounds {
		o := obj(v)
		tag, protocol := str(o["tag"]), str(o["protocol"])
		if tag == "" || tags[tag] != "" {
			return nil, errors.New("every outbound must have a unique nonempty tag")
		}
		tags[tag] = protocol
		if !allowed[protocol] {
			return nil, fmt.Errorf("unsupported outbound protocol: %s", protocol)
		}
		if protocol != "freedom" && protocol != "blackhole" && protocol != "dns" {
			proxyCount++
		}
		stream := obj(o["streamSettings"])
		for _, key := range []string{"tlsSettings", "realitySettings"} {
			if obj(stream[key])["allowInsecure"] == true {
				return nil, errors.New("profile disables TLS certificate verification")
			}
		}
	}
	if proxyCount == 0 {
		return nil, errors.New("this entry does not contain a supported proxy")
	}
	isProxy := func(tag string) bool {
		p, ok := tags[tag]
		return ok && p != "freedom" && p != "blackhole" && p != "dns"
	}
	routing := obj(m["routing"])
	balancers := list(routing["balancers"])
	knownBalancers := map[string]bool{}
	for _, v := range balancers {
		b := obj(v)
		tag := str(b["tag"])
		if tag == "" || knownBalancers[tag] {
			return nil, errors.New("invalid balancer tag")
		}
		knownBalancers[tag] = true
		selectors := list(b["selector"])
		if len(selectors) == 0 {
			return nil, errors.New("balancer has no selectors")
		}
		for _, sel := range selectors {
			prefix := str(sel)
			matched := 0
			if prefix == "" {
				return nil, errors.New("empty balancer selector")
			}
			for t := range tags {
				if strings.HasPrefix(t, prefix) {
					matched++
					if !isProxy(t) {
						return nil, errors.New("balancer selector includes a direct or non-proxy outbound")
					}
				}
			}
			if matched == 0 {
				return nil, errors.New("balancer selector does not match a proxy")
			}
		}
		if fallback := str(b["fallbackTag"]); fallback != "" && !isProxy(fallback) {
			return nil, errors.New("balancer fallback must be a proxy")
		}
	}
	route := map[string]any{"type": "field", "network": "tcp,udp"}
	found := false
	rules := list(routing["rules"])
	for i := len(rules) - 1; i >= 0; i-- {
		rule := obj(rules[i])
		broad := true
		for k := range rule {
			if k != "type" && k != "network" && k != "outboundTag" && k != "balancerTag" {
				broad = false
			}
		}
		if !broad {
			continue
		}
		network := str(rule["network"])
		if network != "" && network != "tcp,udp" && network != "udp,tcp" {
			continue
		}
		if tag := str(rule["balancerTag"]); tag != "" {
			if !knownBalancers[tag] {
				return nil, errors.New("final route refers to an unknown balancer")
			}
			route["balancerTag"] = tag
			found = true
			break
		}
		if tag := str(rule["outboundTag"]); tag != "" {
			if !isProxy(tag) {
				return nil, errors.New("final profile route is not a proxy")
			}
			route["outboundTag"] = tag
			found = true
			break
		}
	}
	if !found {
		first := str(obj(outbounds[0])["tag"])
		if !isProxy(first) {
			return nil, errors.New("cannot identify an unambiguous default proxy route")
		}
		route["outboundTag"] = first
	}
	// Only internal tagged DNS may use the provider's direct bootstrap rules.
	directTag := ""
	for _, o := range outbounds {
		v := obj(o)
		if str(v["protocol"]) == "freedom" {
			directTag = str(v["tag"])
			break
		}
	}
	if directTag == "" {
		directTag = "porttide-dns-direct"
		if tags[directTag] != "" {
			return nil, errors.New("reserved outbound tag collision")
		}
		outbounds = append(outbounds, map[string]any{"tag": directTag, "protocol": "freedom"})
	}
	safeRules := []any{}
	dns := obj(m["dns"])
	if dns != nil {
		dns["tag"] = "porttide-internal-dns"
		for _, rv := range rules {
			v := obj(rv)
			if str(v["outboundTag"]) == directTag && str(v["port"]) == "53" {
				copy := map[string]any{}
				for k, x := range v {
					copy[k] = x
				}
				copy["inboundTag"] = []string{"porttide-internal-dns"}
				safeRules = append(safeRules, copy)
			}
		}
	}
	safeRules = append(safeRules, route)
	accounts := []any{}
	if proxy.Auth {
		accounts = append(accounts, map[string]any{"user": proxy.Username, "pass": proxy.Password})
	}
	socks := map[string]any{"auth": "noauth", "udp": proxy.UDP, "ip": "127.0.0.1"}
	if proxy.Auth {
		socks["auth"] = "password"
		socks["accounts"] = accounts
	}
	inbounds := []any{map[string]any{"tag": "porttide-socks", "listen": "127.0.0.1", "port": proxy.SOCKSPort, "protocol": "socks", "settings": socks}}
	if proxy.Mode == "separate" {
		httpSettings := map[string]any{"allowTransparent": false}
		if proxy.Auth {
			httpSettings["accounts"] = accounts
		}
		inbounds = append(inbounds, map[string]any{"tag": "porttide-http", "listen": "127.0.0.1", "port": proxy.HTTPPort, "protocol": "http", "settings": httpSettings})
	}
	result := map[string]any{"log": map[string]any{"loglevel": "none", "access": "none"}, "inbounds": inbounds, "outbounds": outbounds, "routing": map[string]any{"domainStrategy": "AsIs", "balancers": balancers, "rules": safeRules}}
	if dns != nil {
		result["dns"] = dns
	}
	for _, k := range []string{"observatory", "burstObservatory", "policy"} {
		if m[k] != nil {
			result[k] = m[k]
		}
	}
	return json.MarshalIndent(result, "", "  ")
}
