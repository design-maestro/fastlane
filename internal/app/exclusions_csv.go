package app

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/design-maestro/fastlane/internal/domain"
)

const MaxExclusionsCSVBytes = 64 * 1024

var exclusionsCSVHeader = []string{"name", "enabled", "domains", "cidrs", "services"}

type ExclusionsImportResult struct {
	Groups  int  `json:"groups"`
	Added   int  `json:"added"`
	Updated int  `json:"updated"`
	Changed bool `json:"changed"`
}

// ExportExclusionsCSV includes disabled groups and ungrouped bypass selectors.
func (s *Service) ExportExclusionsCSV() (string, error) {
	settings, err := s.store.LoadSettings()
	if err != nil {
		return "", err
	}
	firewall := domain.CanonicalFirewallSettings(settings.Firewall)
	var output strings.Builder
	output.WriteString("\ufeff")
	writer := csv.NewWriter(&output)
	writer.Comma, writer.UseCRLF = ';', true
	_ = writer.Write(exclusionsCSVHeader)
	names := make([]string, 0, len(firewall.TargetServiceCatalog))
	for name := range firewall.TargetServiceCatalog {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		definition := firewall.TargetServiceCatalog[name]
		_ = writer.Write([]string{name, strconv.FormatBool(slices.Contains(firewall.Split.Bypass.Services, name)), strings.Join(definition.Domains, " "), strings.Join(definition.CIDRs, " "), strings.Join(definition.Services, " ")})
	}
	var services []string
	for _, name := range firewall.Split.Bypass.Services {
		if _, custom := firewall.TargetServiceCatalog[name]; !custom {
			services = append(services, name)
		}
	}
	if len(services)+len(firewall.Split.Bypass.Domains)+len(firewall.Split.Bypass.CIDRs) > 0 {
		_ = writer.Write([]string{"", "true", strings.Join(firewall.Split.Bypass.Domains, " "), strings.Join(firewall.Split.Bypass.CIDRs, " "), strings.Join(services, " ")})
	}
	writer.Flush()
	if output.Len() > MaxExclusionsCSVBytes {
		return "", fmt.Errorf("exclusions exceed the CSV size limit of 64 KiB")
	}
	return output.String(), writer.Error()
}

// ImportExclusionsCSV merges validated groups in one settings transaction.
func (s *Service) ImportExclusionsCSV(ctx context.Context, content string, checkOnly bool) (ExclusionsImportResult, error) {
	return runStoreWriteLockedResult(s, func() (ExclusionsImportResult, error) {
		var result ExclusionsImportResult
		if len(content) > MaxExclusionsCSVBytes {
			return result, fmt.Errorf("CSV exceeds 64 KiB")
		}
		content = strings.TrimPrefix(content, "\ufeff")
		reader := csv.NewReader(strings.NewReader(content))
		if strings.Contains(strings.SplitN(content, "\n", 2)[0], ";") {
			reader.Comma = ';'
		}
		rows, err := reader.ReadAll()
		if err != nil || len(rows) < 2 || !slices.Equal(rows[0], exclusionsCSVHeader) {
			return result, fmt.Errorf("invalid CSV: expected name;enabled;domains;cidrs;services and at least one row")
		}
		original, err := s.store.LoadSettings()
		if err != nil {
			return result, err
		}
		settings := original
		settings.Firewall = domain.CanonicalFirewallSettings(original.Firewall)
		catalog := settings.Firewall.TargetServiceCatalog
		if catalog == nil {
			catalog = map[string]domain.FirewallTargetDefinition{}
		}
		enabled := map[string]bool{}
		definitions := map[string]domain.FirewallTargetDefinition{}
		var ungrouped *domain.FirewallTargetDefinition
		for index, row := range rows[1:] {
			name := strings.ToLower(strings.TrimSpace(row[0]))
			if _, duplicate := enabled[name]; duplicate {
				return result, fmt.Errorf("CSV row %d: duplicate group %q", index+2, name)
			}
			active, err := strconv.ParseBool(strings.TrimSpace(row[1]))
			if err != nil {
				return result, fmt.Errorf("CSV row %d: enabled must be true or false", index+2)
			}
			enabled[name] = active
			domains, err := domain.ParseFirewallTargets(strings.Fields(row[2]), nil)
			if err != nil || len(domains.Services)+len(domains.CIDRs) != 0 {
				return result, fmt.Errorf("CSV row %d: invalid domains", index+2)
			}
			cidrs, err := domain.ParseFirewallTargets(strings.Fields(row[3]), nil)
			if err != nil || len(cidrs.Services)+len(cidrs.Domains) != 0 {
				return result, fmt.Errorf("CSV row %d: invalid IPv4 addresses or networks", index+2)
			}
			definition := domain.FirewallTargetDefinition{Domains: domains.Domains, CIDRs: cidrs.CIDRs, Services: strings.Fields(strings.ToLower(row[4]))}
			if name == "" {
				if !active {
					return result, fmt.Errorf("CSV row %d: ungrouped exclusions must be enabled", index+2)
				}
				ungrouped = &definition
				continue
			}
			if _, _, err := domain.ParseFirewallTargetDefinition(name, []string{"example.com"}, nil); err != nil {
				return result, fmt.Errorf("CSV row %d: %w", index+2, err)
			}
			definitions[name], catalog[name] = definition, definition
			result.Groups++
			if _, exists := original.Firewall.TargetServiceCatalog[name]; exists {
				result.Updated++
			} else {
				result.Added++
			}
		}
		for name, definition := range definitions {
			for _, dependency := range definition.Services {
				if _, ok := domain.LookupFirewallTargetService(catalog, dependency); !ok {
					return result, fmt.Errorf("group %q: unknown service %q", name, dependency)
				}
			}
			selectors := append(append(slices.Clone(definition.Services), definition.Domains...), definition.CIDRs...)
			_, normalized, err := domain.ParseFirewallTargetDefinition(name, selectors, catalog)
			if err != nil {
				return result, err
			}
			catalog[name] = normalized
			services := settings.Firewall.Split.Bypass.Services
			if !enabled[name] {
				services = slices.DeleteFunc(services, func(value string) bool { return value == name })
			} else if !slices.Contains(services, name) {
				services = append(services, name)
			}
			settings.Firewall.Split.Bypass.Services = services
		}
		settings.Firewall.TargetServiceCatalog = catalog
		if ungrouped != nil {
			selectors := append(append(slices.Clone(ungrouped.Services), ungrouped.Domains...), ungrouped.CIDRs...)
			parsed, err := domain.ParseFirewallTargets(selectors, catalog)
			if err != nil {
				return result, err
			}
			bypass := &settings.Firewall.Split.Bypass
			bypass.Services = mergeExclusionValues(bypass.Services, parsed.Services)
			bypass.Domains = mergeExclusionValues(bypass.Domains, parsed.Domains)
			bypass.CIDRs = mergeExclusionValues(bypass.CIDRs, parsed.CIDRs)
		}
		settings.Firewall = domain.CanonicalFirewallSettings(settings.Firewall)
		settings.Firewall.ModeDrafts.Split.Bypass = domain.CloneFirewallSelectorSet(settings.Firewall.Split.Bypass)
		if err := s.validateFirewall(ctx, settings.Firewall); err != nil {
			return result, err
		}
		result.Changed = !reflect.DeepEqual(domain.CanonicalFirewallSettings(original.Firewall), settings.Firewall)
		if checkOnly || !result.Changed {
			return result, nil
		}
		if err := s.store.SaveSettings(settings); err != nil {
			return result, err
		}
		previousFirewall := domain.CanonicalFirewallSettings(original.Firewall)
		if reflect.DeepEqual(previousFirewall.TargetServiceCatalog, settings.Firewall.TargetServiceCatalog) && reflect.DeepEqual(previousFirewall.Split.Bypass, settings.Firewall.Split.Bypass) {
			return result, nil
		}
		if err := s.reapplyCurrentConnectionWithReload(ctx); err != nil {
			saveErr := s.store.SaveSettings(original)
			var restoreErr error
			if saveErr == nil {
				restoreErr = s.reapplyCurrentConnectionWithReload(ctx)
			}
			return result, errors.Join(err, saveErr, restoreErr)
		}
		return result, nil
	})
}

func mergeExclusionValues(current, imported []string) []string {
	for _, value := range imported {
		if !slices.Contains(current, value) {
			current = append(current, value)
		}
	}
	return current
}
