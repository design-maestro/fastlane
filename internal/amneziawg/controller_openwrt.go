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
	RouteTable           = 51821
	RouteMark            = 0x200
)

type Compatibility struct {
	Compatible    bool   `json:"compatible"`
	Kernel        string `json:"kernel,omitempty"`
	Module        string `json:"module,omitempty"`
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
	tools, err := c.run(ctx, nil, "awg", "--version")
	if err != nil {
		result.FailureReason = "amneziawg-tools are not installed"
		return result, errors.New(result.FailureReason)
	}
	result.Tools = strings.TrimSpace(string(tools))
	verifiedByPackageABI := false
	vermagic, err := c.run(ctx, nil, "modinfo", "-F", "vermagic", "amneziawg")
	if err != nil {
		modulePath := c.rooted(filepath.Join("lib/modules", result.Kernel, "amneziawg.ko"))
		if info, statErr := os.Stat(modulePath); statErr != nil || info.IsDir() {
			result.FailureReason = "AmneziaWG kernel module is not installed for the running kernel"
			return result, errors.New(result.FailureReason)
		}
		moduleStatus, moduleErr := c.run(ctx, nil, "opkg", "status", "kmod-amneziawg")
		kernelStatus, kernelErr := c.run(ctx, nil, "opkg", "status", "kernel")
		moduleDepends := packageField(string(moduleStatus), "Depends")
		kernelVersion := packageField(string(kernelStatus), "Version")
		compactDepends := strings.NewReplacer(" ", "", "\t", "").Replace(moduleDepends)
		compactVersion := strings.NewReplacer(" ", "", "\t", "").Replace(kernelVersion)
		if moduleErr != nil || kernelErr != nil || compactVersion == "" || !strings.Contains(compactDepends, "kernel(="+compactVersion+")") {
			result.FailureReason = fmt.Sprintf("cannot confirm AmneziaWG module compatibility with the installed kernel ABI (dependency %q, kernel %q)", moduleDepends, kernelVersion)
			return result, errors.New(result.FailureReason)
		}
		result.Module = "kernel ABI " + kernelVersion
		verifiedByPackageABI = true
	} else {
		result.Module = strings.TrimSpace(string(vermagic))
		if result.Module == "" || !strings.HasPrefix(result.Module, result.Kernel+" ") && result.Module != result.Kernel {
			result.FailureReason = fmt.Sprintf("AmneziaWG module was built for a different kernel (running %s)", result.Kernel)
			return result, errors.New(result.FailureReason)
		}
	}
	// Full modprobe supports a dry run. BusyBox modprobe on OpenWrt does not;
	// there the package manager's exact kernel ABI dependency is the fail-closed
	// compatibility proof and successful installation proves dependencies.
	if !verifiedByPackageABI {
		if _, err := c.run(ctx, nil, "modprobe", "-n", "amneziawg"); err == nil {
			result.Compatible = true
			return result, nil
		}
		result.FailureReason = "AmneziaWG kernel module dependencies are incompatible"
		return result, errors.New(result.FailureReason)
	}
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
	if _, err := c.Preflight(ctx); err != nil {
		return err
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
	name := c.interfaceName()
	if output, err := c.run(ctx, nil, "ifup", name); err != nil {
		return InterfaceStatus{}, fmt.Errorf("bring up AmneziaWG interface: %w: %s", err, strings.TrimSpace(string(output)))
	}
	deadline := time.Now().Add(c.waitTimeout())
	for {
		status, err := c.Status(ctx)
		if err == nil && status.Up && status.Device != "" && status.Address != "" {
			if err := c.installPolicyRoutes(ctx, status); err != nil {
				_ = c.Disconnect(ctx)
				return InterfaceStatus{}, err
			}
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

func (c *OpenWrtController) Disconnect(ctx context.Context) error {
	name := c.interfaceName()
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
		if handshakes, showErr := c.run(ctx, nil, "awg", "show", c.interfaceName(), "latest-handshakes"); showErr == nil {
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
	_, _ = c.run(ctx, nil, "ip", family, "rule", "del", "fwmark", fmt.Sprintf("0x%x", RouteMark), "table", strconv.Itoa(RouteTable), "priority", "10900")
	if output, err := c.run(ctx, nil, "ip", family, "rule", "add", "fwmark", fmt.Sprintf("0x%x", RouteMark), "table", strconv.Itoa(RouteTable), "priority", "10900"); err != nil {
		return fmt.Errorf("install AmneziaWG policy rule: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (c *OpenWrtController) uciBatch(profile Profile) ([]byte, error) {
	if (profile.Version != VersionLegacy && profile.Version != Version20) || len(profile.Interface.Addresses) == 0 {
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
	set(name, "nohostroute", "0")
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
	if profile.Version == Version20 {
		packetJunkCount = len(o.PacketJunkSizes)
	}
	for index := 0; index < packetJunkCount; index++ {
		value := o.PacketJunkSizes[index]
		set(name, fmt.Sprintf("awg_s%d", index+1), strconv.Itoa(int(value)))
	}
	for index, value := range o.MagicHeaders {
		set(name, fmt.Sprintf("awg_h%d", index+1), formatRange(value))
	}
	if profile.Version == Version20 {
		for index, value := range o.SpecialJunk {
			if value != "" {
				set(name, fmt.Sprintf("awg_i%d", index+1), value)
			}
		}
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
	if profile.Peer.PersistentKeepalive > 0 {
		set(peerSection, "persistent_keepalive", strconv.Itoa(int(profile.Peer.PersistentKeepalive)))
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
