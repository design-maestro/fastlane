package domain_test

import (
	"reflect"
	"testing"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestNormalizeAutoHideKeywordsPreservesFirstSpelling(t *testing.T) {
	t.Parallel()

	got := domain.NormalizeAutoHideKeywords([]string{" LTE ", "lte", "Россия", "  Russia   Premium  ", ""})
	want := []string{"LTE", "Россия", "Russia Premium"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected keywords: want=%v got=%v", want, got)
	}
}

func TestMatchingAutoHideKeywordUsesNodeTitleAndRemark(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node domain.Node
		want string
	}{
		{name: "title", node: domain.Node{Name: "Финляндия · LTE Reserve"}, want: "lte"},
		{name: "remark", node: domain.Node{Name: "Finland", Remark: "RUSSIA backup"}, want: "Russia"},
		{name: "provider is ignored", node: domain.Node{Name: "Finland", ProviderName: "LTE VPN"}, want: ""},
		{name: "address is ignored", node: domain.Node{Name: "Finland", Address: "lte.example.com"}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.MatchingAutoHideKeyword([]string{"lte", "Russia"}, tt.node); got != tt.want {
				t.Fatalf("unexpected match: want=%q got=%q", tt.want, got)
			}
		})
	}
}

func TestIsNodeExcludedFromAutoCombinesManualAndKeywordRules(t *testing.T) {
	t.Parallel()

	settings := domain.DefaultSettings()
	settings.AutoExcludedNodes = []string{"sub-1/manual"}
	settings.AutoHideKeywords = []string{"LTE"}

	if !domain.IsNodeExcludedFromAuto(settings, "sub-1", domain.Node{ID: "manual", Name: "Finland"}) {
		t.Fatal("manual exclusion was ignored")
	}
	if !domain.IsNodeExcludedFromAuto(settings, "sub-1", domain.Node{ID: "matched", Name: "Finland · LTE"}) {
		t.Fatal("keyword exclusion was ignored")
	}
	if domain.IsNodeExcludedFromAuto(settings, "sub-1", domain.Node{ID: "visible", Name: "Finland"}) {
		t.Fatal("unmatched node was excluded")
	}
}
