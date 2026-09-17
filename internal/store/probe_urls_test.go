package store

import "testing"

func TestProbeURLsMigrationAndRoundTrip(t *testing.T) {
	settings, err := decodeSettings([]byte(`{"url_test_url":"https://custom.example/204"}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	if settings.URLTestURL != "https://custom.example/204" || settings.URLTestFallbackURL != "https://cp.cloudflare.com/generate_204" {
		t.Fatal("migration lost primary URL or fallback default")
	}
	fs := NewFileStore(t.TempDir())
	settings.URLTestFallbackURL = "https://second.example/204"
	if err := fs.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := fs.LoadSettings()
	if err != nil || loaded.URLTestFallbackURL != settings.URLTestFallbackURL {
		t.Fatalf("round trip failed: %v", err)
	}
}
