package parser

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/design-maestro/fastlane/internal/domain"
)

// ParseTrojan parses a Trojan share link into a normalized node.
func ParseTrojan(raw, provider string) (domain.Node, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return domain.Node{}, fmt.Errorf("parse trojan link: %w", err)
	}

	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return domain.Node{}, fmt.Errorf("parse trojan port: %w", err)
	}

	query := parsed.Query()
	node := domain.Node{
		Name:       parsed.Fragment,
		Remark:     parsed.Fragment,
		Protocol:   domain.ProtocolTrojan,
		Address:    parsed.Hostname(),
		Port:       port,
		Password:   passwordFromURL(parsed),
		Security:   query.Get("security"),
		ServerName: firstNonEmpty(query.Get("sni"), query.Get("serverName")),
		ALPN:       splitCSV(query.Get("alpn")),
		Transport:  firstNonEmpty(query.Get("type"), query.Get("network")),
		Path:       query.Get("path"),
		Host:       query.Get("host"),
		RawQuery:   parsed.RawQuery,
		Extras: map[string]string{
			"type": firstNonEmpty(query.Get("type"), query.Get("network")),
		},
	}

	return normalizeNode(node, provider)
}

// ParseShadowsocks parses common SS share links.
func ParseShadowsocks(raw, provider string) (domain.Node, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return domain.Node{}, fmt.Errorf("parse shadowsocks link: %w", err)
	}

	var credentials, host, portText, fragment string
	if parsed.User != nil && parsed.Host != "" {
		fragment = parsed.Fragment
		host = parsed.Hostname()
		portText = parsed.Port()
		credentials = parsed.User.Username()
		if password, ok := parsed.User.Password(); ok {
			credentials += ":" + password
		} else {
			credentials, err = decodeShadowsocksCredentials(credentials)
			if err != nil {
				return domain.Node{}, err
			}
		}
	} else {
		payload := strings.TrimPrefix(trimmed, parsed.Scheme+"://")
		if idx := strings.Index(payload, "#"); idx >= 0 {
			fragment, err = url.PathUnescape(payload[idx+1:])
			if err != nil {
				return domain.Node{}, fmt.Errorf("decode shadowsocks label: %w", err)
			}
			payload = payload[:idx]
		}
		decoded, decodeErr := decodeShadowsocksBase64(payload)
		if decodeErr != nil {
			return domain.Node{}, decodeErr
		}
		separator := strings.LastIndex(decoded, "@")
		if separator <= 0 || separator == len(decoded)-1 {
			return domain.Node{}, fmt.Errorf("invalid shadowsocks payload")
		}
		credentials = decoded[:separator]
		host, portText, err = net.SplitHostPort(decoded[separator+1:])
		if err != nil {
			return domain.Node{}, fmt.Errorf("invalid shadowsocks host: %w", err)
		}
	}

	credParts := strings.SplitN(credentials, ":", 2)
	if len(credParts) != 2 {
		return domain.Node{}, fmt.Errorf("invalid shadowsocks credentials")
	}
	if host == "" || portText == "" {
		return domain.Node{}, fmt.Errorf("invalid shadowsocks host")
	}

	port, err := strconv.Atoi(portText)
	if err != nil {
		return domain.Node{}, fmt.Errorf("parse shadowsocks port: %w", err)
	}

	node := domain.Node{
		Name:       fragment,
		Remark:     fragment,
		Protocol:   domain.ProtocolShadowsocks,
		Address:    host,
		Port:       port,
		Password:   credParts[1],
		Encryption: credParts[0],
	}

	return normalizeNode(node, provider)
}

func decodeShadowsocksCredentials(input string) (string, error) {
	if strings.Contains(input, ":") {
		return input, nil
	}
	return decodeShadowsocksBase64(input)
}

func decodeShadowsocksBase64(input string) (string, error) {
	encodings := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	var lastErr error
	for _, encoding := range encodings {
		payload, err := encoding.DecodeString(input)
		if err == nil {
			return string(payload), nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("decode shadowsocks payload: %w", lastErr)
}

func passwordFromURL(parsed *url.URL) string {
	if parsed.User == nil {
		return ""
	}

	if password, ok := parsed.User.Password(); ok {
		return password
	}

	return parsed.User.Username()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
