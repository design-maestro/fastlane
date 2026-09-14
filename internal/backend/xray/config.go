package xray

type xrayConfig struct {
	Log         xrayLog          `json:"log"`
	API         *xrayAPI         `json:"api,omitempty"`
	DNS         *xrayDNS         `json:"dns,omitempty"`
	Inbounds    []xrayInbound    `json:"inbounds"`
	Outbounds   []any            `json:"outbounds"`
	Routing     xrayRouting      `json:"routing"`
	Observatory *xrayObservatory `json:"observatory,omitempty"`
}

type xrayObservatory struct {
	SubjectSelector []string `json:"subjectSelector"`
	ProbeURL        string   `json:"probeURL"`
	ProbeInterval   string   `json:"probeInterval"`
}

type xrayAPI struct {
	Tag      string   `json:"tag"`
	Listen   string   `json:"listen,omitempty"`
	Services []string `json:"services"`
}

type xrayLog struct {
	LogLevel string `json:"loglevel"`
}

type xrayDNS struct {
	Servers []any `json:"servers,omitempty"`
}

type xrayDNSServer struct {
	Address      string   `json:"address"`
	Domains      []string `json:"domains,omitempty"`
	SkipFallback bool     `json:"skipFallback,omitempty"`
}

type xrayInbound struct {
	Tag            string `json:"tag"`
	Listen         string `json:"listen"`
	Port           int    `json:"port"`
	Protocol       string `json:"protocol"`
	Settings       any    `json:"settings"`
	Sniffing       any    `json:"sniffing,omitempty"`
	StreamSettings any    `json:"streamSettings,omitempty"`
}

type xrayRouting struct {
	DomainStrategy string          `json:"domainStrategy"`
	Rules          []xrayRouteRule `json:"rules"`
	Balancers      []xrayBalancer  `json:"balancers,omitempty"`
}

type xrayBalancer struct {
	Tag         string   `json:"tag"`
	Selector    []string `json:"selector"`
	FallbackTag string   `json:"fallbackTag,omitempty"`
}

type xrayRouteRule struct {
	Type        string   `json:"type"`
	RuleTag     string   `json:"ruleTag,omitempty"`
	OutboundTag string   `json:"outboundTag,omitempty"`
	BalancerTag string   `json:"balancerTag,omitempty"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	Network     string   `json:"network,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
}

type xrayCommonOutbound struct {
	Tag            string `json:"tag"`
	Protocol       string `json:"protocol"`
	Settings       any    `json:"settings,omitempty"`
	StreamSettings any    `json:"streamSettings,omitempty"`
	Mux            any    `json:"mux,omitempty"`
}
