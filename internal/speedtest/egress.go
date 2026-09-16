package speedtest

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const egressTraceURL = "https://www.cloudflare.com/cdn-cgi/trace"

// ProbeEgressIdentity reads the public address and country observed through
// the supplied client. It is deliberately best-effort: location metadata must
// never turn a successful connectivity check into a failed server check.
func ProbeEgressIdentity(ctx context.Context, client *http.Client, timeout time.Duration) (string, string) {
	if client == nil {
		return "", ""
	}
	if timeout <= 0 || timeout > 4*time.Second {
		timeout = 4 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, egressTraceURL, nil)
	if err != nil {
		return "", ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", ""
	}
	var ip, country string
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "ip":
			if parsed := net.ParseIP(strings.TrimSpace(value)); parsed != nil {
				ip = parsed.String()
			}
		case "loc":
			value = strings.ToUpper(strings.TrimSpace(value))
			if len(value) == 2 {
				country = value
			}
		}
	}
	return ip, country
}
