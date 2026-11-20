package model

import (
	"encoding/json"
	"strings"
)

const OutboundSourceSubscription = "subscription"

type RoutingRule struct {
	ID          int64  `json:"id"`
	InboundID   int64  `json:"inbound_id,omitempty"`
	InboundTag  string `json:"inbound_tag"`
	ClientID    int64  `json:"client_id,omitempty"`
	ClientEmail string `json:"client_email,omitempty"`
	OutboundID  int64  `json:"outbound_id,omitempty"`
	OutboundTag string `json:"outbound_tag"`
	Domain      string `json:"domain"`
	IP          string `json:"ip"`
	RuleSet     string `json:"rule_set"`
	Protocol    string `json:"protocol"`
	Enabled     bool   `json:"enabled"`
	Sort        int    `json:"sort"`
}

type Inbound struct {
	ID                    int64    `json:"id"`
	UUID                  string   `json:"uuid"`
	Remark                string   `json:"remark"`
	Protocol              string   `json:"protocol"`
	Core                  string   `json:"core"`
	Port                  int      `json:"port"`
	Network               string   `json:"network"`
	Security              string   `json:"security"`
	Enabled               bool     `json:"enabled"`
	WsPath                string   `json:"ws_path"`
	WsHost                string   `json:"ws_host"`
	GrpcServiceName       string   `json:"grpc_service_name"`
	RealityDest           string   `json:"reality_dest"`
	RealityServerNames    string   `json:"reality_server_names"`
	RealityShortID        string   `json:"reality_short_id"`
	RealityPrivateKey     string   `json:"reality_private_key"`
	RealityPublicKey      string   `json:"reality_public_key"`
	SSMethod              string   `json:"ss_method"`
	SSPassword            string   `json:"ss_password"`
	TCPOnly               bool     `json:"tcp_only,omitempty"`
	TLSCertFile           string   `json:"tls_cert_file"`
	CertificateID         int64    `json:"certificate_id,omitempty"`
	TLSKeyFile            string   `json:"tls_key_file"`
	TLSSNI                string   `json:"tls_sni"`
	TLSFingerprint        string   `json:"tls_fingerprint"`
	TLSALPN               string   `json:"tls_alpn"`
	XHTTPPath             string   `json:"xhttp_path"`
	XHTTPMode             string   `json:"xhttp_mode"`
	Hy2UpMbps             int      `json:"hy2_up_mbps"`
	Hy2DownMbps           int      `json:"hy2_down_mbps"`
	Hy2Obfs               string   `json:"hy2_obfs"`
	Hy2ObfsPassword       string   `json:"hy2_obfs_password"`
	Hy2MPort              string   `json:"hy2_mport"`
	TuicCongestionControl string   `json:"tuic_congestion_control"`
	TuicZeroRTT           bool     `json:"tuic_zero_rtt"`
	WgPrivateKey          string   `json:"wg_private_key"`
	WgAddress             string   `json:"wg_address"`
	WgPeerPublicKey       string   `json:"wg_peer_public_key"`
	WgAllowedIPs          string   `json:"wg_allowed_ips"`
	WgEndpoint            string   `json:"wg_endpoint"`
	WgPresharedKey        string   `json:"wg_preshared_key"`
	WgMTU                 int      `json:"wg_mtu"`
	ShadowTLSVersion      int      `json:"shadowtls_version"`
	ShadowTLSPassword     string   `json:"shadowtls_password"`
	Clients               []Client `json:"clients"`
}

type Outbound struct {
	ID                   int64    `json:"id"`
	Tag                  string   `json:"tag"`
	Remark               string   `json:"remark"`
	Protocol             string   `json:"protocol"`
	Address              string   `json:"address"`
	Port                 int      `json:"port"`
	Username             string   `json:"username"`
	Password             string   `json:"password"`
	SupportedCores       []string `json:"supported_cores"`
	Enabled              bool     `json:"enabled"`
	Sort                 int      `json:"sort"`
	Source               string   `json:"source"`
	SubscriptionID       int64    `json:"subscription_id,omitempty"`
	SubscriptionIdentity string   `json:"subscription_identity,omitempty"`
	RawLink              string   `json:"raw_link,omitempty"`
	SettingsJSON         string   `json:"settings_json,omitempty"`
	LastSeenAt           string   `json:"last_seen_at,omitempty"`
}

type MaterializedSubscriptionOutbound struct {
	ID                   int64
	Tag                  string
	Remark               string
	Protocol             string
	Address              string
	Port                 int
	Username             string
	Password             string
	SubscriptionIdentity string
	RawLink              string
	SettingsJSON         string
	Position             int
}

type Client struct {
	ID                int64  `json:"id"`
	InboundID         int64  `json:"inbound_id"`
	UUID              string `json:"uuid"`
	CredentialID      string `json:"credential_id,omitempty"`
	Password          string `json:"password,omitempty"`
	SubscriptionToken string `json:"subscription_token,omitempty"`
	StatsKey          string `json:"stats_key,omitempty"`
	Email             string `json:"email"`
	Enabled           bool   `json:"enabled"`
	TrafficLimit      int64  `json:"traffic_limit"`
	ExpiryAt          int64  `json:"expiry_at"`
}

func (c Client) CredentialIDValue() string {
	if strings.TrimSpace(c.CredentialID) != "" {
		return strings.TrimSpace(c.CredentialID)
	}
	return strings.TrimSpace(c.UUID)
}

func (c Client) PasswordValue() string {
	if strings.TrimSpace(c.Password) != "" {
		return c.Password
	}
	return c.UUID
}

type OutboundSettings struct {
	UDPOverTCP         bool     `json:"udp_over_tcp,omitempty"`
	Obfs               string   `json:"obfs,omitempty"`
	ObfsPassword       string   `json:"obfs_password,omitempty"`
	CongestionControl  string   `json:"congestion_control,omitempty"`
	ZeroRTT            bool     `json:"zero_rtt,omitempty"`
	SPKI               string   `json:"spki,omitempty"`
	ShadowTLSVersion   int      `json:"shadowtls_version,omitempty"`
	Security           string   `json:"security,omitempty"`
	Network            string   `json:"network,omitempty"`
	TLS                bool     `json:"tls,omitempty"`
	Reality            bool     `json:"reality,omitempty"`
	SNI                string   `json:"sni,omitempty"`
	Host               string   `json:"host,omitempty"`
	Path               string   `json:"path,omitempty"`
	Flow               string   `json:"flow,omitempty"`
	Fingerprint        string   `json:"fp,omitempty"`
	PublicKey          string   `json:"pbk,omitempty"`
	ShortID            string   `json:"sid,omitempty"`
	ALPN               []string `json:"alpn,omitempty"`
	Method             string   `json:"method,omitempty"`
	ServiceName        string   `json:"service_name,omitempty"`
	Mode               string   `json:"mode,omitempty"` // XHTTP 模式（auto / packet-up / stream-up / stream-one）
	AllowInsecure      bool     `json:"allow_insecure,omitempty"`
	TrustedCertificate string   `json:"trusted_certificate,omitempty"`
}

func ParseOutboundSettings(raw string) OutboundSettings {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return OutboundSettings{}
	}
	var settings OutboundSettings
	_ = json.Unmarshal([]byte(raw), &settings)
	return settings
}
