package luci_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestFastLaneViewsUseEnglishSourceAndCompleteRussianCatalog(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	paths := []string{
		filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "fastlane", "fastlane-20260906-v4.js"),
		filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "vpn-20260906-latency-v19.js"),
		filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "routing-20260906-v4.js"),
		filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "diagnostics-20260904-v3.js"),
		filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "settings-20260905-updates-v6.js"),
	}
	poPath := filepath.Join(root, "luci-app-fastlane", "po", "ru", "fastlane.po")
	poData, err := os.ReadFile(poPath)
	if err != nil {
		t.Fatalf("read Russian catalog: %v", err)
	}
	translations := parsePOStrings(t, string(poData))
	msgIDPattern := regexp.MustCompile(`_\('((?:[^'\\]|\\.)*)'\)`)
	cyrillic := regexp.MustCompile(`[А-Яа-яЁё]`)

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		source := strings.ReplaceAll(string(data), "Русский", "")
		if cyrillic.MatchString(source) {
			t.Fatalf("%s contains untranslated Cyrillic source text", filepath.Base(path))
		}
		for _, match := range msgIDPattern.FindAllStringSubmatch(source, -1) {
			msgID := strings.ReplaceAll(match[1], `\'`, `'`)
			if strings.TrimSpace(translations[msgID]) == "" {
				t.Fatalf("%s has no Russian translation for %q", filepath.Base(path), msgID)
			}
		}
	}
}

func TestFastLaneCountrySelectorContainsCompleteISOCatalog(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "fastlane", "countries.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read country catalog: %v", err)
	}
	source := string(data)
	if !strings.Contains(source, "Intl.DisplayNames") {
		t.Fatal("country selector must localize names through Intl.DisplayNames")
	}
	codes := regexp.MustCompile(`'([A-Z]{2})'`).FindAllStringSubmatch(source, -1)
	unique := map[string]struct{}{}
	for _, match := range codes {
		unique[match[1]] = struct{}{}
	}
	want := domain.SupportedCountryCodes()
	if len(unique) != len(want) {
		t.Fatalf("expected %d ISO countries in LuCI, got %d", len(want), len(unique))
	}
	for _, code := range want {
		if _, ok := unique[code]; !ok {
			t.Fatalf("LuCI country catalog is missing backend-supported code %s", code)
		}
	}
}

func parsePOStrings(t *testing.T, source string) map[string]string {
	t.Helper()
	result := map[string]string{}
	lines := strings.Split(source, "\n")
	for i := 0; i+1 < len(lines); i++ {
		if !strings.HasPrefix(lines[i], `msgid "`) || !strings.HasPrefix(lines[i+1], `msgstr "`) {
			continue
		}
		msgID, err := strconv.Unquote(strings.TrimPrefix(lines[i], "msgid "))
		if err != nil {
			t.Fatalf("parse msgid on line %d: %v", i+1, err)
		}
		msgstr, err := strconv.Unquote(strings.TrimPrefix(lines[i+1], "msgstr "))
		if err != nil {
			t.Fatalf("parse msgstr on line %d: %v", i+2, err)
		}
		if msgID != "" {
			result[msgID] = msgstr
		}
	}
	return result
}
