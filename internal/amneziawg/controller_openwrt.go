package amneziawg

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultInterfaceName = "fastlane_awg"
	ProbeInterfaceName   = "flawg_probe"
	FastLaneAWGTool      = "/usr/libexec/fastlane-amneziawg"
	FastLaneMwan3Helper  = "/usr/libexec/fastlane-awg-mwan3"
	RouteTable           = 51821
	ProbeRouteTable      = 51822
	ProbeRulePriority    = 901
	// Outside mwan3's 0x3f00 mask and Fast Lane's transparent-proxy bit.
	RouteMark = 0x10000
)

type Compatibility struct {
	Compatible    bool   `json:"compatible"`
	Kernel        string `json:"kernel,omitempty"`
	Module        string `json:"module,omitempty"`
	Runtime       string `json:"runtime,omitempty"`
	Tools         string `json:"tools,omitempty"`
	Netifd        bool   `json:"netifd"`
	FailureReason string `json:"failure_reason,omitempty"`
}

type InterfaceStatus struct {
	Up            bool   `json:"up"`
	Device        string `json:"device,omitempty"`
	Address       string `json:"address,omitempty"`
	LastHandshake int64  `json:"last_handshake,omitempty"`
}

type Controller interface {
	Preflight(context.Context) (Compatibility, error)
	Prepare(context.Context, Profile) error
	Connect(context.Context) (InterfaceStatus, error)
	Disconnect(context.Context) error
	Remove(context.Context) error
	Status(context.Context) (InterfaceStatus, error)
}

// IsolatedProbeController can measure a candidate without changing the
// interface that carries an already selected AmneziaWG connection.
type IsolatedProbeController interface {
	PrepareIsolatedProbe(context.Context, Profile) (InterfaceStatus, func(context.Context) error, error)
}

type processRunner interface {
	Run(context.Context, []byte, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

type OpenWrtController struct {
	InterfaceName string
	SysRoot       string
	Runner        processRunner
	WaitTimeout   time.Duration
}

func NewOpenWrtController() *OpenWrtController {
	return &OpenWrtController{InterfaceName: DefaultInterfaceName, SysRoot: "/", Runner: execRunner{}, WaitTimeout: 8 * time.Second}
}

func (c *OpenWrtController) Preflight(ctx context.Context) (Compatibility, error) {
	result := Compatibility{}
	kernelBytes, err := c.run(ctx, nil, "uname", "-r")
	if err != nil {
		return result, fmt.Errorf("read running kernel: %w", err)
	}
	result.Kernel = strings.TrimSpace(string(kernelBytes))
	protoPath := c.rooted("lib/netifd/proto/amneziawg.sh")
	if info, statErr := os.Stat(protoPath); statErr != nil || info.IsDir() {
		result.FailureReason = "AmneziaWG netifd protocol is not installed"
		return result, errors.New(result.FailureReason)
	}
	result.Netifd = true
	tools, err := c.run(ctx, nil, FastLaneAWGTool, "--version")
	if err != nil {
		result.FailureReason = "Fast Lane AmneziaWG tools are not installed"
		return result, errors.New(result.FailureReason)
	}
	result.Tools = strings.TrimSpace(string(tools))
	userspaceFailure := ""
	userspaceAvailable := func() bool {
		version, userspaceErr := c.run(ctx, nil, "amneziawg-go", "--version")
		if userspaceErr != nil {
			userspaceFailure = "amneziawg-go is not installed"
			return false
		}
		protoScript, readErr := os.ReadFile(protoPath)
		if readErr != nil || !bytes.Contains(protoScript, []byte("amneziawg-go")) || !bytes.Contains(protoScript, []byte(FastLaneAWGTool)) {
			userspaceFailure = "installed AmneziaWG netifd protocol does not support amneziawg-go"
			return false
		}
		if info, statErr := os.Stat(c.rooted("dev/net/tun")); statErr != nil || info.IsDir() {
			userspaceFailure = "Linux TUN device is not available"
			return false
		}
		result.Runtime = strings.TrimSpace(string(version))
		result.Compatible = true
		return true
	}
	// Fast Lane ships and owns the userspace chain. Prefer it to probing a
	// possibly absent kernel module: some BusyBox builds print diagnostics for
	// an unsupported modinfo invocation on every health check.
	if userspaceAvailable() {
		return result, nil
	}
	fallbackFailure := func(primary string) string {
		if userspaceFailure != "" {
			return primary + "; userspace fallback unavailable: " + userspaceFailure
		}
		return primary
	}
	verifiedByPackageABI := false
	vermagic, err := c.run(ctx, nil, "modinfo", "-F", "vermagic", "amneziawg")
	if err != nil {
		modulePath := c.rooted(filepath.Join("lib/modules", result.Kernel, "amneziawg.ko"))
		if info, statErr := os.Stat(modulePath); statErr != nil || info.IsDir() {
			if userspaceAvailable() {
				return result, nil
			}
			result.FailureReason = fallbackFailure("AmneziaWG kernel module is not installed for the running kernel")
			return result, errors.New(result.FailureReason)
		}
		moduleStatus, moduleErr := c.run(ctx, nil, "opkg", "status", "kmod-amneziawg")
		kernelStatus, kernelErr := c.run(ctx, nil, "opkg", "status", "kernel")
		moduleDepends := packageField(string(moduleStatus), "Depends")
		kernelVersion := packageField(string(kernelStatus), "Version")
		compactDepends := strings.NewReplacer(" ", "", "\t", "").Replace(moduleDepends)
		compactVersion := strings.NewReplacer(" ", "", "\t", "").Replace(kernelVersion)
		if moduleErr != nil || kernelErr != nil || compactVersion == "" || !strings.Contains(compactDepends, "kernel(="+compactVersion+")") {
			if userspaceAvailable() {
				return result, nil
			}
			result.FailureReason = fallbackFailure(fmt.Sprintf("cannot confirm AmneziaWG module compatibility with the installed kernel ABI (dependency %q, kernel %q)", moduleDepends, kernelVersion))
			return result, errors.New(result.FailureReason)
		}
		result.Module = "kernel ABI " + kernelVersion
		verifiedByPackageABI = true
	} else {
		result.Module = strings.TrimSpace(string(vermagic))
		if result.Module == "" || !strings.HasPrefix(result.Module, result.Kernel+" ") && result.Module != result.Kernel {
			if userspaceAvailable() {
				return result, nil
			}
			result.FailureReason = fallbackFailure(fmt.Sprintf("AmneziaWG module was built for a different kernel (running %s)", result.Kernel))
			return result, errors.New(result.FailureReason)
		}
	}
	// Full modprobe supports a dry run. BusyBox modprobe on OpenWrt does not;
	// there the package manager's exact kernel ABI dependency is the fail-closed
	// compatibility proof and successful installation proves dependencies.
	if !verifiedByPackageABI {
		if _, err := c.run(ctx, nil, "modprobe", "-n", "amneziawg"); err == nil {
			result.Runtime = "kernel"
			result.Compatible = true
			return result, nil
		}
		if userspaceAvailable() {
			return result, nil
		}
		result.FailureReason = fallbackFailure("AmneziaWG kernel module dependencies are incompatible")
		return result, errors.New(result.FailureReason)
	}
	result.Runtime = "kernel"
	result.Compatible = true
	return result, nil
}

func packageField(control, name string) string {
	prefix := strings.ToLower(name) + ":"
	for _, line := range strings.Split(control, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), prefix) {
			return strings.TrimSpace(strings.TrimSpace(line)[len(prefix):])
		}
	}
	return ""
}

func (c *OpenWrtController) Prepare(ctx context.Context, profile Profile) error {
	compatibility, err := c.Preflight(ctx)
	if err != nil {
		return err
	}
	if profile.Version == Version31 {
		// amneziawg-go v3.1 keeps an upstream legacy build string in its
		// --version output. The bundled module checksum and the AWG tools
		// version are the compatibility contract; rejecting a runnable
		// userspace runtime based on that display string blocks valid 3.1
		// profiles.
		if !strings.Contains(compatibility.Tools, "v3.1.") || compatibility.Runtime == "" {
			return fmt.Errorf("installed Fast Lane AmneziaWG runtime is incompatible with AWG 3.1")
		}
		proto, err := os.ReadFile(c.rooted("lib/netifd/proto/amneziawg.sh"))
		if err != nil || !bytes.Contains(proto, []byte("awg_header_protection_key")) || !bytes.Contains(proto, []byte("awg_random_trailers")) || !bytes.Contains(proto, []byte("awg_force_userspace")) {
			return fmt.Errorf("installed AmneziaWG netifd protocol does not support AWG 3.1")
		}
	}
	batch, err := c.uciBatch(profile)
	if err != nil {
		return err
	}
	if output, err := c.run(ctx, batch, "uci", "-q", "batch"); err != nil {
		return fmt.Errorf("stage AmneziaWG netifd config: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if output, err := c.run(ctx, nil, "uci", "commit", "network"); err != nil {
		return fmt.Errorf("commit AmneziaWG netifd config: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if output, err := c.run(ctx, nil, "ubus", "call", "network", "reload"); err != nil {
		return fmt.Errorf("reload netifd configuration: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (c *OpenWrtController) Connect(ctx context.Context) (InterfaceStatus, error) {
	if err := c.syncMwanEndpointRoute(ctx); err != nil {
		return InterfaceStatus{}, err
	}
	status, err := c.connectWithoutPolicy(ctx)
	if err != nil {
		return InterfaceStatus{}, err
	}
	if err := c.installPolicyRoutes(ctx, status); err != nil {
		_ = c.Disconnect(ctx)
		return InterfaceStatus{}, err
	}
	return status, nil
}

func (c *OpenWrtController) syncMwanEndpointRoute(ctx context.Context) error {
	if _, err := os.Stat(c.rooted(strings.TrimPrefix(FastLaneMwan3Helper, "/"))); err != nil {
		return nil
	}
	output, err := c.run(ctx, nil, FastLaneMwan3Helper, "--interface", c.interfaceName())
	if err != nil {
		return fmt.Errorf("synchronize AmneziaWG endpoint with mwan3: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (c *OpenWrtController) connectWithoutPolicy(ctx context.Context) (InterfaceStatus, error) {
	name := c.interfaceName()
	if output, err := c.run(ctx, nil, "ifup", name); err != nil {
		return InterfaceStatus{}, fmt.Errorf("bring up AmneziaWG interface: %w: %s", err, strings.TrimSpace(string(output)))
	}
	deadline := time.Now().Add(c.waitTimeout())
	for {
		status, err := c.Status(ctx)
		if err == nil && status.Up && status.Device != "" && status.Address != "" {
			return status, nil
		}
		if time.Now().After(deadline) {
			return InterfaceStatus{}, fmt.Errorf("AmneziaWG interface did not become ready")
		}
		select {
		case <-ctx.Done():
			return InterfaceStatus{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// PrepareIsolatedProbe uses a dedicated netifd interface and an output-device rule.
// Its route never shares the active AWG mark/table, so candidate checks cannot
// replace, drain, or reroute the active AWG connection.
func (c *OpenWrtController) PrepareIsolatedProbe(ctx context.Context, profile Profile) (InterfaceStatus, func(context.Context) error, error) {
	probe := *c
	probe.InterfaceName = ProbeInterfaceName
	cleanup := func(cleanupCtx context.Context) error {
		return probe.removeIsolatedProbe(cleanupCtx)
	}
	if err := cleanup(ctx); err != nil {
		return InterfaceStatus{}, cleanup, err
	}
	if err := probe.Prepare(ctx, profile); err != nil {
		return InterfaceStatus{}, cleanup, err
	}
	status, err := probe.connectWithoutPolicy(ctx)
	if err != nil {
		_ = cleanup(context.Background())
		return InterfaceStatus{}, cleanup, err
	}
	address, parseErr := netip.ParseAddr(status.Address)
	if parseErr != nil {
		_ = cleanup(context.Background())
		return InterfaceStatus{}, cleanup, fmt.Errorf("invalid isolated AmneziaWG address %q", status.Address)
	}
	family := "-4"
	if address.Is6() {
		family = "-6"
	}
	if output, routeErr := probe.run(ctx, nil, "ip", family, "route", "replace", "default", "dev", status.Device, "table", strconv.Itoa(ProbeRouteTable)); routeErr != nil {
		_ = cleanup(context.Background())
		return InterfaceStatus{}, cleanup, fmt.Errorf("install isolated AmneziaWG route: %w: %s", routeErr, strings.TrimSpace(string(output)))
	}
	_, _ = probe.run(ctx, nil, "ip", family, "rule", "del", "oif", status.Device, "table", strconv.Itoa(ProbeRouteTable), "priority", strconv.Itoa(ProbeRulePriority))
	if output, ruleErr := probe.run(ctx, nil, "ip", family, "rule", "add", "oif", status.Device, "table", strconv.Itoa(ProbeRouteTable), "priority", strconv.Itoa(ProbeRulePriority)); ruleErr != nil {
		_ = cleanup(context.Background())
		return InterfaceStatus{}, cleanup, fmt.Errorf("install isolated AmneziaWG source rule: %w: %s", ruleErr, strings.TrimSpace(string(output)))
	}
	return status, cleanup, nil
}

func (c *OpenWrtController) removeIsolatedProbe(ctx context.Context) error {
	name := c.interfaceName()
	for _, family := range []string{"-4", "-6"} {
		_, _ = c.run(ctx, nil, "ip", family, "rule", "del", "oif", name, "table", strconv.Itoa(ProbeRouteTable), "priority", strconv.Itoa(ProbeRulePriority))
		_, _ = c.run(ctx, nil, "ip", family, "route", "flush", "table", strconv.Itoa(ProbeRouteTable))
	}
	status, _ := c.Status(ctx)
	if status.Address != "" {
		family := "-4"
		if address, err := netip.ParseAddr(status.Address); err == nil && address.Is6() {
			family = "-6"
		}
		// Remove the source rule left by 0.1.48 as well.
		_, _ = c.run(ctx, nil, "ip", family, "rule", "del", "from", status.Address, "table", strconv.Itoa(ProbeRouteTable), "priority", "10901")
		_, _ = c.run(ctx, nil, "ip", family, "route", "flush", "table", strconv.Itoa(ProbeRouteTable))
	}
	_, _ = c.run(ctx, nil, "ifdown", name)
	batch := []byte("delete network." + name + "\ndelete network.amneziawg_" + name + "\n")
	if output, err := c.run(ctx, batch, "uci", "-q", "batch"); err != nil {
		return fmt.Errorf("remove isolated AmneziaWG config: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if _, err := c.run(ctx, nil, "uci", "commit", "network"); err != nil {
		return fmt.Errorf("commit removed isolated AmneziaWG config: %w", err)
	}
	return nil
}

func (c *OpenWrtController) Disconnect(ctx context.Context) error {
	name := c.interfaceName()
	for _, family := range []string{"-4", "-6"} {
		_, _ = c.run(ctx, nil, "ip", family, "rule", "del", "oif", name, "table", strconv.Itoa(RouteTable), "priority", "900")
		for _, mark := range []string{"0x200", "0x400"} {
			_, _ = c.run(ctx, nil, "ip", family, "rule", "del", "fwmark", mark, "table", strconv.Itoa(RouteTable), "priority", "10900")
		}
	}
	_, _ = c.run(ctx, nil, "ip", "-4", "rule", "del", "fwmark", fmt.Sprintf("0x%x", RouteMark), "table", strconv.Itoa(RouteTable), "priority", "10900")
	_, _ = c.run(ctx, nil, "ip", "-6", "rule", "del", "fwmark", fmt.Sprintf("0x%x", RouteMark), "table", strconv.Itoa(RouteTable), "priority", "10900")
	_, _ = c.run(ctx, nil, "ip", "-4", "route", "flush", "table", strconv.Itoa(RouteTable))
	_, _ = c.run(ctx, nil, "ip", "-6", "route", "flush", "table", strconv.Itoa(RouteTable))
	if output, err := c.run(ctx, nil, "ifdown", name); err != nil && !strings.Contains(strings.ToLower(string(output)), "not found") {
		return fmt.Errorf("bring down AmneziaWG interface: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (c *OpenWrtController) Remove(ctx context.Context) error {
	if err := c.Disconnect(ctx); err != nil {
		return err
	}
	name := c.interfaceName()
	batch := []byte("delete network." + name + "\ndelete network.amneziawg_" + name + "\n")
	if output, err := c.run(ctx, batch, "uci", "-q", "batch"); err != nil {
		return fmt.Errorf("remove AmneziaWG netifd config: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if _, err := c.run(ctx, nil, "uci", "commit", "network"); err != nil {
		return fmt.Errorf("commit removed AmneziaWG config: %w", err)
	}
	return nil
}

func (c *OpenWrtController) Status(ctx context.Context) (InterfaceStatus, error) {
	output, err := c.run(ctx, nil, "ubus", "call", "network.interface."+c.interfaceName(), "status")
	if err != nil {
		return InterfaceStatus{}, err
	}
	var raw struct {
		Up     bool   `json:"up"`
		Device string `json:"l3_device"`
		IPv4   []struct {
			Address string `json:"address"`
		} `json:"ipv4-address"`
		IPv6 []struct {
			Address string `json:"address"`
		} `json:"ipv6-address"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return InterfaceStatus{}, fmt.Errorf("decode AmneziaWG interface status: %w", err)
	}
	status := InterfaceStatus{Up: raw.Up, Device: raw.Device}
	if len(raw.IPv4) > 0 {
		status.Address = raw.IPv4[0].Address
	} else if len(raw.IPv6) > 0 {
		status.Address = raw.IPv6[0].Address
	}
	if raw.Up {
		if handshakes, showErr := c.run(ctx, nil, FastLaneAWGTool, "show", c.interfaceName(), "latest-handshakes"); showErr == nil {
			for _, field := range strings.Fields(string(handshakes)) {
				if value, parseErr := strconv.ParseInt(field, 10, 64); parseErr == nil && value > status.LastHandshake {
					status.LastHandshake = value
				}
			}
		}
	}
	return status, nil
}

func (c *OpenWrtController) installPolicyRoutes(ctx context.Context, status InterfaceStatus) error {
	address, err := netip.ParseAddr(status.Address)
	if err != nil {
		return fmt.Errorf("invalid AmneziaWG runtime address %q", status.Address)
	}
	family := "-4"
	if address.Is6() {
		family = "-6"
	}
	if output, err := c.run(ctx, nil, "ip", family, "route", "replace", "default", "dev", status.Device, "table", strconv.Itoa(RouteTable)); err != nil {
		return fmt.Errorf("install AmneziaWG route table: %w: %s", err, strings.TrimSpace(string(output)))
	}
	// Delete first so repeated preparation remains idempotent.
	_, _ = c.run(ctx, nil, "ip", family, "rule", "del", "oif", status.Device, "table", strconv.Itoa(RouteTable), "priority", "900")
	if output, err := c.run(ctx, nil, "ip", family, "rule", "add", "oif", status.Device, "table", strconv.Itoa(RouteTable), "priority", "900"); err != nil {
		return fmt.Errorf("install AmneziaWG policy rule: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// EnsurePolicyRoutes also repairs policy rules when upgrading an already-up tunnel.
func (c *OpenWrtController) EnsurePolicyRoutes(ctx context.Context, status InterfaceStatus) error {
	if err := c.syncMwanEndpointRoute(ctx); err != nil {
		return err
	}
	return c.installPolicyRoutes(ctx, status)
}

func (c *OpenWrtController) uciBatch(profile Profile) ([]byte, error) {
	if (profile.Version != VersionLegacy && profile.Version != Version20 && profile.Version != Version31) || len(profile.Interface.Addresses) == 0 {
		return nil, fmt.Errorf("invalid AmneziaWG runtime profile")
	}
	host, port, err := net.SplitHostPort(profile.Peer.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid AmneziaWG endpoint: %w", err)
	}
	name := c.interfaceName()
	peerSection := "amneziawg_" + name
	var lines []string
	set := func(section, key, value string) {
		lines = append(lines, "set network."+section+"."+key+"="+uciQuote(value))
	}
	addList := func(section, key, value string) {
		lines = append(lines, "add_list network."+section+"."+key+"="+uciQuote(value))
	}
	lines = append(lines, "delete network."+name, "delete network."+peerSection, "set network."+name+"=interface")
	set(name, "proto", "amneziawg")
	set(name, "private_key", profile.Interface.PrivateKey.base64())
	// Fast Lane owns this host route and synchronizes it with mwan3's selected
	// failover member. netifd's one-time dependency otherwise remains pinned to
	// the uplink that happened to be active when the tunnel was created.
	set(name, "nohostroute", "1")
	if profile.Interface.MTU > 0 {
		set(name, "mtu", strconv.Itoa(int(profile.Interface.MTU)))
	}
	for _, address := range profile.Interface.Addresses {
		addList(name, "addresses", address.String())
	}
	o := profile.Interface.Obfuscation
	set(name, "awg_jc", strconv.Itoa(int(o.JunkPacketCount)))
	set(name, "awg_jmin", strconv.Itoa(int(o.JunkPacketMinSize)))
	set(name, "awg_jmax", strconv.Itoa(int(o.JunkPacketMaxSize)))
	packetJunkCount := 2
	if profile.Version == Version20 || profile.Version == Version31 {
		packetJunkCount = len(o.PacketJunkSizes)
	}
	for index := 0; index < packetJunkCount; index++ {
		value := o.PacketJunkSizes[index]
		set(name, fmt.Sprintf("awg_s%d", index+1), strconv.Itoa(int(value)))
	}
	for index, value := range o.MagicHeaders {
		set(name, fmt.Sprintf("awg_h%d", index+1), formatRange(value))
	}
	if profile.Version == Version20 || profile.Version == Version31 {
		for index, value := range o.SpecialJunk {
			if value != "" {
				set(name, fmt.Sprintf("awg_i%d", index+1), value)
			}
		}
	}
	if profile.Version == Version31 {
		set(name, "awg_force_userspace", "1")
		v := profile.Interface.V31
		if v.HeaderProtectionKey.present() {
			set(name, "awg_header_protection_key", v.HeaderProtectionKey.base64())
		}
		set(name, "awg_content_padding_addition", formatRange(v.ContentPaddingAddition))
		set(name, "awg_rekey_after_time", formatRange(v.RekeyAfterTime))
		set(name, "awg_rekey_timeout", formatRange(v.RekeyTimeout))
		set(name, "awg_reject_after_time", formatRange(v.RejectAfterTime))
		set(name, "awg_keepalive_timeout", formatRange(v.KeepaliveTimeout))
		set(name, "awg_max_handshake_attempts", formatRange(v.MaxHandshakeAttempts))
		set(name, "awg_random_trailers", strconv.FormatBool(v.RandomTrailers))
		set(name, "awg_disable_cookies", strconv.FormatBool(v.DisableCookies))
	}
	lines = append(lines, "set network."+peerSection+"="+peerSection)
	set(peerSection, "public_key", profile.Peer.PublicKey.String())
	if profile.Peer.PresharedKey.present() {
		set(peerSection, "preshared_key", profile.Peer.PresharedKey.base64())
	}
	set(peerSection, "endpoint_host", host)
	set(peerSection, "endpoint_port", port)
	set(peerSection, "route_allowed_ips", "0")
	addList(peerSection, "allowed_ips", "0.0.0.0/0")
	if hasIPv6Address(profile.Interface.Addresses) {
		addList(peerSection, "allowed_ips", "::/0")
	}
	if profile.Peer.PersistentKeepalive.Max > 0 {
		set(peerSection, "persistent_keepalive", formatRange(profile.Peer.PersistentKeepalive))
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func (k PrivateKey) base64() string   { return encodeKey(k.value) }
func (k PresharedKey) base64() string { return encodeKey(k.value) }
func encodeKey(value [32]byte) string { return base64.StdEncoding.EncodeToString(value[:]) }

func formatRange(value Uint32Range) string {
	if value.Min == value.Max {
		return strconv.FormatUint(uint64(value.Min), 10)
	}
	return strconv.FormatUint(uint64(value.Min), 10) + "-" + strconv.FormatUint(uint64(value.Max), 10)
}

func hasIPv6Address(addresses []netip.Prefix) bool {
	for _, address := range addresses {
		if address.Addr().Is6() {
			return true
		}
	}
	return false
}

func uciQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func (c *OpenWrtController) interfaceName() string {
	if strings.TrimSpace(c.InterfaceName) == "" {
		return DefaultInterfaceName
	}
	return c.InterfaceName
}
func (c *OpenWrtController) waitTimeout() time.Duration {
	if c.WaitTimeout <= 0 {
		return 8 * time.Second
	}
	return c.WaitTimeout
}
func (c *OpenWrtController) rooted(path string) string {
	root := c.SysRoot
	if root == "" {
		root = "/"
	}
	return filepath.Join(root, path)
}
func (c *OpenWrtController) run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	runner := c.Runner
	if runner == nil {
		runner = execRunner{}
	}
	return runner.Run(ctx, stdin, name, args...)
}
