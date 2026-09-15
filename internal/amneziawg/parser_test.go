package amneziawg

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

var (
	testPrivateKey = base64.StdEncoding.EncodeToString(sequence(1))
	testPublicKey  = base64.StdEncoding.EncodeToString(sequence(33))
	testPSK        = base64.StdEncoding.EncodeToString(sequence(65))
)

func TestParseValidAWG20Profile(t *testing.T) {
	t.Parallel()

	profile, err := Parse([]byte(validProfile("")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if profile.Version != Version20 {
		t.Fatalf("version = %q, want %q", profile.Version, Version20)
	}
	gotAddresses := []string{profile.Interface.Addresses[0].String(), profile.Interface.Addresses[1].String()}
	if want := []string{"10.8.0.2/32", "fd00::2/128"}; !reflect.DeepEqual(gotAddresses, want) {
		t.Fatalf("addresses = %#v, want %#v", gotAddresses, want)
	}
	if got := profile.Peer.Endpoint; got != "vpn.example.com:51820" {
		t.Fatalf("endpoint = %q", got)
	}
	if got := profile.Peer.PublicKey.String(); got != testPublicKey {
		t.Fatalf("public key = %q, want %q", got, testPublicKey)
	}
	if got := profile.Interface.Obfuscation.MagicHeaders[0]; got != (Uint32Range{Min: 100, Max: 200}) {
		t.Fatalf("H1 = %#v", got)
	}
	if got := profile.Interface.Obfuscation.SpecialJunk[4]; got != "<b 0x0506><r 5>" {
		t.Fatalf("I5 = %q", got)
	}
	if want := []string{"[Interface].DNS", "[Interface].Table", "[Peer].AllowedIPs"}; !reflect.DeepEqual(profile.IgnoredParameters, want) {
		t.Fatalf("ignored = %#v, want %#v", profile.IgnoredParameters, want)
	}
}

func TestParseAllowsCaseInsensitiveSectionsAndKeys(t *testing.T) {
	t.Parallel()

	input := strings.ReplaceAll(validProfile(""), "[Interface]", "[interface]")
	input = strings.ReplaceAll(input, "[Peer]", "[PEER]")
	input = strings.Replace(input, "PrivateKey", "privatekey", 1)
	if _, err := ParseConfig([]byte(input)); err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
}

func TestParseAcceptsExplicitVersion20AndInlineComments(t *testing.T) {
	t.Parallel()

	input := validProfile("Version = 2.0")
	input = strings.Replace(input, "Address = 10.8.0.2/32, fd00::2/128", "Address = 10.8.0.2/32, fd00::2/128 # local addresses", 1)
	input = strings.Replace(input, "Endpoint = vpn.example.com:51820", "Endpoint = vpn.example.com:51820 # server", 1)
	if _, err := Parse([]byte(input)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

func TestParseRejectsExplicitUnsupportedVersion(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(validProfile("ProtocolVersion = 3.0")))
	if !errors.Is(err, ErrUnsupportedVersion) || !strings.Contains(err.Error(), "3.0") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseRequiresExactlyOneInterfaceAndPeer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing peer", input: strings.Split(validProfile(""), "[Peer]")[0], want: "exactly one"},
		{name: "duplicate peer", input: validProfile("") + "\n[Peer]\n", want: "exactly one [Peer]"},
		{name: "duplicate interface", input: validProfile("") + "\n[Interface]\n", want: "exactly one [Interface]"},
		{name: "parameter before section", input: "PrivateKey = no\n" + validProfile(""), want: "before a section"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(test.input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestParseRejectsEveryLifecycleHook(t *testing.T) {
	t.Parallel()

	for _, directive := range []string{"PreUp", "PostUp", "PreDown", "PostDown"} {
		directive := directive
		t.Run(directive, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(validProfile(directive + " = touch /tmp/owned")))
			if !errors.Is(err, ErrUnsafeDirective) || !strings.Contains(err.Error(), directive) {
				t.Fatalf("error = %v, want ErrUnsafeDirective naming %s", err, directive)
			}
		})
	}
}

func TestParseAcceptsSafeMTU(t *testing.T) {
	t.Parallel()

	profile, err := Parse([]byte(validProfile("MTU = 1280")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if profile.Interface.MTU != 1280 {
		t.Fatalf("MTU = %d", profile.Interface.MTU)
	}
}

func TestParseRejectsUnsafeMTU(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(validProfile("MTU = 64")))
	if err == nil || !strings.Contains(err.Error(), "MTU") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseRejectsAWG3ParametersAsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	for _, parameter := range []string{
		"HeaderProtectionKey", "ContentPaddingAddition", "RekeyAfterTime",
		"RekeyTimeout", "RejectAfterTime", "KeepaliveTimeout",
		"MaxHandshakeAttempts", "RandomTrailers", "DisableCookies",
	} {
		parameter := parameter
		t.Run(parameter, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(validProfile(parameter + " = 1")))
			if !errors.Is(err, ErrUnsupportedVersion) || !strings.Contains(err.Error(), parameter) || !strings.Contains(err.Error(), "3.x") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestParseRejectsOlderAWGVersion(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"S3", "S4"} {
		field := field
		t.Run("without "+field, func(t *testing.T) {
			t.Parallel()
			input := removeLine(validProfile(""), field+" =")
			_, err := Parse([]byte(input))
			if !errors.Is(err, ErrUnsupportedVersion) || !strings.Contains(err.Error(), field) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestParseRequiresAllAWG20ObfuscationFields(t *testing.T) {
	t.Parallel()

	fields := []string{"Jc", "Jmin", "Jmax", "S1", "S2", "H1", "H2", "H3", "H4"}
	for _, field := range fields {
		field := field
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(removeLine(validProfile(""), field+" =")))
			if err == nil || !strings.Contains(err.Error(), "[Interface]."+field) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestParseAcceptsAWG20WithoutOptionalSpecialJunk(t *testing.T) {
	input := validProfile("")
	for _, field := range []string{"I1", "I2", "I3", "I4", "I5"} {
		input = removeLine(input, field+" =")
	}
	profile, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if profile.Version != Version20 {
		t.Fatalf("version = %q", profile.Version)
	}
}

func TestParseRejectsUnsafeSpecialJunkSyntax(t *testing.T) {
	for _, value := range []string{"not-a-signature", "<b 0x01>'bad", `<b 0x01>\"bad`} {
		_, err := Parse([]byte(replaceValue(validProfile(""), "I1", value)))
		if err == nil || !strings.Contains(err.Error(), "[Interface].I1") {
			t.Fatalf("value=%q error=%v", value, err)
		}
	}
}

func TestParseValidatesKeysWithoutEchoingPrivateKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		field       string
		replacement string
	}{
		{name: "private malformed", field: "PrivateKey", replacement: "not-a-secret-key"},
		{name: "private wrong length", field: "PrivateKey", replacement: base64.StdEncoding.EncodeToString([]byte("short"))},
		{name: "private zero", field: "PrivateKey", replacement: base64.StdEncoding.EncodeToString(make([]byte, 32))},
		{name: "public malformed", field: "PublicKey", replacement: "not-a-public-key"},
		{name: "public zero", field: "PublicKey", replacement: base64.StdEncoding.EncodeToString(make([]byte, 32))},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := replaceValue(validProfile(""), test.field, test.replacement)
			_, err := Parse([]byte(input))
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("error = %v", err)
			}
			if strings.Contains(err.Error(), test.replacement) {
				t.Fatalf("error exposed key input: %v", err)
			}
		})
	}
}

func TestParseValidatesAddressCIDRs(t *testing.T) {
	t.Parallel()

	for _, address := range []string{"10.8.0.2", "10.8.0.2/33", "10.8.0.2/32, 10.8.0.2/32", ""} {
		address := address
		t.Run(address, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(replaceValue(validProfile(""), "Address", address)))
			if err == nil || !strings.Contains(err.Error(), "Address") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestParseValidatesAndNormalizesEndpoint(t *testing.T) {
	t.Parallel()

	valid := map[string]string{
		"VPN.Example.COM:51820": "vpn.example.com:51820",
		"192.0.2.1:53":          "192.0.2.1:53",
		"[2001:db8::1]:65535":   "[2001:db8::1]:65535",
	}
	for input, want := range valid {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			profile, err := Parse([]byte(replaceValue(validProfile(""), "Endpoint", input)))
			if err != nil || profile.Peer.Endpoint != want {
				t.Fatalf("endpoint = %q, error = %v, want %q", profile.Peer.Endpoint, err, want)
			}
		})
	}
	for _, endpoint := range []string{"vpn.example.com", "vpn.example.com:0", "vpn.example.com:65536", "2001:db8::1:51820", "bad_host:80", ":80"} {
		endpoint := endpoint
		t.Run("invalid "+endpoint, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(replaceValue(validProfile(""), "Endpoint", endpoint)))
			if err == nil || !strings.Contains(err.Error(), "Endpoint") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestParseValidatesObfuscationNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
		value string
	}{
		{name: "uint16 overflow", field: "S1", value: "65536"},
		{name: "negative", field: "Jc", value: "-1"},
		{name: "descending junk range", field: "Jmin", value: "101"},
		{name: "uint32 overflow", field: "H1", value: "4294967296"},
		{name: "descending header range", field: "H1", value: "200-100"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(replaceValue(validProfile(""), test.field, test.value)))
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestStatusAndFormattingNeverExposeSecretKeys(t *testing.T) {
	t.Parallel()

	input := strings.Replace(validProfile(""), "[Peer]\n", "[Peer]\nPresharedKey = "+testPSK+"\n", 1)
	profile, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	status := profile.Status()
	if status.PrivateKey != redacted || status.PresharedKey != redacted {
		t.Fatalf("unexpected redaction: %#v", status)
	}
	if status.PeerPublicKey == testPublicKey || !strings.Contains(status.PeerPublicKey, "...") {
		t.Fatalf("public key was not abbreviated: %q", status.PeerPublicKey)
	}

	statusJSON, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	profileJSON, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	outputs := []string{string(statusJSON), string(profileJSON), profile.String(), fmt.Sprintf("%+v", profile), fmt.Sprintf("%#v", profile)}
	for _, output := range outputs {
		for _, secret := range []string{testPrivateKey, testPSK} {
			if strings.Contains(output, secret) {
				t.Fatalf("output exposed secret %q: %s", secret, output)
			}
		}
	}
}

func TestParseRejectsDuplicateParameters(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(validProfile("Address = 10.0.0.3/32")))
	if err == nil || !strings.Contains(err.Error(), "duplicate parameter \"Address\"") {
		t.Fatalf("error = %v", err)
	}
}

func validProfile(extraInterface string) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = 10.8.0.2/32, fd00::2/128
DNS = 1.1.1.1
Table = 123
Jc = 4
Jmin = 20
Jmax = 100
S1 = 10
S2 = 20
S3 = 30
S4 = 40
H1 = 100-200
H2 = 201
H3 = 202
H4 = 203
I1 = <b 0x0102><c>
I2 = <b 0x0203><t>
I3 = <b 0x0304><r 3>
I4 = <b 0x0405><r 4>
I5 = <b 0x0506><r 5>
%s

[Peer]
PublicKey = %s
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = vpn.example.com:51820
PersistentKeepalive = 25
`, testPrivateKey, extraInterface, testPublicKey)
}

func sequence(start byte) []byte {
	value := make([]byte, 32)
	for i := range value {
		value[i] = start + byte(i)
	}
	return value
}

func removeLine(input, prefix string) string {
	lines := strings.Split(input, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func replaceValue(input, key, value string) string {
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, key+" =") {
			lines[i] = key + " = " + value
			break
		}
	}
	return strings.Join(lines, "\n")
}
