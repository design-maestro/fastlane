package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestDurationJSONRoundTrip(t *testing.T) {
	t.Parallel()

	type payload struct {
		Interval domain.Duration `json:"interval"`
	}

	in := payload{Interval: domain.NewDuration(90 * time.Minute)}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out payload
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if out.Interval.Duration() != 90*time.Minute {
		t.Fatalf("unexpected duration: %s", out.Interval.Duration())
	}
}

func TestSubscriptionNodeLookup(t *testing.T) {
	t.Parallel()

	sub := domain.Subscription{
		ID: "sub-1",
		Nodes: []domain.Node{
			{ID: "node-a", Name: "A"},
			{ID: "node-b", Name: "B"},
		},
	}

	node, ok := sub.NodeByID("node-b")
	if !ok {
		t.Fatal("expected node lookup to succeed")
	}

	if node.Name != "B" {
		t.Fatalf("unexpected node: %+v", node)
	}
}

func TestDefaultSettingsAreSane(t *testing.T) {
	t.Parallel()

	settings := domain.DefaultSettings()

	if settings.RefreshInterval.Duration() <= 0 {
		t.Fatal("refresh interval must be positive")
	}

	if settings.SwitchCooldown.Duration() <= 0 {
		t.Fatal("switch cooldown must be positive")
	}

	if settings.Mode != domain.SelectionModeManual {
		t.Fatalf("unexpected default mode: %s", settings.Mode)
	}

	if !settings.StrictEgressCheck {
		t.Fatal("backend egress verification must be strict by default")
	}
	if settings.CountryRouting.Enabled || settings.CountryRouting.CountryCode != "" {
		t.Fatal("country routing must remain unset until the user chooses a country")
	}
	if !domain.FirewallRoutingEnabled(settings.Firewall) || domain.CanonicalFirewallMode(settings.Firewall) != domain.FirewallModeSplit {
		t.Fatalf("fresh settings must route the whole LAN through VPN: %+v", settings.Firewall)
	}
}

func TestCountryRoutingAcceptsTheCompleteISOCatalog(t *testing.T) {
	t.Parallel()

	codes := domain.SupportedCountryCodes()
	if len(codes) != 249 {
		t.Fatalf("expected 249 ISO 3166-1 alpha-2 entries, got %d", len(codes))
	}
	for _, code := range codes {
		if normalized, err := domain.NormalizeCountryCode(code); err != nil || normalized != code {
			t.Fatalf("country %q is not canonical: normalized=%q err=%v", code, normalized, err)
		}
	}
	if _, err := domain.NormalizeCountryCode("ZZ"); err == nil {
		t.Fatal("unknown country code must be rejected")
	}
}

func TestStableNodeIDIncludesProtocolSpecificSettings(t *testing.T) {
	t.Parallel()

	base := domain.Node{
		Protocol:    domain.ProtocolVLESS,
		Address:     "edge.example.com",
		Port:        443,
		UUID:        "11111111-1111-1111-1111-111111111111",
		Security:    "reality",
		PublicKey:   "public-key-a",
		Flow:        "xtls-rprx-vision",
		Transport:   "xhttp",
		RawOutbound: json.RawMessage(`{"protocol":"vless","streamSettings":{"xhttpSettings":{"mode":"auto"}}}`),
		Extras:      map[string]string{"packet_encoding": "xudp", "mode": "auto"},
	}

	variants := []domain.Node{base, base, base, base}
	variants[0].Flow = ""
	variants[1].PublicKey = "public-key-b"
	variants[2].RawOutbound = json.RawMessage(`{"protocol":"vless","streamSettings":{"xhttpSettings":{"mode":"packet-up"}}}`)
	variants[3].Extras = map[string]string{"packet_encoding": "packetaddr", "mode": "auto"}

	baseID := domain.StableNodeID(base)
	for idx, variant := range variants {
		if got := domain.StableNodeID(variant); got == baseID {
			t.Fatalf("variant %d unexpectedly reused stable ID %q", idx, got)
		}
	}

	reordered := base
	reordered.Extras = map[string]string{"mode": "auto", "packet_encoding": "xudp"}
	if got := domain.StableNodeID(reordered); got != baseID {
		t.Fatalf("map order changed stable ID: want %q, got %q", baseID, got)
	}
}

func TestEffectiveTransparentBlockQUICHonorsExplicitSetting(t *testing.T) {
	t.Parallel()

	settings := domain.DefaultSettings().Firewall
	settings.BlockQUIC = true

	if !domain.EffectiveTransparentBlockQUIC(settings, nil) {
		t.Fatal("expected explicit block-quic setting to win")
	}
}

func TestEffectiveTransparentBlockQUICAutoBlocksIncompatibleNode(t *testing.T) {
	t.Parallel()

	settings := domain.DefaultSettings().Firewall
	node := domain.Node{
		Protocol:  domain.ProtocolVLESS,
		Transport: "tcp",
		Security:  "reality",
		Flow:      "xtls-rprx-vision",
	}

	if !domain.EffectiveTransparentBlockQUIC(settings, &node) {
		t.Fatal("expected incompatible node to force block-quic")
	}
}

func TestEffectiveTransparentBlockQUICKeepsCompatibleNodeProxied(t *testing.T) {
	t.Parallel()

	settings := domain.DefaultSettings().Firewall
	node := domain.Node{
		Protocol:  domain.ProtocolVLESS,
		Transport: "ws",
		Security:  "tls",
	}

	if domain.EffectiveTransparentBlockQUIC(settings, &node) {
		t.Fatal("expected compatible node to keep proxied quic")
	}
}
