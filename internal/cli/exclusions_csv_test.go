package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/domain"
)

func TestImportCSVSingleLineTransport(t *testing.T) {
	store := &cliMemoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	service := app.NewService(app.Dependencies{Store: store})
	data := "\ufeffname;enabled;domains;cidrs;services\r\ncloud;true;" + strings.Repeat("cloud.example.com ", 80) + ";;\r\n"
	t.Setenv("FASTLANE_EXCLUSIONS_CSV", data)
	for _, check := range []bool{true, false} {
		cmd := newServicesCmd(&rootOptions{service: service, jsonOutput: true})
		cmd.SetOut(new(bytes.Buffer))
		args := []string{"import-csv", "--data-env"}
		if check {
			args = append(args, "--check")
		}
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if len(store.settings.Firewall.TargetServiceCatalog) != map[bool]int{true: 0, false: 1}[check] {
			t.Fatal("CSV was changed before confirmation or lost in transport")
		}
	}
}
