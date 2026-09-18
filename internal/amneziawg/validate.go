package amneziawg

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

func buildProfile(raw rawProfile) (Profile, error) {
	interfaceValues := raw.sections["interface"]
	peerValues := raw.sections["peer"]

	_, hasS3 := interfaceValues["s3"]
	_, hasS4 := interfaceValues["s4"]
	if hasS3 != hasS4 {
		return Profile{}, fmt.Errorf("invalid AmneziaWG profile: S3 and S4 must either both be present or both be absent")
	}
	version := VersionLegacy
	if hasS3 {
		version = Version20
	}
	if raw.declaredVersion == Version31 || hasV31Parameter(interfaceValues) {
		version = Version31
	}
	if raw.declaredVersion != "" && raw.declaredVersion != version {
		return Profile{}, fmt.Errorf("invalid AmneziaWG profile: declared version %s does not match its obfuscation parameters", raw.declaredVersion)
	}
	if version == VersionLegacy {
		for i := 1; i <= 5; i++ {
			if strings.TrimSpace(interfaceValues[fmt.Sprintf("i%d", i)]) != "" {
				return Profile{}, fmt.Errorf("invalid AmneziaWG Legacy profile: I%d requires AWG 2.0", i)
			}
		}
	}

	privateRaw, err := required(interfaceValues, "privatekey", "[Interface].PrivateKey")
	if err != nil {
		return Profile{}, err
	}
	privateKey, err := parsePrivateKey(privateRaw, "[Interface].PrivateKey")
	if err != nil {
		return Profile{}, err
	}
	addressRaw, err := required(interfaceValues, "address", "[Interface].Address")
	if err != nil {
		return Profile{}, err
	}
	addresses, err := parsePrefixes(addressRaw, "[Interface].Address")
	if err != nil {
		return Profile{}, err
	}
	var mtu uint16
	if mtuRaw := strings.TrimSpace(interfaceValues["mtu"]); mtuRaw != "" {
		parsed, parseErr := strconv.ParseUint(mtuRaw, 10, 16)
		if parseErr != nil || parsed < 576 || parsed > 9000 {
			return Profile{}, fmt.Errorf("invalid [Interface].MTU: expected an integer from 576 to 9000")
		}
		mtu = uint16(parsed)
	}

	obfuscation, err := parseObfuscation(interfaceValues, version)
	if err != nil {
		return Profile{}, err
	}
	v31, err := parseV31(interfaceValues, version)
	if err != nil {
		return Profile{}, err
	}
	publicRaw, err := required(peerValues, "publickey", "[Peer].PublicKey")
	if err != nil {
		return Profile{}, err
	}
	publicKey, err := parsePublicKey(publicRaw)
	if err != nil {
		return Profile{}, err
	}
	endpointRaw, err := required(peerValues, "endpoint", "[Peer].Endpoint")
	if err != nil {
		return Profile{}, err
	}
	endpoint, err := parseEndpoint(endpointRaw)
	if err != nil {
		return Profile{}, err
	}

	peer := Peer{PublicKey: publicKey, Endpoint: endpoint}
	if pskRaw := strings.TrimSpace(peerValues["presharedkey"]); pskRaw != "" {
		peer.PresharedKey, err = parsePresharedKey(pskRaw, "[Peer].PresharedKey")
		if err != nil {
			return Profile{}, err
		}
	}
	if keepaliveRaw := strings.TrimSpace(peerValues["persistentkeepalive"]); keepaliveRaw != "" {
		keepalive, parseErr := parseUint32Range(keepaliveRaw, "[Peer].PersistentKeepalive")
		if parseErr != nil || keepalive.Max > 65535 {
			return Profile{}, fmt.Errorf("invalid [Peer].PersistentKeepalive: expected an integer or range from 0 to 65535")
		}
		peer.PersistentKeepalive = keepalive
	}

	ignored := make([]string, 0, len(raw.ignored))
	for parameter := range raw.ignored {
		ignored = append(ignored, parameter)
	}
	sort.Strings(ignored)
	profile := Profile{
		Version: version,
		Interface: Interface{
			PrivateKey:  privateKey,
			Addresses:   addresses,
			MTU:         mtu,
			Obfuscation: obfuscation,
			V31:         v31,
		},
		Peer:              peer,
		IgnoredParameters: ignored,
	}
	if err := profile.Validate(); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// Validate checks a profile's domain invariants without exposing key material.
func Validate(profile Profile) error { return profile.Validate() }

// Validate checks a profile's domain invariants without exposing key material.
func (p Profile) Validate() error {
	if p.Version != VersionLegacy && p.Version != Version20 && p.Version != Version31 {
		return fmt.Errorf("%w %q; Legacy, 2.0 and 3.1 are supported", ErrUnsupportedVersion, p.Version)
	}
	if !p.Interface.PrivateKey.present() || allZero(p.Interface.PrivateKey.value[:]) {
		return fmt.Errorf("invalid [Interface].PrivateKey: expected a non-zero 32-byte base64 key")
	}
	if len(p.Interface.Addresses) == 0 {
		return fmt.Errorf("missing required parameter [Interface].Address")
	}
	for _, address := range p.Interface.Addresses {
		if !address.IsValid() {
			return fmt.Errorf("invalid [Interface].Address CIDR")
		}
	}
	if p.Interface.MTU != 0 && (p.Interface.MTU < 576 || p.Interface.MTU > 9000) {
		return fmt.Errorf("invalid [Interface].MTU: expected an integer from 576 to 9000")
	}
	if allZero(p.Peer.PublicKey[:]) {
		return fmt.Errorf("invalid [Peer].PublicKey: expected a non-zero 32-byte base64 key")
	}
	if _, err := parseEndpoint(p.Peer.Endpoint); err != nil {
		return err
	}
	if p.Interface.Obfuscation.JunkPacketMinSize > p.Interface.Obfuscation.JunkPacketMaxSize {
		return fmt.Errorf("invalid AmneziaWG obfuscation: Jmin must not exceed Jmax")
	}
	if p.Version == VersionLegacy && (p.Interface.Obfuscation.PacketJunkSizes[2] != 0 || p.Interface.Obfuscation.PacketJunkSizes[3] != 0) {
		return fmt.Errorf("invalid AmneziaWG Legacy profile: S3/S4 must be absent")
	}
	for i, specialJunk := range p.Interface.Obfuscation.SpecialJunk {
		if specialJunk == "" {
			continue
		}
		if len(specialJunk) > 2048 || strings.ContainsAny(specialJunk, "'\"\\\x00\r\n") || !strings.HasPrefix(specialJunk, "<") || !strings.HasSuffix(specialJunk, ">") {
			return fmt.Errorf("invalid [Interface].I%d: expected an AWG packet signature", i+1)
		}
		if p.Version == VersionLegacy {
			return fmt.Errorf("invalid AmneziaWG Legacy profile: I%d requires AWG 2.0", i+1)
		}
	}
	if p.Peer.PresharedKey.set && allZero(p.Peer.PresharedKey.value[:]) {
		return fmt.Errorf("invalid [Peer].PresharedKey: expected a non-zero 32-byte base64 key")
	}
	return nil
}

func parseObfuscation(values map[string]string, version string) (Obfuscation, error) {
	var result Obfuscation
	var err error
	if result.JunkPacketCount, err = parseUint16(values, "jc", "[Interface].Jc"); err != nil {
		return result, err
	}
	if result.JunkPacketMinSize, err = parseUint16(values, "jmin", "[Interface].Jmin"); err != nil {
		return result, err
	}
	if result.JunkPacketMaxSize, err = parseUint16(values, "jmax", "[Interface].Jmax"); err != nil {
		return result, err
	}
	if result.JunkPacketMinSize > result.JunkPacketMaxSize {
		return result, fmt.Errorf("invalid AmneziaWG obfuscation: Jmin must not exceed Jmax")
	}
	packetJunkCount := 2
	if version == Version20 || version == Version31 {
		packetJunkCount = len(result.PacketJunkSizes)
	}
	for i := 0; i < packetJunkCount; i++ {
		key := fmt.Sprintf("s%d", i+1)
		display := fmt.Sprintf("[Interface].S%d", i+1)
		if result.PacketJunkSizes[i], err = parseUint16(values, key, display); err != nil {
			return result, err
		}
	}
	for i := range result.MagicHeaders {
		key := fmt.Sprintf("h%d", i+1)
		display := fmt.Sprintf("[Interface].H%d", i+1)
		raw, requiredErr := required(values, key, display)
		if requiredErr != nil {
			return result, requiredErr
		}
		if result.MagicHeaders[i], err = parseUint32Range(raw, display); err != nil {
			return result, err
		}
	}
	for i := range result.SpecialJunk {
		key := fmt.Sprintf("i%d", i+1)
		display := fmt.Sprintf("[Interface].I%d", i+1)
		value := strings.TrimSpace(values[key])
		if value == "" {
			continue
		}
		if len(value) > 2048 || strings.ContainsAny(value, "'\"\\\x00\r\n") || !strings.HasPrefix(value, "<") || !strings.HasSuffix(value, ">") {
			return result, fmt.Errorf("invalid %s: expected an AWG packet signature", display)
		}
		result.SpecialJunk[i] = value
	}
	return result, nil
}

func parseV31(values map[string]string, version string) (V31Parameters, error) {
	var result V31Parameters
	keys := []string{"headerprotectionkey", "contentpaddingaddition", "rekeyaftertime", "rekeytimeout", "rejectaftertime", "keepalivetimeout", "maxhandshakeattempts", "randomtrailers", "disablecookies"}
	has := false
	for _, key := range keys {
		if strings.TrimSpace(values[key]) != "" {
			has = true
			break
		}
	}
	if !has {
		return result, nil
	}
	if version != Version31 {
		return result, fmt.Errorf("invalid AmneziaWG profile: AWG 3.1 parameters require AWG 3.1")
	}
	var err error
	if raw := strings.TrimSpace(values["headerprotectionkey"]); raw != "" {
		result.HeaderProtectionKey, err = parsePrivateKey(raw, "[Interface].HeaderProtectionKey")
		if err != nil {
			return result, err
		}
	}
	for _, item := range []struct {
		key, label string
		target     *Uint32Range
	}{
		{"contentpaddingaddition", "ContentPaddingAddition", &result.ContentPaddingAddition}, {"rekeyaftertime", "RekeyAfterTime", &result.RekeyAfterTime}, {"rekeytimeout", "RekeyTimeout", &result.RekeyTimeout}, {"rejectaftertime", "RejectAfterTime", &result.RejectAfterTime}, {"keepalivetimeout", "KeepaliveTimeout", &result.KeepaliveTimeout}, {"maxhandshakeattempts", "MaxHandshakeAttempts", &result.MaxHandshakeAttempts},
	} {
		if raw := strings.TrimSpace(values[item.key]); raw != "" {
			n, e := parseUint32Range(raw, "[Interface]."+item.label)
			if e != nil {
				return result, e
			}
			*item.target = n
		}
	}
	for _, item := range []struct {
		key, label string
		target     *bool
	}{{"randomtrailers", "RandomTrailers", &result.RandomTrailers}, {"disablecookies", "DisableCookies", &result.DisableCookies}} {
		if raw := strings.TrimSpace(values[item.key]); raw != "" {
			value, e := strconv.ParseBool(raw)
			if e != nil {
				return result, fmt.Errorf("invalid [Interface].%s: expected true or false", item.label)
			}
			*item.target = value
		}
	}
	return result, nil
}

func hasV31Parameter(values map[string]string) bool {
	for _, key := range []string{"headerprotectionkey", "contentpaddingaddition", "rekeyaftertime", "rekeytimeout", "rejectaftertime", "keepalivetimeout", "maxhandshakeattempts", "randomtrailers", "disablecookies"} {
		if strings.TrimSpace(values[key]) != "" {
			return true
		}
	}
	return false
}

func parsePrivateKey(raw, field string) (PrivateKey, error) {
	decoded, err := decodeKey(raw)
	if err != nil || allZero(decoded[:]) {
		return PrivateKey{}, fmt.Errorf("invalid %s: expected a non-zero 32-byte base64 key", field)
	}
	return PrivateKey{value: decoded, set: true}, nil
}

func parsePresharedKey(raw, field string) (PresharedKey, error) {
	decoded, err := decodeKey(raw)
	if err != nil || allZero(decoded[:]) {
		return PresharedKey{}, fmt.Errorf("invalid %s: expected a non-zero 32-byte base64 key", field)
	}
	return PresharedKey{value: decoded, set: true}, nil
}

func parsePublicKey(raw string) (PublicKey, error) {
	decoded, err := decodeKey(raw)
	if err != nil || allZero(decoded[:]) {
		return PublicKey{}, fmt.Errorf("invalid [Peer].PublicKey: expected a non-zero 32-byte base64 key")
	}
	return PublicKey(decoded), nil
}

func decodeKey(raw string) ([32]byte, error) {
	var result [32]byte
	decoded, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(raw))
	if err != nil || len(decoded) != len(result) {
		return result, fmt.Errorf("invalid key")
	}
	copy(result[:], decoded)
	return result, nil
}

func allZero(value []byte) bool {
	var combined byte
	for _, current := range value {
		combined |= current
	}
	return combined == 0
}

func parsePrefixes(raw, field string) ([]netip.Prefix, error) {
	parts := strings.Split(raw, ",")
	result := make([]netip.Prefix, 0, len(parts))
	seen := make(map[netip.Prefix]struct{}, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			address, addressErr := netip.ParseAddr(value)
			if addressErr != nil {
				return nil, fmt.Errorf("invalid %s CIDR", field)
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		if _, duplicate := seen[prefix]; duplicate {
			return nil, fmt.Errorf("invalid %s: duplicate CIDR %q", field, value)
		}
		seen[prefix] = struct{}{}
		result = append(result, prefix)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("missing required parameter %s", field)
	}
	return result, nil
}

func parseEndpoint(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	host, portRaw, err := net.SplitHostPort(value)
	if err != nil || !validEndpointHost(host) {
		return "", fmt.Errorf("invalid [Peer].Endpoint: expected host:port (IPv6 must use brackets)")
	}
	port, err := strconv.ParseUint(portRaw, 10, 16)
	if err != nil || port == 0 {
		return "", fmt.Errorf("invalid [Peer].Endpoint: port must be from 1 to 65535")
	}
	if address, parseErr := netip.ParseAddr(host); parseErr == nil {
		return net.JoinHostPort(address.String(), strconv.Itoa(int(port))), nil
	}
	return net.JoinHostPort(strings.ToLower(host), strconv.Itoa(int(port))), nil
}

func validEndpointHost(host string) bool {
	if host == "" || strings.ContainsAny(host, " \t\r\n") {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	if len(host) > 253 || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
				(character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func parseUint32Range(raw, field string) (Uint32Range, error) {
	parts := strings.Split(strings.TrimSpace(raw), "-")
	if len(parts) > 2 {
		return Uint32Range{}, fmt.Errorf("invalid %s: expected uint32 or min-max", field)
	}
	minimum, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 0, 32)
	if err != nil {
		return Uint32Range{}, fmt.Errorf("invalid %s: expected uint32 or min-max", field)
	}
	maximum := minimum
	if len(parts) == 2 {
		maximum, err = strconv.ParseUint(strings.TrimSpace(parts[1]), 0, 32)
		if err != nil || minimum > maximum {
			return Uint32Range{}, fmt.Errorf("invalid %s: expected ascending uint32 range", field)
		}
	}
	return Uint32Range{Min: uint32(minimum), Max: uint32(maximum)}, nil
}
