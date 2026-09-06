package xray

import (
	"reflect"
	"testing"

	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
)

func TestCountryDirectRulesRemainAheadOfSelectiveCaptureFallback(t *testing.T) {
	t.Parallel()

	rules := transparentRoutingRules(backend.ConfigRequest{
		TransparentProxy:            true,
		TransparentSelectiveCapture: true,
		TransparentCountryRouting:   domain.CountryRouting{Enabled: true, CountryCode: "RU"},
		TransparentDefaultAction:    domain.FirewallDefaultActionDirect,
		TransparentProxyDomains:     []string{"example.ru"},
	})

	if len(rules) != 6 {
		t.Fatalf("expected four country-direct rules before two selective-capture fallback rules, got %d: %+v", len(rules), rules)
	}
	if !reflect.DeepEqual(rules[0].Domain, []string{"geosite:category-ru"}) || rules[0].OutboundTag != "direct" {
		t.Fatalf("selective capture lost the Russia GeoSite rule: %+v", rules[0])
	}
	if !reflect.DeepEqual(rules[2].IP, []string{"geoip:ru"}) || rules[2].OutboundTag != "direct" {
		t.Fatalf("selective capture lost the Russia GeoIP rule: %+v", rules[2])
	}
	if rules[4].OutboundTag != "selected" || rules[5].OutboundTag != "selected" {
		t.Fatalf("expected the selected VPN fallback only after country-direct rules: %+v", rules)
	}
}
