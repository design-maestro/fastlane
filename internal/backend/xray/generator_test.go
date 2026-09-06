package xray

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
)

func TestFormatDNSServersSupportsPlainAndDoH(t *testing.T) {
	t.Parallel()

	plain, err := formatDNSServers([]string{" 1.1.1.1 ", ""}, domain.DNSTransportPlain)
	if err != nil {
		t.Fatalf("format plain dns servers: %v", err)
	}
	if !reflect.DeepEqual(plain, []string{"1.1.1.1"}) {
		t.Fatalf("unexpected plain dns servers: %+v", plain)
	}

	doh, err := formatDNSServers([]string{"dns.google", "https://dns.quad9.net/dns-query"}, domain.DNSTransportDoH)
	if err != nil {
		t.Fatalf("format doh dns servers: %v", err)
	}
	if !reflect.DeepEqual(doh, []string{"https://dns.google/dns-query", "https://dns.quad9.net/dns-query"}) {
		t.Fatalf("unexpected doh dns servers: %+v", doh)
	}
}

func TestDNSBootstrapDomainsAndDirectDestinations(t *testing.T) {
	t.Parallel()

	servers := []string{
		"https://dns.google/dns-query",
		"https://dns.google/dns-query",
		"1.1.1.1",
		"https://1.0.0.1/dns-query",
		"https://dns.quad9.net/dns-query",
	}

	if got := dnsBootstrapDomains(servers); !reflect.DeepEqual(got, []string{"full:dns.google", "full:dns.quad9.net"}) {
		t.Fatalf("unexpected bootstrap domains: %+v", got)
	}

	ips, domains := directDNSDestinations(servers)
	if !reflect.DeepEqual(ips, []string{"1.1.1.1", "1.0.0.1"}) {
		t.Fatalf("unexpected direct dns ips: %+v", ips)
	}
	if !reflect.DeepEqual(domains, []string{"full:dns.google", "full:dns.quad9.net"}) {
		t.Fatalf("unexpected direct dns domains: %+v", domains)
	}
}

func TestCountryDirectRulesUseVerifiedGeoSiteAndGeoIPBeforeVPNFallback(t *testing.T) {
	t.Parallel()

	rules := transparentRoutingRules(backend.ConfigRequest{
		TransparentProxy: true,
		TransparentCountryRouting: domain.CountryRouting{
			Enabled: true, CountryCode: "RU",
		},
		TransparentDefaultAction: domain.FirewallDefaultActionProxy,
	})
	if len(rules) != 6 {
		t.Fatalf("expected four Russia rules and two VPN fallback rules, got %d", len(rules))
	}
	if !reflect.DeepEqual(rules[0].Domain, []string{"geosite:category-ru"}) || rules[0].OutboundTag != "direct" {
		t.Fatalf("unexpected Russia domain rule: %+v", rules[0])
	}
	if !reflect.DeepEqual(rules[2].IP, []string{"geoip:ru"}) || rules[2].OutboundTag != "direct" {
		t.Fatalf("unexpected Russia IP rule: %+v", rules[2])
	}
	if rules[4].OutboundTag != "selected" || rules[5].OutboundTag != "selected" {
		t.Fatalf("expected VPN fallback after Russia rules: %+v", rules[4:])
	}
}

func TestCountryDirectRulesSupportAnyKnownCountryThroughGeoIP(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"DE", "IR", "ZA", "NZ"} {
		rules := transparentRoutingRules(backend.ConfigRequest{
			TransparentProxy:          true,
			TransparentCountryRouting: domain.CountryRouting{Enabled: true, CountryCode: code},
			TransparentDefaultAction:  domain.FirewallDefaultActionProxy,
		})
		if len(rules) != 4 {
			t.Fatalf("%s: expected two GeoIP rules and two VPN fallbacks, got %d", code, len(rules))
		}
		want := []string{"geoip:" + strings.ToLower(code)}
		if !reflect.DeepEqual(rules[0].IP, want) || rules[0].OutboundTag != "direct" {
			t.Fatalf("%s: unexpected country rule: %+v", code, rules[0])
		}
	}
}

func TestOutboundForNodeSupportsVMessAndTrojan(t *testing.T) {
	t.Parallel()

	xhttpOutbound, err := outboundForNode(domain.Node{
		Protocol:   domain.ProtocolVLESS,
		Address:    "xhttp.example.com",
		Port:       443,
		UUID:       "11111111-1111-1111-1111-111111111111",
		Security:   "reality",
		ServerName: "edge.example.com",
		Transport:  "xhttp",
		Path:       "/xhttp-path",
		Host:       "cdn.example.com",
		RawQuery:   "mode=stream-one&concurrency=4",
	})
	if err != nil {
		t.Fatalf("xhttp outbound: %v", err)
	}
	if xhttpOutbound.Protocol != "vless" {
		t.Fatalf("unexpected xhttp outbound protocol: %q", xhttpOutbound.Protocol)
	}
	streamSettings, ok := xhttpOutbound.StreamSettings.(map[string]any)
	if !ok {
		t.Fatalf("expected stream settings map, got %T", xhttpOutbound.StreamSettings)
	}
	if streamSettings["network"] != "xhttp" {
		t.Fatalf("unexpected network: %v", streamSettings["network"])
	}
	xhttpSettings, ok := streamSettings["xhttpSettings"].(map[string]any)
	if !ok {
		t.Fatalf("expected xhttpSettings map, got %T", streamSettings["xhttpSettings"])
	}
	if xhttpSettings["path"] != "/xhttp-path" || xhttpSettings["host"] != "cdn.example.com" {
		t.Fatalf("unexpected xhttpSettings: %+v", xhttpSettings)
	}
	extra := xhttpSettings["extra"].(map[string]any)
	xmux := extra["xmux"].(map[string]any)
	if xmux["maxConcurrency"] != 4 || xhttpOutbound.Mux != nil {
		t.Fatalf("expected XHTTP XMUX concurrency without legacy Xray mux: %+v", xhttpOutbound)
	}

	vmessOutbound, err := outboundForNode(domain.Node{
		Protocol:   domain.ProtocolVMess,
		Address:    "vmess.example.com",
		Port:       443,
		UUID:       "11111111-1111-1111-1111-111111111111",
		Encryption: "aes-128-gcm",
		Security:   "tls",
		ServerName: "edge.example.com",
		Transport:  "grpc",
		Path:       "service",
	})
	if err != nil {
		t.Fatalf("vmess outbound: %v", err)
	}
	if vmessOutbound.Protocol != "vmess" {
		t.Fatalf("unexpected vmess outbound protocol: %q", vmessOutbound.Protocol)
	}

	trojanOutbound, err := outboundForNode(domain.Node{
		Protocol:   domain.ProtocolTrojan,
		Address:    "trojan.example.com",
		Port:       443,
		Password:   "secret",
		Security:   "tls",
		ServerName: "trojan.example.com",
		Transport:  "ws",
		Path:       "/trojan",
		Host:       "cdn.example.com",
	})
	if err != nil {
		t.Fatalf("trojan outbound: %v", err)
	}
	if trojanOutbound.Protocol != "trojan" {
		t.Fatalf("unexpected trojan outbound protocol: %q", trojanOutbound.Protocol)
	}

	ssOutbound, err := outboundForNode(domain.Node{
		Protocol:   domain.ProtocolShadowsocks,
		Address:    "ss.example.com",
		Port:       8388,
		Password:   "pwd",
		Encryption: "aes-256-gcm",
	})
	if err != nil {
		t.Fatalf("shadowsocks outbound: %v", err)
	}
	if ssOutbound.Protocol != "shadowsocks" {
		t.Fatalf("unexpected shadowsocks protocol: %q", ssOutbound.Protocol)
	}

	socksOutbound, err := outboundForNode(domain.Node{
		Protocol: domain.ProtocolSocks,
		Address:  "socks.example.com",
		Port:     1080,
		UUID:     "user",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("socks outbound: %v", err)
	}
	if socksOutbound.Protocol != "socks" {
		t.Fatalf("unexpected socks protocol: %q", socksOutbound.Protocol)
	}

	hyOutbound, err := outboundForNode(domain.Node{
		Protocol:   domain.ProtocolHysteria,
		Address:    "hy.example.com",
		Port:       443,
		Password:   "auth_token",
		ServerName: "sni.example.com",
	})
	if err != nil {
		t.Fatalf("hysteria outbound: %v", err)
	}
	if hyOutbound.Protocol != "hysteria" {
		t.Fatalf("unexpected hysteria protocol: %q", hyOutbound.Protocol)
	}

	hy2Outbound, err := outboundForNode(domain.Node{
		Protocol:   domain.ProtocolHysteria2,
		Address:    "hy2.example.com",
		Port:       8443,
		Password:   "pwd_token",
		ServerName: "sni.example.com",
		Extras: map[string]string{
			"obfs":          "salamander",
			"obfs-password": "obfspass",
		},
	})
	if err != nil {
		t.Fatalf("hysteria2 outbound: %v", err)
	}
	if hy2Outbound.Protocol != "hysteria" {
		t.Fatalf("unexpected hysteria2 outbound protocol: %q", hy2Outbound.Protocol)
	}
	hy2Settings, ok := hy2Outbound.Settings.(map[string]any)
	if !ok || hy2Settings["version"] != 2 {
		t.Fatalf("expected version 2 in hysteria2 settings, got %+v", hy2Outbound.Settings)
	}

	if _, err := outboundForNode(domain.Node{Protocol: "unknown"}); err == nil {
		t.Fatal("expected unsupported protocol to fail")
	}
}

func TestGeneratorPreservesRawXHTTPOutbound(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
  "tag": "provider-tag",
  "protocol": "vless",
  "settings": {"vnext":[{"address":"node.example.com","port":443,"users":[{"id":"11111111-1111-1111-1111-111111111111","encryption":"none"}]}]},
  "streamSettings": {"network":"xhttp","security":"reality","xhttpSettings":{"mode":"packet-up","path":"/api","extra":{"xmux":{"maxConcurrency":8}},"downloadSettings":{"address":"download.example.com"}}}
}`)

	rendered, err := NewGenerator().Generate(backend.ConfigRequest{
		Nodes: []domain.Node{{
			ID:          "node-raw",
			Protocol:    domain.ProtocolVLESS,
			Address:     "node.example.com",
			Port:        443,
			UUID:        "11111111-1111-1111-1111-111111111111",
			RawOutbound: raw,
		}},
		SelectedNodeID: "node-raw",
	})
	if err != nil {
		t.Fatalf("generate config: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(rendered, &cfg); err != nil {
		t.Fatalf("decode generated config: %v", err)
	}
	selected := cfg["outbounds"].([]any)[0].(map[string]any)
	if selected["tag"] != "selected" {
		t.Fatalf("unexpected selected tag: %v", selected["tag"])
	}
	xhttp := selected["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)
	if xhttp["mode"] != "packet-up" || xhttp["downloadSettings"] == nil || xhttp["extra"] == nil {
		t.Fatalf("raw xhttp settings were lost: %+v", xhttp)
	}

	var source map[string]any
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatalf("decode source outbound: %v", err)
	}
	if source["tag"] != "provider-tag" {
		t.Fatalf("source raw outbound was mutated: %v", source["tag"])
	}
}

func TestGeneratorMarksProbeOutboundsWithoutLosingRawTransport(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
  "protocol": "vless",
  "settings": {"vnext":[{"address":"node.example.com","port":443,"users":[{"id":"11111111-1111-1111-1111-111111111111","encryption":"none"}]}]},
  "streamSettings": {"network":"ws","security":"tls","wsSettings":{"path":"/probe"}}
}`)
	rendered, err := NewGenerator().Generate(backend.ConfigRequest{
		Nodes:          []domain.Node{{ID: "node", Protocol: domain.ProtocolVLESS, RawOutbound: raw}},
		SelectedNodeID: "node",
		OutboundMark:   0x100,
	})
	if err != nil {
		t.Fatalf("generate marked probe config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(rendered, &cfg); err != nil {
		t.Fatalf("decode generated config: %v", err)
	}
	outbounds := cfg["outbounds"].([]any)
	for _, index := range []int{0, 1} {
		stream := outbounds[index].(map[string]any)["streamSettings"].(map[string]any)
		sockopt := stream["sockopt"].(map[string]any)
		if sockopt["mark"] != float64(0x100) {
			t.Fatalf("outbound %d has no direct-test mark: %+v", index, sockopt)
		}
	}
	selectedStream := outbounds[0].(map[string]any)["streamSettings"].(map[string]any)
	if selectedStream["wsSettings"].(map[string]any)["path"] != "/probe" {
		t.Fatalf("probe mark overwrote preserved transport: %+v", selectedStream)
	}
}

func TestGeneratorHandlesSplitDNSAndSelectedNodeErrors(t *testing.T) {
	t.Parallel()

	req := backend.ConfigRequest{
		Nodes: []domain.Node{
			{
				ID:          "node-1",
				Protocol:    domain.ProtocolVLESS,
				Address:     "node1.example.com",
				Port:        443,
				UUID:        "11111111-1111-1111-1111-111111111111",
				Encryption:  "none",
				Security:    "reality",
				ServerName:  "edge.example.com",
				Fingerprint: "chrome",
				PublicKey:   "public-key-1",
				ShortID:     "ab12cd34",
			},
		},
		SelectedNodeID: "node-1",
		DNS: domain.DNSSettings{
			Mode:          domain.DNSModeSplit,
			Transport:     domain.DNSTransportDoH,
			Servers:       []string{"dns.google", "1.1.1.1"},
			Bootstrap:     []string{"9.9.9.9"},
			DirectDomains: []string{"domain:lan"},
		},
	}

	rendered, err := NewGenerator().Generate(req)
	if err != nil {
		t.Fatalf("generate config: %v", err)
	}
	if !strings.Contains(string(rendered), "\"dns\"") {
		t.Fatalf("expected dns block in config, got %s", rendered)
	}
	if !strings.Contains(string(rendered), `"domainStrategy": "IPIfNonMatch"`) {
		t.Fatalf("expected Xray DNS to resolve outbound hostnames, got %s", rendered)
	}

	req.SelectedNodeID = "missing-node"
	if _, err := NewGenerator().Generate(req); err == nil {
		t.Fatal("expected missing selected node to fail")
	}
}

func TestGeneratorAddsLocalDNSRuntime(t *testing.T) {
	t.Parallel()

	req := backend.ConfigRequest{
		Nodes: []domain.Node{
			{
				ID:         "node-1",
				Protocol:   domain.ProtocolVLESS,
				Address:    "node1.example.com",
				Port:       443,
				UUID:       "11111111-1111-1111-1111-111111111111",
				Encryption: "none",
				Security:   "reality",
				PublicKey:  "public-key-1",
				ShortID:    "ab12cd34",
			},
		},
		SelectedNodeID: "node-1",
		DNS: domain.DNSSettings{
			Mode:          domain.DNSModeSplit,
			Transport:     domain.DNSTransportDoH,
			Servers:       []string{"1.1.1.1", "1.0.0.1"},
			DirectDomains: []string{"domain:lan"},
		},
		LocalDNSEnabled: true,
		LocalDNSListen:  "127.0.0.1",
		LocalDNSPort:    1053,
	}

	rendered, err := NewGenerator().Generate(req)
	if err != nil {
		t.Fatalf("generate config: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(rendered, &cfg); err != nil {
		t.Fatalf("unmarshal generated json: %v", err)
	}

	inbounds, ok := cfg["inbounds"].([]any)
	if !ok {
		t.Fatalf("inbounds missing: %+v", cfg)
	}
	dnsInbound := findInboundByTag(t, inbounds, "dns-in")
	if dnsInbound["listen"] != "127.0.0.1" {
		t.Fatalf("unexpected dns-in listen: %+v", dnsInbound)
	}
	if dnsInbound["port"] != float64(1053) {
		t.Fatalf("unexpected dns-in port: %+v", dnsInbound)
	}
	settings, ok := dnsInbound["settings"].(map[string]any)
	if !ok {
		t.Fatalf("dns-in settings missing: %+v", dnsInbound)
	}
	if settings["network"] != "tcp,udp" || settings["address"] != "1.1.1.1" || settings["port"] != float64(53) {
		t.Fatalf("unexpected dns-in settings: %+v", settings)
	}

	outbounds, ok := cfg["outbounds"].([]any)
	if !ok {
		t.Fatalf("outbounds missing: %+v", cfg)
	}
	var dnsOutbound map[string]any
	for _, raw := range outbounds {
		candidate, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if candidate["tag"] == "dns-out" {
			dnsOutbound = candidate
			break
		}
	}
	if dnsOutbound == nil {
		t.Fatalf("dns-out missing: %+v", outbounds)
	}
	if dnsOutbound["protocol"] != "dns" {
		t.Fatalf("unexpected dns-out protocol: %+v", dnsOutbound)
	}
	dnsSettings, ok := dnsOutbound["settings"].(map[string]any)
	if !ok {
		t.Fatalf("dns-out settings missing: %+v", dnsOutbound)
	}
	if dnsSettings["nonIPQuery"] != "skip" {
		t.Fatalf("unexpected dns-out settings: %+v", dnsSettings)
	}

	routing, ok := cfg["routing"].(map[string]any)
	if !ok {
		t.Fatalf("routing section missing: %+v", cfg)
	}
	rules, ok := routing["rules"].([]any)
	if !ok || len(rules) < 3 {
		t.Fatalf("routing rules missing: %+v", routing)
	}
	firstRule, ok := rules[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first routing rule object, got %T", rules[0])
	}
	if firstRule["outboundTag"] != "dns-out" {
		t.Fatalf("expected first routing rule to send dns-in to dns-out, got %+v", firstRule)
	}
	if !reflect.DeepEqual(asStringSlice(t, firstRule["inboundTag"]), []string{"dns-in"}) {
		t.Fatalf("unexpected dns-in inbound tag: %+v", firstRule["inboundTag"])
	}
}

func TestInitdControllerCommandsAndRuntimeBackendDelegation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	scriptPath := writeExecutable(t, filepath.Join(dir, "xray-service.sh"), "#!/bin/sh\nprintf '%s\n' \"$1\" >> \""+logPath+"\"\nif [ \"$1\" = \"status\" ]; then\n  echo running\nfi\nexit 0\n")

	controller := InitdController{ScriptPath: scriptPath}
	if err := controller.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := controller.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := controller.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, err := controller.Status(context.Background()); err != nil {
		t.Fatalf("status: %v", err)
	}

	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calls log: %v", err)
	}
	for _, want := range []string{"start", "stop", "reload", "status"} {
		if !strings.Contains(string(calls), want) {
			t.Fatalf("expected controller calls to contain %q, got %q", want, calls)
		}
	}

	tracker := &lifecycleController{status: backend.RuntimeStatus{Running: true, ServiceState: "running"}}
	runtimeBackend := NewRuntimeBackend(filepath.Join(dir, "config.json"), tracker).WithLogger(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := runtimeBackend.Start(context.Background()); err != nil {
		t.Fatalf("runtime start: %v", err)
	}
	if err := runtimeBackend.Stop(context.Background()); err != nil {
		t.Fatalf("runtime stop: %v", err)
	}
	if err := runtimeBackend.Reload(context.Background()); err != nil {
		t.Fatalf("runtime reload: %v", err)
	}
	if _, err := runtimeBackend.Status(context.Background()); err != nil {
		t.Fatalf("runtime status: %v", err)
	}
	if tracker.startCalls != 1 || tracker.stopCalls != 1 || tracker.reloadCalls != 1 {
		t.Fatalf("unexpected lifecycle controller calls: %+v", tracker)
	}
}

type lifecycleController struct {
	startCalls  int
	stopCalls   int
	reloadCalls int
	status      backend.RuntimeStatus
}

func (c *lifecycleController) Start(context.Context) error {
	c.startCalls++
	return nil
}

func (c *lifecycleController) Stop(context.Context) error {
	c.stopCalls++
	return nil
}

func (c *lifecycleController) Reload(context.Context) error {
	c.reloadCalls++
	return nil
}

func (c *lifecycleController) Status(context.Context) (backend.RuntimeStatus, error) {
	return c.status, nil
}

func findInboundByTag(t *testing.T, inbounds []any, tag string) map[string]any {
	t.Helper()

	for _, raw := range inbounds {
		inbound, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if inbound["tag"] == tag {
			return inbound
		}
	}

	t.Fatalf("inbound %q not found: %+v", tag, inbounds)
	return nil
}

func asStringSlice(t *testing.T, raw any) []string {
	t.Helper()

	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", raw)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		value, ok := item.(string)
		if !ok {
			t.Fatalf("expected string item, got %T", item)
		}
		out = append(out, value)
	}
	return out
}
