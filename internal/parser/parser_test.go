package parser_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/design-maestro/fastlane/internal/parser"
)

func TestParseVLESSLink(t *testing.T) {
	t.Parallel()

	input := mustReadFixture(t, "vless", "subscription.txt")
	nodes, err := parser.ParseNodes(input, "Example Provider")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}

	assertGoldenNodes(t, nodes, "vless", "normalized.golden.json")
}

func TestParseVMessLink(t *testing.T) {
	t.Parallel()

	input := mustReadFixture(t, "vmess", "subscription.txt")
	nodes, err := parser.ParseNodes(input, "Example Provider")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}

	assertGoldenNodes(t, nodes, "vmess", "normalized.golden.json")
}

func TestParseMixedBase64Subscription(t *testing.T) {
	t.Parallel()

	input := mustReadFixture(t, "mixed", "subscription.b64")
	nodes, err := parser.ParseNodes(input, "Mixed Provider")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}

	assertGoldenNodes(t, nodes, "mixed", "normalized.golden.json")
}

func TestParseClashYAMLProxiesAndProviderPayload(t *testing.T) {
	t.Parallel()

	proxies := `proxies:
  - name: "NL Reality"
    type: vless
    server: nl.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    network: grpc
    tls: true
    servername: cdn.example.com
    client-fingerprint: chrome
    grpc-opts:
      grpc-service-name: edge
  - name: "SS Edge"
    type: ss
    server: 203.0.113.10
    port: 8388
    cipher: aes-256-gcm
    password: secret
`
	nodes, err := parser.ParseNodes(proxies, "Uploaded")
	if err != nil {
		t.Fatalf("parse Clash proxies YAML: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 YAML nodes, got %d", len(nodes))
	}
	if got := nodes[0]; got.Protocol != "vless" || got.Transport != "grpc" || got.Path != "edge" || got.ServerName != "cdn.example.com" {
		t.Fatalf("unexpected VLESS YAML node: %+v", got)
	}
	if got := nodes[1]; got.Protocol != "shadowsocks" || got.Encryption != "aes-256-gcm" || got.Password != "secret" {
		t.Fatalf("unexpected Shadowsocks YAML node: %+v", got)
	}

	payload := `payload:
  - name: "SOCKS"
    type: socks5
    server: 127.0.0.2
    port: 1080
    username: user
    password: pass
`
	nodes, err = parser.ParseNodes(payload, "Provider file")
	if err != nil {
		t.Fatalf("parse provider payload YAML: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Protocol != "socks" || nodes[0].UUID != "user" {
		t.Fatalf("unexpected provider payload node: %+v", nodes)
	}
}

func TestParseFormattedURLSafeBase64Subscription(t *testing.T) {
	t.Parallel()

	link := "vless://11111111-1111-1111-1111-111111111111@edge.example.com:443?encryption=none&security=reality&type=xhttp&pbk=public-key&sid=abcd#Edge"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(link))
	formatted := "  " + encoded[:16] + "\n\t" + encoded[16:] + "  "

	nodes, err := parser.ParseNodes(formatted, "URL-safe Provider")
	if err != nil {
		t.Fatalf("parse URL-safe base64 subscription: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Address != "edge.example.com" || nodes[0].Transport != "xhttp" {
		t.Fatalf("unexpected parsed nodes: %+v", nodes)
	}
}

func TestParseShadowsocksLink(t *testing.T) {
	t.Parallel()

	input := "ss://YWVzLTI1Ni1nY206cGFzc3dvcmRAc3MuZXhhbXBsZS5jb206ODM4OA#SS-Edge"
	nodes, err := parser.ParseNodes(input, "Example Provider")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	got := nodes[0]
	if got.Protocol != "shadowsocks" {
		t.Fatalf("expected shadowsocks protocol, got %+v", got)
	}
	if got.Address != "ss.example.com" || got.Port != 8388 {
		t.Fatalf("unexpected endpoint: %+v", got)
	}
	if got.Encryption != "aes-256-gcm" || got.Password != "password" {
		t.Fatalf("unexpected credentials: %+v", got)
	}
	if got.Name != "SS-Edge" || got.Remark != "SS-Edge" {
		t.Fatalf("unexpected label: %+v", got)
	}
}

func TestParseSocksLink(t *testing.T) {
	t.Parallel()

	input := "socks://ZGVtbzpkZW1vLXBhc3N3b3Jk@198.51.100.66:1080#Demo-Germany-SOCKS"
	nodes, err := parser.ParseNodes(input, "Example Provider")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	got := nodes[0]
	if got.Protocol != "socks" {
		t.Fatalf("expected socks protocol, got %+v", got)
	}
	if got.Address != "198.51.100.66" || got.Port != 1080 {
		t.Fatalf("unexpected endpoint: %+v", got)
	}
	if got.UUID != "demo" || got.Password != "demo-password" {
		t.Fatalf("unexpected credentials: %+v", got)
	}
	if got.Name != "Demo-Germany-SOCKS" || got.Remark != "Demo-Germany-SOCKS" {
		t.Fatalf("unexpected label: %+v", got)
	}
}

func TestParseHysteriaLink(t *testing.T) {
	t.Parallel()

	input := "hysteria://auth_token@hy.example.com:443?insecure=1&peer=sni.example.com#Hysteria-Node"
	nodes, err := parser.ParseNodes(input, "Example Provider")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	got := nodes[0]
	if got.Protocol != "hysteria" {
		t.Fatalf("expected hysteria protocol, got %+v", got)
	}
	if got.Address != "hy.example.com" || got.Port != 443 {
		t.Fatalf("unexpected endpoint: %+v", got)
	}
	if got.Password != "auth_token" || got.UUID != "auth_token" {
		t.Fatalf("unexpected credentials: %+v", got)
	}
	if got.ServerName != "sni.example.com" {
		t.Fatalf("unexpected SNI: %+v", got)
	}
	if got.Name != "Hysteria-Node" || got.Remark != "Hysteria-Node" {
		t.Fatalf("unexpected label: %+v", got)
	}
}

func TestParseHysteria2Link(t *testing.T) {
	t.Parallel()

	input := "hy2://password_token@hy2.example.com:8443?insecure=1&sni=sni.example.com&obfs=salamander&obfs-password=obfspass#Hysteria2-Node"
	nodes, err := parser.ParseNodes(input, "Example Provider")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	got := nodes[0]
	if got.Protocol != "hysteria2" {
		t.Fatalf("expected hysteria2 protocol, got %+v", got)
	}
	if got.Address != "hy2.example.com" || got.Port != 8443 {
		t.Fatalf("unexpected endpoint: %+v", got)
	}
	if got.Password != "password_token" || got.UUID != "password_token" {
		t.Fatalf("unexpected credentials: %+v", got)
	}
	if got.ServerName != "sni.example.com" {
		t.Fatalf("unexpected SNI: %+v", got)
	}
	if got.Extras["obfs"] != "salamander" || got.Extras["obfs-password"] != "obfspass" {
		t.Fatalf("unexpected obfs settings: %+v", got)
	}
	if got.Name != "Hysteria2-Node" || got.Remark != "Hysteria2-Node" {
		t.Fatalf("unexpected label: %+v", got)
	}
}

func TestParseXrayJSONConfig(t *testing.T) {
	t.Parallel()

	input := mustReadFixture(t, "three_x_ui", "config.json")
	nodes, err := parser.ParseNodes(input, "3x-ui Import")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}

	assertGoldenNodes(t, nodes, "three_x_ui", "normalized.golden.json")
}

func TestParseXrayJSONPreservesRawXHTTPOutbound(t *testing.T) {
	t.Parallel()

	input := `{"outbounds":[{"tag":"lossless","protocol":"vless","settings":{"vnext":[{"address":"node.example.com","port":443,"users":[{"id":"11111111-1111-1111-1111-111111111111","encryption":"none"}]}]},"streamSettings":{"network":"xhttp","security":"reality","xhttpSettings":{"mode":"packet-up","extra":{"xmux":{"maxConcurrency":8}},"downloadSettings":{"address":"download.example.com"}}}}]}`
	nodes, err := parser.ParseNodes(input, "HAPP JSON")
	if err != nil {
		t.Fatalf("parse xhttp json: %v", err)
	}
	if len(nodes) != 1 || len(nodes[0].RawOutbound) == 0 {
		t.Fatalf("expected one node with preserved outbound, got %+v", nodes)
	}
	var outbound map[string]any
	if err := json.Unmarshal(nodes[0].RawOutbound, &outbound); err != nil {
		t.Fatalf("decode preserved outbound: %v", err)
	}
	stream := outbound["streamSettings"].(map[string]any)
	xhttp := stream["xhttpSettings"].(map[string]any)
	if xhttp["mode"] != "packet-up" || xhttp["extra"] == nil || xhttp["downloadSettings"] == nil {
		t.Fatalf("xhttp settings were not preserved: %+v", xhttp)
	}
}

func TestParseXrayJSONConfigUsesTopLevelRemarksForNodeLabel(t *testing.T) {
	t.Parallel()

	input := `{
	  "remarks": "🇭🇺Венгрия",
	  "outbounds": [
	    {
	      "protocol": "vless",
	      "tag": "proxy",
	      "settings": {
	        "vnext": [
	          {
	            "address": "hungary-edge.example",
	            "port": 8443,
	            "users": [
	              {
	                "id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
	                "encryption": "none",
	                "flow": "xtls-rprx-vision"
	              }
	            ]
	          }
	        ]
	      },
	      "streamSettings": {
	        "network": "tcp",
	        "security": "reality",
	        "realitySettings": {
	          "serverName": "gateway.example",
	          "publicKey": "test-public-key",
	          "shortId": "testshort01",
	          "fingerprint": "random"
	        }
	      }
	    },
	    {
	      "protocol": "freedom",
	      "tag": "direct"
	    }
	  ]
	}`

	nodes, err := parser.ParseNodes(input, "JSON Import")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Name != "🇭🇺Венгрия" || nodes[0].Remark != "🇭🇺Венгрия" {
		t.Fatalf("expected top-level remarks to become node label, got %+v", nodes[0])
	}
}

func TestParseXrayJSONConfigSupportsDirectVLESSSettings(t *testing.T) {
	t.Parallel()

	input := `{
	  "outbounds": [
	    {
	      "settings": {
	        "encryption": "none",
	        "flow": "xtls-rprx-vision",
	        "port": 8443,
	        "address": "hungary-edge.example",
	        "id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	      },
	      "protocol": "vless",
	      "tag": "proxy",
	      "streamSettings": {
	        "tcpSettings": {
	          "header": {
	            "type": "none"
	          }
	        },
	        "realitySettings": {
	          "shortId": "testshort01",
	          "publicKey": "test-public-key",
	          "spiderX": "",
	          "serverName": "gateway.example",
	          "fingerprint": "random"
	        },
	        "security": "reality",
	        "network": "tcp"
	      }
	    },
	    {
	      "settings": {
	        "response": {
	          "type": "none"
	        }
	      },
	      "protocol": "blackhole",
	      "tag": "block"
	    },
	    {
	      "settings": {},
	      "protocol": "freedom",
	      "tag": "direct"
	    }
	  ],
	  "policy": {
	    "system": {
	      "statsOutboundUplink": true,
	      "statsInboundUplink": true,
	      "statsInboundDownlink": true,
	      "statsOutboundDownlink": true
	    }
	  },
	  "log": {
	    "loglevel": "info"
	  },
	  "id": "AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA",
	  "inbounds": [
	    {
	      "settings": {
	        "udp": true
	      },
	      "listen": "[::1]",
	      "port": 1080,
	      "protocol": "socks",
	      "tag": "socks",
	      "sniffing": {
	        "enabled": true,
	        "destOverride": [
	          "tls",
	          "http",
	          "quic"
	        ],
	        "routeOnly": false
	      }
	    }
	  ],
	  "stats": {},
	  "remarks": "🇭🇺Венгрия"
	}`

	nodes, err := parser.ParseNodes(input, "JSON Import")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	got := nodes[0]
	if got.Name != "🇭🇺Венгрия" || got.Remark != "🇭🇺Венгрия" {
		t.Fatalf("expected top-level remarks to become node label, got %+v", got)
	}
	if got.Address != "hungary-edge.example" || got.Port != 8443 {
		t.Fatalf("unexpected endpoint: %+v", got)
	}
	if got.UUID != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" {
		t.Fatalf("unexpected uuid: %+v", got)
	}
	if got.Security != "reality" || got.ServerName != "gateway.example" {
		t.Fatalf("unexpected stream settings: %+v", got)
	}
}

func TestParseXrayJSONDirectProtocolUsesTopLevelRemarksForNameAndRemark(t *testing.T) {
	t.Parallel()

	input := `{
	  "remarks": "🇳🇱 Нидерланды",
	  "protocol": "vless",
	  "tag": "proxy",
	  "settings": {
	    "encryption": "none",
	    "flow": "xtls-rprx-vision",
	    "port": 8443,
	    "address": "nl-node.example.com",
	    "id": "11111111-1111-1111-1111-111111111111"
	  },
	  "streamSettings": {
	    "network": "tcp",
	    "security": "reality",
	    "realitySettings": {
	      "serverName": "www.example.com",
	      "publicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	      "shortId": "",
	      "fingerprint": "qq"
	    }
	  }
	}`

	nodes, err := parser.ParseNodes(input, "Starlink")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Name != "🇳🇱 Нидерланды" || nodes[0].Remark != "🇳🇱 Нидерланды" {
		t.Fatalf("expected name and remark to use top-level remarks, got %+v", nodes[0])
	}
}

func TestParseXrayJSONWrapperPropagatesTopLevelRemarksIntoNestedConfig(t *testing.T) {
	t.Parallel()

	input := `{
	  "remarks": "🇳🇱 Нидерланды",
	  "config": {
	    "outbounds": [
	      {
	        "protocol": "vless",
	        "tag": "proxy",
	        "settings": {
	          "encryption": "none",
	          "flow": "xtls-rprx-vision",
	          "port": 8443,
	          "address": "nl-node.example.com",
	          "id": "11111111-1111-1111-1111-111111111111"
	        },
	        "streamSettings": {
	          "network": "tcp",
	          "security": "reality",
	          "realitySettings": {
	            "serverName": "www.example.com",
	            "publicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	            "shortId": "",
	            "fingerprint": "qq"
	          }
	        }
	      },
	      {
	        "protocol": "freedom",
	        "tag": "direct"
	      }
	    ]
	  }
	}`

	nodes, err := parser.ParseNodes(input, "Starlink")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Name != "🇳🇱 Нидерланды" || nodes[0].Remark != "🇳🇱 Нидерланды" {
		t.Fatalf("expected wrapper remarks to populate name and remark, got %+v", nodes[0])
	}
}

func TestParseJSONWrapperLinkPrefersWrapperRemarksOverNestedLinkName(t *testing.T) {
	t.Parallel()

	input := `{
	  "remarks": "Netherlands",
	  "link": "vless://11111111-1111-1111-1111-111111111111@nl.example.com:443?encryption=none&security=tls&sni=nl.example.com&type=ws&path=%2Fa&host=cdn.example.com#Netherlands-bypass"
	}`

	nodes, err := parser.ParseNodes(input, "Liberty VPN")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Name != "Netherlands" || nodes[0].Remark != "Netherlands" {
		t.Fatalf("expected wrapper remarks to override nested link label, got %+v", nodes[0])
	}
}

func TestParseJSONWrapperStringConfigPrefersWrapperRemarksOverNestedLinkName(t *testing.T) {
	t.Parallel()

	input := `{
	  "remarks": "Netherlands",
	  "config": "vless://11111111-1111-1111-1111-111111111111@nl.example.com:443?encryption=none&security=tls&sni=nl.example.com&type=ws&path=%2Fa&host=cdn.example.com#Netherlands-bypass"
	}`

	nodes, err := parser.ParseNodes(input, "Liberty VPN")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Name != "Netherlands" || nodes[0].Remark != "Netherlands" {
		t.Fatalf("expected wrapper remarks to override nested config label, got %+v", nodes[0])
	}
}

func TestParseJSONArrayWrapperRemarksProduceDistinctNodeIDs(t *testing.T) {
	t.Parallel()

	input := `[
	  {
	    "remarks": "Netherlands",
	    "link": "vless://11111111-1111-1111-1111-111111111111@nl.example.com:443?encryption=none&security=tls&sni=nl.example.com&type=ws&path=%2Fa&host=cdn.example.com#Shared-bypass"
	  },
	  {
	    "remarks": "Netherlands-bypass",
	    "link": "vless://11111111-1111-1111-1111-111111111111@nl.example.com:443?encryption=none&security=tls&sni=nl.example.com&type=ws&path=%2Fa&host=cdn.example.com#Shared-bypass"
	  }
	]`

	nodes, err := parser.ParseNodes(input, "Liberty VPN")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].Name != "Netherlands" || nodes[1].Name != "Netherlands-bypass" {
		t.Fatalf("expected wrapper labels to win over nested link labels, got %+v", nodes)
	}
	if nodes[0].ID == nodes[1].ID {
		t.Fatalf("expected distinct node ids after wrapper label override, got %+v", nodes)
	}
}

func TestParseJSONArrayOfXrayConfigs(t *testing.T) {
	t.Parallel()

	input := `[
	  {
	    "remarks": "One",
	    "outbounds": [
	      {
	        "protocol": "vless",
	        "tag": "proxy-one",
	        "settings": {
	          "vnext": [
	            {
	              "address": "one.example.com",
	              "port": 443,
	              "users": [
	                {
	                  "id": "11111111-1111-1111-1111-111111111111",
	                  "encryption": "none",
	                  "flow": "xtls-rprx-vision"
	                }
	              ]
	            }
	          ]
	        },
	        "streamSettings": {
	          "network": "tcp",
	          "security": "reality",
	          "realitySettings": {
	            "serverName": "gateway-one.example",
	            "publicKey": "public-key-one",
	            "shortId": "short-one",
	            "fingerprint": "random"
	          }
	        }
	      },
	      {
	        "protocol": "freedom",
	        "tag": "direct"
	      }
	    ]
	  },
	  {
	    "remarks": "Two",
	    "outbounds": [
	      {
	        "protocol": "vless",
	        "tag": "proxy-two",
	        "settings": {
	          "vnext": [
	            {
	              "address": "two.example.com",
	              "port": 8443,
	              "users": [
	                {
	                  "id": "22222222-2222-2222-2222-222222222222",
	                  "encryption": "none",
	                  "flow": "xtls-rprx-vision"
	                }
	              ]
	            }
	          ]
	        },
	        "streamSettings": {
	          "network": "tcp",
	          "security": "reality",
	          "realitySettings": {
	            "serverName": "gateway-two.example",
	            "publicKey": "public-key-two",
	            "shortId": "short-two",
	            "fingerprint": "random"
	          }
	        }
	      }
	    ]
	  }
	]`

	nodes, err := parser.ParseNodes(input, "JSON Array Provider")
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].Protocol != "vless" || nodes[1].Protocol != "vless" {
		t.Fatalf("unexpected protocols: %+v", nodes)
	}
	if nodes[0].Name != "One" || nodes[0].Remark != "One" {
		t.Fatalf("expected first node label from top-level remarks, got %+v", nodes[0])
	}
	if nodes[1].Name != "Two" || nodes[1].Remark != "Two" {
		t.Fatalf("expected second node label from top-level remarks, got %+v", nodes[1])
	}
}

func TestParseHysteriaJSONConfig(t *testing.T) {
	t.Parallel()

	input := `{
		"remarks": "Test Hysteria",
		"outbounds": [
			{
				"protocol": "hysteria",
				"settings": {
					"address": "hy2.example.com",
					"port": 8449,
					"version": 2
				},
				"streamSettings": {
					"hysteriaSettings": {
						"auth": "11111111-1111-1111-1111-111111111111",
						"version": 2
					},
					"network": "hysteria",
					"security": "tls",
					"tlsSettings": {
						"allowInsecure": false,
						"alpn": ["h3"],
						"serverName": "sni.example.com",
						"show": false,
						"fingerprint": "firefox"
					}
				},
				"tag": "proxy"
			}
		]
	}`

	nodes, err := parser.ParseNodes(input, "Test source")
	if err != nil {
		t.Fatalf("parse hysteria json config: %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	node := nodes[0]
	if node.Protocol != "hysteria2" {
		t.Fatalf("expected protocol hysteria2, got %q", node.Protocol)
	}
	if node.Address != "hy2.example.com" {
		t.Fatalf("expected address hy2.example.com, got %q", node.Address)
	}
	if node.Port != 8449 {
		t.Fatalf("expected port 8449, got %d", node.Port)
	}
	if node.UUID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("expected UUID/auth 11111111-1111-1111-1111-111111111111, got %q", node.UUID)
	}
	if node.ServerName != "sni.example.com" {
		t.Fatalf("expected serverName sni.example.com, got %q", node.ServerName)
	}
	if len(node.ALPN) != 1 || node.ALPN[0] != "h3" {
		t.Fatalf("expected ALPN [h3], got %+v", node.ALPN)
	}
}

func TestParseInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := parser.ParseNodes("not-a-subscription", "Broken"); err == nil {
		t.Fatal("expected invalid input to fail")
	}
}

func mustReadFixture(t *testing.T, parts ...string) string {
	t.Helper()

	path := filepath.Join(append([]string{"..", "..", "test", "fixtures"}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	return string(data)
}

func assertGoldenNodes(t *testing.T, nodes any, fixtureDir, golden string) {
	t.Helper()

	rawGot, err := marshalCanonicalJSON(nodes)
	if err != nil {
		t.Fatalf("marshal nodes: %v", err)
	}

	got, err := normalizeJSONString(string(rawGot))
	if err != nil {
		t.Fatalf("normalize generated nodes: %v", err)
	}

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		path := filepath.Join("..", "..", "test", "fixtures", fixtureDir, golden)
		if err := os.WriteFile(path, []byte(got+"\n"), 0644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
	}

	want, err := normalizeJSONString(mustReadFixture(t, fixtureDir, golden))
	if err != nil {
		t.Fatalf("normalize golden: %v", err)
	}

	if got != want {
		t.Fatalf("golden mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func normalizeJSONString(input string) (string, error) {
	var value any
	if err := json.Unmarshal([]byte(input), &value); err != nil {
		return "", err
	}

	data, err := marshalCanonicalJSON(value)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func marshalCanonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}

	return bytes.TrimSpace(buffer.Bytes()), nil
}

func TestParseNodesSkipsInvalidLines(t *testing.T) {
	t.Parallel()

	input := `
# A comment line
vless://11111111-1111-1111-1111-111111111111@de.example.com:443?type=ws&security=tls#Germany
ss://invalid-credentials@us.example.com:443#USA
vmess://eyJhZGQiOiJqcC5leGFtcGxlLmNvbSIsImFpZCI6IjAiLCJhbHBuIjoiIiwiaG9zdCI6IiIsImlkIjoiMjIyMjIyMjItMjIyMi0yMjIyLTIyMjItMjIyMjIyMjIyMjIyIiwiaW5zZWVkIjoiIiwibmV0IjoidGNwIiwicGF0aCI6IiIsInBvcnQiOiI0NDMiLCJwcyI6IkphcGFuIiwic2N5IjoiYXV0byIsInNuaSI6IiIsInRscyI6InRscyIsInR5cGUiOiJub25lIiwidmVyIjoiMiJ9
`

	nodes, err := parser.ParseNodes(input, "Test Provider")
	if err != nil {
		t.Fatalf("unexpected error parsing nodes with some invalid lines: %v", err)
	}

	if len(nodes) != 2 {
		t.Fatalf("expected 2 valid nodes, got %d", len(nodes))
	}

	if nodes[0].Name != "Germany" || nodes[0].Protocol != "vless" {
		t.Errorf("unexpected first node: %+v", nodes[0])
	}

	if nodes[1].Name != "Japan" || nodes[1].Protocol != "vmess" {
		t.Errorf("unexpected second node: %+v", nodes[1])
	}
}
