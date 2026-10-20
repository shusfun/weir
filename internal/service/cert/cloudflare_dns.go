package cert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var cloudflareID = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

type CloudflareDNS struct {
	Token  string
	ZoneID string
	Client *http.Client
}

func (p CloudflareDNS) request(ctx context.Context, method, path string, body any) (string, error) {
	if !cloudflareID.MatchString(p.ZoneID) || strings.TrimSpace(p.Token) == "" {
		return "", errors.New("dns_credentials_required")
	}
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.cloudflare.com/client/v4/zones/"+p.ZoneID+"/dns_records"+path, bytes.NewReader(data))
	if err != nil {
		return "", errors.New("dns_request_failed")
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	if p.Client != nil {
		*client = *p.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		return "", errors.New("dns_provider_unavailable")
	}
	defer response.Body.Close()
	var out struct {
		Success bool `json:"success"`
		Result  struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&out) != nil || !out.Success {
		return "", errors.New("dns_provider_rejected")
	}
	return out.Result.ID, nil
}
func (p CloudflareDNS) Present(ctx context.Context, name, value string) (func(context.Context) error, error) {
	if !strings.HasPrefix(name, "_acme-challenge.") || len(name) > 280 || len(value) > 256 || value == "" {
		return nil, errors.New("dns_record_invalid")
	}
	id, err := p.request(ctx, "POST", "", map[string]any{"type": "TXT", "name": name, "content": value, "ttl": 120})
	if err != nil {
		return nil, err
	}
	if !cloudflareID.MatchString(id) {
		return nil, errors.New("dns_record_identity_missing")
	}
	return func(ctx context.Context) error { _, err := p.request(ctx, "DELETE", "/"+id, nil); return err }, nil
}
