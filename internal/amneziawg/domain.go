// Package amneziawg parses and validates safe, single-peer AmneziaWG
// profiles. It deliberately does not apply DNS, routes, or lifecycle hooks.
package amneziawg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

const (
	VersionLegacy = "legacy"
	Version20     = "2.0"
	redacted      = "[redacted]"
)

// PrivateKey stores private key material without giving it a printable or
// serializable representation. Its bytes remain package-private.
type PrivateKey struct {
	value [32]byte
	set   bool
}

func (k PrivateKey) String() string { return redacted }

func (k PrivateKey) GoString() string { return redacted }

func (k PrivateKey) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

func (k PrivateKey) present() bool { return k.set }

// PresharedKey stores optional preshared key material with the same redaction
// guarantees as PrivateKey.
type PresharedKey struct {
	value [32]byte
	set   bool
}

func (k PresharedKey) String() string { return redacted }

func (k PresharedKey) GoString() string { return redacted }

func (k PresharedKey) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

func (k PresharedKey) present() bool { return k.set }

// PublicKey is a validated WireGuard public key.
type PublicKey [32]byte

// String returns the canonical base64 representation of a public key.
func (k PublicKey) String() string {
	return base64.StdEncoding.EncodeToString(k[:])
}

// Uint32Range is an inclusive AWG header-value range. A scalar has Min == Max.
type Uint32Range struct {
	Min uint32 `json:"min"`
	Max uint32 `json:"max"`
}

// Obfuscation contains the supported AWG obfuscation parameter set. Legacy
// profiles leave the S3/S4 slots and special-junk signatures empty.
type Obfuscation struct {
	JunkPacketCount   uint16         `json:"junk_packet_count"`
	JunkPacketMinSize uint16         `json:"junk_packet_min_size"`
	JunkPacketMaxSize uint16         `json:"junk_packet_max_size"`
	PacketJunkSizes   [4]uint16      `json:"packet_junk_sizes"`
	MagicHeaders      [4]Uint32Range `json:"magic_headers"`
	SpecialJunk       [5]string      `json:"special_junk"`
}

// Interface describes the safe, runtime-relevant part of [Interface].
type Interface struct {
	PrivateKey  PrivateKey     `json:"private_key"`
	Addresses   []netip.Prefix `json:"addresses"`
	MTU         uint16         `json:"mtu,omitempty"`
	Obfuscation Obfuscation    `json:"obfuscation"`
}

// Peer describes the safe, runtime-relevant part of the single [Peer].
type Peer struct {
	PublicKey           PublicKey    `json:"public_key"`
	Endpoint            string       `json:"endpoint"`
	PersistentKeepalive uint16       `json:"persistent_keepalive,omitempty"`
	PresharedKey        PresharedKey `json:"preshared_key,omitempty"`
}

// Profile is a validated, single-interface, single-peer AWG profile.
// IgnoredParameters records DNS/routing inputs that were intentionally not
// retained for application.
type Profile struct {
	Version           string    `json:"version"`
	Interface         Interface `json:"interface"`
	Peer              Peer      `json:"peer"`
	IgnoredParameters []string  `json:"ignored_parameters,omitempty"`
}

// RedactedStatus is safe for status output and structured logs. It never
// contains private or preshared key material and abbreviates the public key.
type RedactedStatus struct {
	Version           string   `json:"version"`
	Addresses         []string `json:"addresses"`
	MTU               uint16   `json:"mtu,omitempty"`
	Endpoint          string   `json:"endpoint"`
	PeerPublicKey     string   `json:"peer_public_key"`
	PrivateKey        string   `json:"private_key"`
	PresharedKey      string   `json:"preshared_key,omitempty"`
	IgnoredParameters []string `json:"ignored_parameters,omitempty"`
}

// Status returns a projection suitable for user-visible diagnostics.
func (p Profile) Status() RedactedStatus {
	addresses := make([]string, len(p.Interface.Addresses))
	for i, address := range p.Interface.Addresses {
		addresses[i] = address.String()
	}

	ignored := append([]string(nil), p.IgnoredParameters...)
	sort.Strings(ignored)
	status := RedactedStatus{
		Version:           p.Version,
		Addresses:         addresses,
		MTU:               p.Interface.MTU,
		Endpoint:          p.Peer.Endpoint,
		PeerPublicKey:     abbreviatePublicKey(p.Peer.PublicKey.String()),
		PrivateKey:        redacted,
		IgnoredParameters: ignored,
	}
	if p.Peer.PresharedKey.present() {
		status.PresharedKey = redacted
	}
	return status
}

// String intentionally renders only the redacted status projection.
func (p Profile) String() string {
	encoded, err := json.Marshal(p.Status())
	if err != nil {
		return fmt.Sprintf("amneziawg profile version=%s", p.Version)
	}
	return string(encoded)
}

func abbreviatePublicKey(value string) string {
	if len(value) <= 12 {
		return value
	}
	return strings.Join([]string{value[:8], value[len(value)-4:]}, "...")
}
