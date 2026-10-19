package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestExclusionsCSVRoundTripAndValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := &memoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	source.settings.Firewall.TargetServiceCatalog = map[string]domain.FirewallTargetDefinition{
		"home":  {Services: []string{"cloud"}, Domains: []string{"home.example.com"}},
		"cloud": {Domains: []string{"cloud.example.com"}, CIDRs: []string{"192.0.2.0/24"}},
	}
	source.settings.Firewall.Split.Bypass = domain.FirewallSelectorSet{Services: []string{"home", "telegram"}, Domains: []string{"direct.example.com"}, CIDRs: []string{"198.51.100.0/24"}}
	data, err := NewService(Dependencies{Store: source}).ExportExclusionsCSV()
	if err != nil || !strings.HasPrefix(data, "\ufeffname;enabled;") {
		t.Fatalf("export: %q %v", data, err)
	}
	destination := &memoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	destination.settings.Firewall.TargetServiceCatalog = map[string]domain.FirewallTargetDefinition{"keep": {Domains: []string{"keep.example.com"}}}
	service := NewService(Dependencies{Store: destination})
	before := domain.CanonicalFirewallSettings(destination.settings.Firewall)
	preview, err := service.ImportExclusionsCSV(ctx, data, true)
	if err != nil || preview.Added != 2 || !reflect.DeepEqual(before, destination.settings.Firewall) {
		t.Fatalf("preview mutated settings or failed: %+v %v", preview, err)
	}
	if _, err := service.ImportExclusionsCSV(ctx, data, false); err != nil {
		t.Fatal(err)
	}
	firewall := destination.settings.Firewall
	if _, ok := firewall.TargetServiceCatalog["keep"]; !ok || len(firewall.TargetServiceCatalog) != 3 {
		t.Fatalf("unrelated group lost: %+v", firewall.TargetServiceCatalog)
	}
	if !reflect.DeepEqual(firewall.Split.Bypass, source.settings.Firewall.Split.Bypass) {
		t.Fatalf("bypass states or ungrouped selectors lost: %+v", firewall.Split.Bypass)
	}
	if result, err := service.ImportExclusionsCSV(ctx, data, false); err != nil || result.Changed {
		t.Fatalf("repeat import must be a no-op: %+v %v", result, err)
	}
	valid := "name;enabled;domains;cidrs;services\n"
	for _, invalid := range []string{
		"wrong;headers\nx;y\n",
		valid + "test;true;bad domain;;\n",
		valid + "test;true;;999.1.1.1;\n",
		valid + "test;maybe;test.example.com;;\n",
		valid + "test;true;test.example.com;;\ntest;false;other.example.com;;\n",
		valid + "first;true;;;second\nsecond;true;;;first\n",
		valid + "test;true;;;missing\n",
		valid + "youtube;true;test.example.com;;\n",
		strings.Repeat("x", MaxExclusionsCSVBytes+1),
	} {
		previous := domain.CanonicalFirewallSettings(destination.settings.Firewall)
		if _, err := service.ImportExclusionsCSV(ctx, invalid, false); err == nil {
			t.Fatalf("invalid CSV accepted: %.100s", invalid)
		}
		if !reflect.DeepEqual(previous, destination.settings.Firewall) {
			t.Fatal("invalid CSV partially changed settings")
		}
	}
}
