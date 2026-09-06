package luci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFastLaneCurrentViewsKeepMobileInteractionContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		required  []string
		forbidden []string
	}{
		{
			name: "shared navigation",
			path: filepath.Join("fastlane", "fastlane-20260906-v4.js"),
			required: []string{
				"@media(max-width:1100px)",
				".fl-nav-links{display:flex;gap:8px}",
				"@media(max-width:760px)",
				".fl-shell-nav{height:auto;grid-template-columns:1fr",
				".fl-nav-links{width:100%;height:48px;gap:4px;overflow-x:auto",
				".fl-nav-link{flex:1;justify-content:center;min-width:max-content",
			},
			forbidden: []string{".fl-nav-links{display:none"},
		},
		{
			name: "VPN",
			path: filepath.Join("view", "fastlane", "vpn-20260906-latency-v19.js"),
			required: []string{
				"@media(max-width:1100px)",
				".fl-status{grid-template-columns:1fr 1fr 1fr}",
				"@media(max-width:760px)",
				".fl-status{grid-template-columns:1fr 1fr}",
				".fl-toolbar-actions{grid-template-columns:1fr 1fr}",
				".fl-search-wrap{grid-column:1/-1;min-width:0}",
				".fl-table tbody tr{position:relative;display:grid;grid-template-columns:repeat(4,minmax(0,1fr))",
				".fl-table-single tbody tr{grid-template-columns:repeat(3,minmax(0,1fr))}",
				".fl-table-single .fl-meta-status{grid-column:3}",
				".fastlane-modal{width:calc(100vw - 24px)!important",
				".fastlane-modal .fl-modal-button{flex:1;min-width:0!important}",
			},
		},
		{
			name: "Routes",
			path: filepath.Join("view", "fastlane", "routing-20260906-v5.js"),
			required: []string{
				"@media(max-width:900px)",
				".flr-head{align-items:flex-start;flex-direction:column}",
				".flr-control{grid-template-columns:1fr}",
				".flr-flow{grid-template-columns:1fr}",
				".flr-arrow{height:28px;transform:rotate(90deg)}",
				".flr-import{grid-template-columns:1fr}",
				".flr-button{width:100%}",
			},
		},
		{
			name: "Diagnostics",
			path: filepath.Join("view", "fastlane", "diagnostics-20260904-v3.js"),
			required: []string{
				"@media(max-width:760px)",
				".fld-row{grid-template-columns:1fr 1fr",
				".fld-note{grid-column:1/-1",
				".fld-tech-row{grid-template-columns:1fr",
			},
		},
		{
			name: "Settings",
			path: filepath.Join("view", "fastlane", "settings-20260905-updates-v6.js"),
			required: []string{
				"@media(max-width:850px)",
				".fls-head{align-items:flex-start;flex-direction:column}",
				".fls-grid{grid-template-columns:1fr}",
				".fls-field{grid-template-columns:1fr}",
				".fls-toggle{justify-content:flex-start}",
			},
		},
	}

	root, err := filepath.Abs(filepath.Join("..", "..", "luci-app-fastlane", "htdocs", "luci-static", "resources"))
	if err != nil {
		t.Fatalf("resolve LuCI resources root: %v", err)
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			sourcePath := filepath.Join(root, test.path)
			payload, err := os.ReadFile(sourcePath)
			if err != nil {
				t.Fatalf("read %s: %v", sourcePath, err)
			}
			source := string(payload)
			for _, marker := range test.required {
				if !strings.Contains(source, marker) {
					t.Errorf("missing mobile contract %q", marker)
				}
			}
			for _, marker := range test.forbidden {
				if strings.Contains(source, marker) {
					t.Errorf("forbidden mobile behavior returned: %q", marker)
				}
			}
		})
	}
}
