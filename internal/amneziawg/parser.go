package amneziawg

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	// ErrUnsupportedVersion marks profiles from AWG versions other than 2.0.
	ErrUnsupportedVersion = errors.New("unsupported AmneziaWG version")
	// ErrUnsupportedParameter marks parameters outside the safe import contract.
	ErrUnsupportedParameter = errors.New("unsupported AmneziaWG parameter")
	// ErrUnsafeDirective marks lifecycle hooks that must never be executed.
	ErrUnsafeDirective = errors.New("unsafe AmneziaWG directive")
)

var unsafeDirectives = map[string]struct{}{
	"preup": {}, "postup": {}, "predown": {}, "postdown": {},
}

var awg3Parameters = map[string]struct{}{
	"headerprotectionkey": {}, "contentpaddingaddition": {},
	"rekeyaftertime": {}, "rekeytimeout": {}, "rejectaftertime": {},
	"keepalivetimeout": {}, "maxhandshakeattempts": {},
	"randomtrailers": {}, "disablecookies": {},
}

type rawProfile struct {
	sections map[string]map[string]string
	seen     map[string]map[string]bool
	ignored  map[string]struct{}
}

// Parse parses and validates one native AWG 2.0 .conf profile.
func Parse(data []byte) (Profile, error) {
	raw, err := parseINI(data)
	if err != nil {
		return Profile{}, err
	}
	return buildProfile(raw)
}

// ParseConfig is an explicit alias for Parse.
func ParseConfig(data []byte) (Profile, error) { return Parse(data) }

func parseINI(data []byte) (rawProfile, error) {
	raw := rawProfile{
		sections: map[string]map[string]string{"interface": {}, "peer": {}},
		seen:     map[string]map[string]bool{"interface": {}, "peer": {}},
		ignored:  make(map[string]struct{}),
	}
	sectionCounts := map[string]int{}
	currentSection := ""
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if currentSection != "interface" && currentSection != "peer" {
				return rawProfile{}, fmt.Errorf("line %d: %w %q", lineNumber, ErrUnsupportedParameter, line)
			}
			sectionCounts[currentSection]++
			if sectionCounts[currentSection] > 1 {
				return rawProfile{}, fmt.Errorf("line %d: expected exactly one [%s] section", lineNumber, canonicalSection(currentSection))
			}
			continue
		}
		if currentSection == "" {
			return rawProfile{}, fmt.Errorf("line %d: parameter appears before a section", lineNumber)
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return rawProfile{}, fmt.Errorf("line %d: expected Key = Value", lineNumber)
		}
		canonicalKey := strings.TrimSpace(key)
		normalizedKey := strings.ToLower(canonicalKey)
		value = strings.TrimSpace(value)
		if normalizedKey != "i1" && normalizedKey != "i2" && normalizedKey != "i3" && normalizedKey != "i4" && normalizedKey != "i5" {
			value = stripInlineComment(value)
		}
		if _, unsafe := unsafeDirectives[normalizedKey]; unsafe {
			return rawProfile{}, fmt.Errorf("line %d: %w %q; lifecycle hooks are forbidden", lineNumber, ErrUnsafeDirective, canonicalKey)
		}
		if _, newer := awg3Parameters[normalizedKey]; newer {
			return rawProfile{}, fmt.Errorf("line %d: %w: parameter %q requires AWG 3.x; only 2.0 is supported", lineNumber, ErrUnsupportedVersion, canonicalKey)
		}
		if currentSection == "interface" && (normalizedKey == "version" || normalizedKey == "protocolversion") {
			if value != "2" && value != Version20 {
				return rawProfile{}, fmt.Errorf("line %d: %w %q; only 2.0 is supported", lineNumber, ErrUnsupportedVersion, value)
			}
			if raw.seen[currentSection][normalizedKey] {
				return rawProfile{}, fmt.Errorf("line %d: duplicate parameter %q in [Interface]", lineNumber, canonicalKey)
			}
			raw.seen[currentSection][normalizedKey] = true
			continue
		}
		if raw.seen[currentSection][normalizedKey] {
			return rawProfile{}, fmt.Errorf("line %d: duplicate parameter %q in [%s]", lineNumber, canonicalKey, canonicalSection(currentSection))
		}
		if err := acceptParameter(raw, currentSection, normalizedKey, canonicalKey, value, lineNumber); err != nil {
			return rawProfile{}, err
		}
		raw.seen[currentSection][normalizedKey] = true
	}
	if err := scanner.Err(); err != nil {
		return rawProfile{}, fmt.Errorf("read AmneziaWG profile: %w", err)
	}
	if sectionCounts["interface"] != 1 || sectionCounts["peer"] != 1 {
		return rawProfile{}, fmt.Errorf("expected exactly one [Interface] and one [Peer] section")
	}
	return raw, nil
}

func stripInlineComment(value string) string {
	if index := strings.IndexByte(value, '#'); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return value
}

func acceptParameter(raw rawProfile, section, key, displayKey, value string, line int) error {
	if section == "interface" {
		switch key {
		case "privatekey", "address", "mtu", "jc", "jmin", "jmax",
			"s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4",
			"i1", "i2", "i3", "i4", "i5":
			raw.sections[section][key] = value
			return nil
		case "dns", "table":
			raw.ignored[canonicalParameter(section, key)] = struct{}{}
			return nil
		}
	} else {
		switch key {
		case "publickey", "endpoint", "presharedkey", "persistentkeepalive":
			raw.sections[section][key] = value
			return nil
		case "allowedips":
			raw.ignored[canonicalParameter(section, key)] = struct{}{}
			return nil
		}
	}
	return fmt.Errorf("line %d: %w %q in [%s]", line, ErrUnsupportedParameter, displayKey, canonicalSection(section))
}

func canonicalSection(section string) string {
	if section == "interface" {
		return "Interface"
	}
	return "Peer"
}

func canonicalParameter(section, key string) string {
	lookup := map[string]string{
		"dns": "DNS", "table": "Table", "allowedips": "AllowedIPs",
	}
	return "[" + canonicalSection(section) + "]." + lookup[key]
}

func required(values map[string]string, key, display string) (string, error) {
	value, ok := values[key]
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("missing required parameter %s", display)
	}
	return value, nil
}

func parseUint16(values map[string]string, key, display string) (uint16, error) {
	raw, err := required(values, key, display)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseUint(raw, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: expected an integer from 0 to 65535", display)
	}
	return uint16(parsed), nil
}
