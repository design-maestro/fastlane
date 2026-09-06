package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/design-maestro/fastlane/internal/domain"
)

func newSettingsCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Get or update Fast Lane settings",
		Long:  "General Fast Lane settings. For DNS, prefer `fastlane dns ...` because it explains each option in plain language.",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "get",
			Short: "Print current settings",
			RunE: func(cmd *cobra.Command, args []string) error {
				settings, err := opts.service.GetSettings()
				if err != nil {
					return err
				}
				zapret := domain.CanonicalZapretSettingsWithCatalog(settings.Zapret, settings.Firewall.TargetServiceCatalog)

				if opts.jsonOutput {
					return printOutput(cmd, true, settings, "")
				}

				text := fmt.Sprintf(
					"refresh-interval=%s\nhealth-check-interval=%s\nurl-test-url=%s\nurl-test-timeout=%s\nswitch-cooldown=%s\nlatency-threshold=%s\nstrict-egress-check=%t\ncountry-routing-enabled=%t\ncountry-routing-country=%s\nauto-mode=%t\nauto-excluded-nodes=%s\nmode=%s\nlog-level=%s\nfirewall-enabled=%t\nfirewall-mode=%s\nfirewall-port=%d\nfirewall-default-action=%s\nfirewall-targets=%s\nfirewall-target-services=%s\nfirewall-target-domains=%s\nfirewall-target-cidrs=%s\nfirewall-split-proxy=%s\nfirewall-split-bypass=%s\nfirewall-split-excluded-sources=%s\nfirewall-hosts=%s\nfirewall-block-quic=%t\nfirewall-disable-ipv6=%t\nzapret-enabled=%t\nzapret-selectors=%s\nzapret-domains=%s\nzapret-failback-success-threshold=%d",
					settings.RefreshInterval,
					settings.HealthCheckInterval,
					settings.URLTestURL,
					settings.URLTestTimeout,
					settings.SwitchCooldown,
					settings.LatencyThreshold,
					settings.StrictEgressCheck,
					settings.CountryRouting.Enabled,
					settings.CountryRouting.CountryCode,
					settings.AutoMode,
					strings.Join(settings.AutoExcludedNodes, ", "),
					settings.Mode,
					settings.LogLevel,
					settings.Firewall.Enabled,
					domain.NormalizeFirewallMode(settings.Firewall.Mode),
					settings.Firewall.TransparentPort,
					domain.NormalizeFirewallDefaultAction(settings.Firewall.Split.DefaultAction),
					firewallSelectorSummary(settings.Firewall.Targets),
					strings.Join(settings.Firewall.Targets.Services, ", "),
					strings.Join(settings.Firewall.Targets.Domains, ", "),
					strings.Join(settings.Firewall.Targets.CIDRs, ", "),
					firewallSelectorSummary(settings.Firewall.Split.Proxy),
					firewallSelectorSummary(settings.Firewall.Split.Bypass),
					strings.Join(settings.Firewall.Split.ExcludedSources, ", "),
					strings.Join(settings.Firewall.Hosts, ", "),
					settings.Firewall.BlockQUIC,
					settings.Firewall.DisableIPv6,
					zapret.Enabled,
					zapretSelectorSummary(zapret.Selectors),
					strings.Join(zapret.Selectors.Domains, ", "),
					zapret.FailbackSuccessThreshold,
				)
				return printOutput(cmd, false, nil, text)
			},
		},
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Update a setting",
			Long:  "Update one low-level setting key. For DNS settings, prefer `fastlane dns set ...` because it uses simpler names and clearer help.",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				settings, err := opts.service.SetSetting(args[0], args[1])
				if err != nil {
					return err
				}

				return printOutput(cmd, opts.jsonOutput, settings, fmt.Sprintf("Updated %s=%s", args[0], args[1]))
			},
		},
		&cobra.Command{
			Use:   "patch <json>",
			Short: "Validate and update multiple operational settings atomically",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				values, err := decodeOperationalSettingsPatch(args[0])
				if err != nil {
					return err
				}
				settings, err := opts.service.PatchSettings(values)
				if err != nil {
					return err
				}
				return printOutput(cmd, opts.jsonOutput, settings, "Settings updated")
			},
		},
	)

	return cmd
}

func decodeOperationalSettingsPatch(input string) (map[string]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		return nil, fmt.Errorf("decode settings patch: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("decode settings patch: value must be a JSON object")
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("decode settings patch: at least one setting is required")
	}
	aliases := map[string]string{
		"refresh_interval": "refresh-interval", "health_check_interval": "health-check-interval",
		"url_test_url": "url-test-url", "url_test_timeout": "url-test-timeout",
		"switch_cooldown": "switch-cooldown", "latency_threshold": "latency-threshold",
		"strict_egress_check": "strict-egress-check",
		"country_direct":      "country-routing.enabled", "direct_country": "country-routing.country",
	}
	values := make(map[string]string, len(raw))
	for inputKey, encoded := range raw {
		key := inputKey
		if alias, ok := aliases[key]; ok {
			key = alias
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("decode settings patch: duplicate key %q", key)
		}
		var value string
		if err := json.Unmarshal(encoded, &value); err != nil {
			if key != "strict-egress-check" && key != "country-routing.enabled" {
				return nil, fmt.Errorf("decode settings patch %q: value must be a string", inputKey)
			}
			var boolean bool
			if err := json.Unmarshal(encoded, &boolean); err != nil {
				return nil, fmt.Errorf("decode settings patch %q: value must be a boolean or string", inputKey)
			}
			value = strconv.FormatBool(boolean)
		}
		values[key] = value
	}
	return values, nil
}
