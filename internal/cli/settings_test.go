package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/domain"
)

func TestSettingsPatchAcceptsTypedJSONAndSnakeCase(t *testing.T) {
	t.Parallel()

	store := &cliMemoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	cmd := newSettingsCmd(&rootOptions{service: app.NewService(app.Dependencies{Store: store}), jsonOutput: true})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"patch", `{"health_check_interval":"45s","strict_egress_check":false}`})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute typed settings patch: %v", err)
	}
	if got := store.settings.HealthCheckInterval.Duration(); got != 45*time.Second {
		t.Fatalf("unexpected health interval: %s", got)
	}
	if store.settings.StrictEgressCheck {
		t.Fatal("typed boolean was not applied")
	}
}

func TestSettingsPatchRejectsNonStringDuration(t *testing.T) {
	t.Parallel()

	store := &cliMemoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	cmd := newSettingsCmd(&rootOptions{service: app.NewService(app.Dependencies{Store: store})})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"patch", `{"url_test_timeout":5000}`})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "value must be a string") {
		t.Fatalf("expected typed duration rejection, got %v", err)
	}
}

func TestSettingsPatchRejectsNullAndEmptyObjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "null", input: `null`, want: "must be a JSON object"},
		{name: "empty object", input: `{}`, want: "at least one setting is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeOperationalSettingsPatch(tt.input); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decodeOperationalSettingsPatch(%q) error = %v; want %q", tt.input, err, tt.want)
			}
		})
	}
}

func TestSettingsGetIncludesURLTestContract(t *testing.T) {
	t.Parallel()

	store := &cliMemoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	cmd := newSettingsCmd(&rootOptions{service: app.NewService(app.Dependencies{Store: store})})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"get"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute settings get: %v", err)
	}
	for _, want := range []string{"url-test-url=https://", "url-test-timeout=5s", "strict-egress-check=true"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("settings output missing %q:\n%s", want, stdout.String())
		}
	}
}
