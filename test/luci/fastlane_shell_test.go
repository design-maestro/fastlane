package luci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFastLaneShellKeepsBrandAndBackgroundConsistent(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	shellPath := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "fastlane", "fastlane-20260906-v4.js")
	shellData, err := os.ReadFile(shellPath)
	if err != nil {
		t.Fatalf("read %s: %v", shellPath, err)
	}
	shellSource := string(shellData)

	for _, want := range []string{
		"--fl-bg:#02090c",
		"border:0",
		"font-family:\"Avenir Next\"",
		"font-size:25px",
		"font-weight:750",
		"letter-spacing:-.025em",
		"fastlane-mark.png",
		".fl-nav-links{display:flex",
		".main-right>header.bg-primary",
		"showToast: function(message, type, details)",
		"window.setTimeout(close, 3000)",
		".fl-toast-layer{position:fixed;z-index:10000;top:40px;left:50%",
		"transform:translateX(-50%)",
		"max-width:min(420px",
		"text-align:left",
		"fl-toast-details",
		".fl-dialog-form{display:grid!important;gap:17px!important",
		".fl-dialog .fl-dialog-form input[type=\"text\"]",
		".fl-dialog .fl-dialog-form textarea{min-height:150px!important",
		".fl-dialog-actions{display:flex!important;justify-content:flex-end!important",
		".fl-dialog-button{min-width:112px!important;min-height:44px!important",
	} {
		if !strings.Contains(shellSource, want) {
			t.Fatalf("Fast Lane shell missing shared style marker %q", want)
		}
	}
	if strings.Contains(shellSource, ".fl-nav-links{display:none") {
		t.Fatal("Fast Lane shell must keep section navigation reachable on narrow screens")
	}
	for _, forbidden := range []string{"fl-toast-close", "Закрыть уведомление"} {
		if strings.Contains(shellSource, forbidden) {
			t.Fatalf("self-dismissing Fast Lane toast must not contain %q", forbidden)
		}
	}

	for _, viewName := range []string{"vpn-20260906-latency-v19.js", "routing-20260906-v4.js", "diagnostics-20260904-v3.js", "settings-20260905-updates-v6.js"} {
		viewPath := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", viewName)
		viewData, err := os.ReadFile(viewPath)
		if err != nil {
			t.Fatalf("read %s: %v", viewPath, err)
		}
		viewSource := string(viewData)
		if !strings.Contains(viewSource, "require fastlane.fastlane-20260906-v4 as fastlaneShell") ||
			!strings.Contains(viewSource, "fastlaneShell.renderStyles()") ||
			!strings.Contains(viewSource, "fastlaneShell.renderHeader(") {
			t.Fatalf("%s does not use the shared Fast Lane shell", viewName)
		}
	}
}

func TestFastLaneViewsUseSharedToastsInsteadOfLuCINotifications(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	for _, viewName := range []string{"vpn-20260906-latency-v19.js", "routing-20260906-v4.js", "diagnostics-20260904-v3.js", "settings-20260905-updates-v6.js"} {
		path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", viewName)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		source := string(data)
		if strings.Contains(source, "ui.addNotification") {
			t.Fatalf("%s must not use persistent LuCI notifications", viewName)
		}
		if !strings.Contains(source, "fastlaneShell.showToast") {
			t.Fatalf("%s must use shared Fast Lane toasts", viewName)
		}
	}
}
