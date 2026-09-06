package parser

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/design-maestro/fastlane/internal/domain"
	"gopkg.in/yaml.v3"
)

type yamlProxyFile struct {
	Proxies []map[string]any `yaml:"proxies"`
	Payload []map[string]any `yaml:"payload"`
}

func tryParseYAMLNodes(input, provider string) ([]domain.Node, bool, error) {
	if !strings.Contains(input, "proxies:") && !strings.Contains(input, "payload:") {
		return nil, false, nil
	}
	var file yamlProxyFile
	if err := yaml.Unmarshal([]byte(input), &file); err != nil {
		return nil, true, fmt.Errorf("decode YAML: %w", err)
	}
	entries := file.Proxies
	if len(entries) == 0 {
		entries = file.Payload
	}
	if len(entries) == 0 {
		return nil, true, fmt.Errorf("YAML has no proxies or payload entries")
	}

	nodes := make([]domain.Node, 0, len(entries))
	var lastErr error
	for index, entry := range entries {
		node, err := yamlProxyNode(entry, provider)
		if err != nil {
			lastErr = fmt.Errorf("YAML proxy %d: %w", index+1, err)
			continue
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		if lastErr != nil {
			return nil, true, lastErr
		}
		return nil, true, fmt.Errorf("YAML has no supported proxies")
	}
	return nodes, true, nil
}

func yamlProxyNode(entry map[string]any, provider string) (domain.Node, error) {
	typeName := strings.ToLower(yamlString(entry, "type"))
	protocol := domain.Protocol(typeName)
	switch typeName {
	case "ss":
		protocol = domain.ProtocolShadowsocks
	case "socks5":
		protocol = domain.ProtocolSocks
	case "hy2":
		protocol = domain.ProtocolHysteria2
	case "vless", "vmess", "trojan", "socks", "hysteria", "hysteria2":
	default:
		return domain.Node{}, fmt.Errorf("unsupported proxy type %q", typeName)
	}

	port, err := yamlInt(entry, "port")
	if err != nil {
		return domain.Node{}, err
	}
	node := domain.Node{
		Name:        yamlString(entry, "name"),
		Remark:      yamlString(entry, "name"),
		Protocol:    protocol,
		Address:     yamlString(entry, "server"),
		Port:        port,
		UUID:        yamlString(entry, "uuid"),
		Password:    yamlString(entry, "password"),
		Encryption:  firstNonEmpty(yamlString(entry, "cipher"), yamlString(entry, "encryption")),
		ServerName:  firstNonEmpty(yamlString(entry, "servername"), yamlString(entry, "sni"), yamlString(entry, "peer")),
		Fingerprint: firstNonEmpty(yamlString(entry, "client-fingerprint"), yamlString(entry, "fingerprint"), yamlString(entry, "fp")),
		Flow:        yamlString(entry, "flow"),
		Transport:   firstNonEmpty(yamlString(entry, "network"), yamlString(entry, "transport")),
		ALPN:        yamlStringSlice(entry, "alpn"),
		Extras:      map[string]string{},
	}
	if node.Protocol == domain.ProtocolSocks {
		node.UUID = yamlString(entry, "username")
	}
	if node.Protocol == domain.ProtocolHysteria || node.Protocol == domain.ProtocolHysteria2 {
		node.Password = firstNonEmpty(node.Password, yamlString(entry, "auth"), yamlString(entry, "auth-str"))
		node.UUID = node.Password
		node.Extras["obfs"] = yamlString(entry, "obfs")
		node.Extras["obfs-password"] = firstNonEmpty(yamlString(entry, "obfs-password"), yamlString(entry, "obfs-passwords"))
	}
	if yamlBool(entry, "skip-cert-verify") {
		node.Extras["insecure"] = "true"
	}

	reality := yamlMap(entry, "reality-opts")
	if len(reality) > 0 {
		node.Security = "reality"
		node.PublicKey = firstNonEmpty(yamlString(reality, "public-key"), yamlString(reality, "pbk"))
		node.ShortID = firstNonEmpty(yamlString(reality, "short-id"), yamlString(reality, "sid"))
	} else if yamlBool(entry, "tls") || strings.EqualFold(yamlString(entry, "security"), "tls") {
		node.Security = "tls"
	} else {
		node.Security = yamlString(entry, "security")
	}

	switch node.Transport {
	case "ws":
		opts := yamlMap(entry, "ws-opts")
		node.Path = yamlString(opts, "path")
		headers := yamlMap(opts, "headers")
		node.Host = firstNonEmpty(yamlString(headers, "Host"), yamlString(headers, "host"))
	case "grpc":
		opts := yamlMap(entry, "grpc-opts")
		node.Path = firstNonEmpty(yamlString(opts, "grpc-service-name"), yamlString(opts, "service-name"))
	case "xhttp", "splithttp":
		node.Transport = "xhttp"
		opts := yamlMap(entry, "xhttp-opts")
		if len(opts) == 0 {
			opts = yamlMap(entry, "splithttp-opts")
		}
		node.Path = yamlString(opts, "path")
		node.Host = yamlString(opts, "host")
		query := url.Values{}
		if mode := yamlString(opts, "mode"); mode != "" {
			query.Set("mode", mode)
		}
		if concurrency := yamlString(opts, "concurrency"); concurrency != "" {
			query.Set("concurrency", concurrency)
		}
		node.RawQuery = query.Encode()
	}

	return normalizeNode(node, provider)
}

func yamlValue(values map[string]any, key string) any {
	for candidate, value := range values {
		if strings.EqualFold(strings.TrimSpace(candidate), key) {
			return value
		}
	}
	return nil
}

func yamlString(values map[string]any, key string) string {
	value := yamlValue(values, key)
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func yamlInt(values map[string]any, key string) (int, error) {
	raw := yamlString(values, key)
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid %s %q", key, raw)
	}
	return value, nil
}

func yamlBool(values map[string]any, key string) bool {
	switch strings.ToLower(yamlString(values, key)) {
	case "true", "yes", "1", "on":
		return true
	default:
		return false
	}
}

func yamlMap(values map[string]any, key string) map[string]any {
	value, _ := yamlValue(values, key).(map[string]any)
	return value
}

func yamlStringSlice(values map[string]any, key string) []string {
	value := yamlValue(values, key)
	switch typed := value.(type) {
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				result = append(result, text)
			}
		}
		return result
	case string:
		return splitCSV(typed)
	default:
		return nil
	}
}
