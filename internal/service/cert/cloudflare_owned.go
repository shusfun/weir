package cert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OwnedTXT identifies one challenge, including its random durable ownership
// marker. Names alone never authorize deleting an existing DNS record.
type OwnedTXT struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Comment string `json:"comment"`
}

var errTXTNotFound = errors.New("dns_record_missing")

func validOwnedTXT(record OwnedTXT) bool {
	return strings.HasPrefix(record.Name, "_acme-challenge.") && validDomain.MatchString(strings.TrimPrefix(record.Name, "_acme-challenge.")) && len(record.Name) <= 280 && record.Content != "" && len(record.Content) <= 256 && strings.HasPrefix(record.Comment, "migate-dns01:") && len(record.Comment) >= 28 && len(record.Comment) <= 100
}

func (p CloudflareDNS) ownedRequest(ctx context.Context, method, suffix string, body any, target any) error {
	if !cloudflareID.MatchString(p.ZoneID) || strings.TrimSpace(p.Token) == "" {
		return errors.New("dns_credentials_required")
	}
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return errors.New("dns_record_invalid")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.cloudflare.com/client/v4/zones/"+p.ZoneID+"/dns_records"+suffix, bytes.NewReader(payload))
	if err != nil {
		return errors.New("dns_request_failed")
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	if p.Client != nil {
		*client = *p.Client
	}
	if client.Timeout <= 0 || client.Timeout > 20*time.Second {
		client.Timeout = 20 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, err := client.Do(req)
	if err != nil {
		return errors.New("dns_provider_unavailable")
	}
	defer r.Body.Close()
	if r.StatusCode == http.StatusNotFound && (method == http.MethodGet || method == http.MethodDelete) {
		return errTXTNotFound
	}
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return errors.New("dns_provider_rejected")
	}
	if err = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(target); err != nil {
		return errors.New("dns_provider_invalid_response")
	}
	return nil
}

func (p CloudflareDNS) CreateOwnedTXT(ctx context.Context, record OwnedTXT) (string, error) {
	if !validOwnedTXT(record) {
		return "", errors.New("dns_record_invalid")
	}
	var response struct {
		Success bool     `json:"success"`
		Result  OwnedTXT `json:"result"`
	}
	body := map[string]any{"type": "TXT", "name": record.Name, "content": record.Content, "comment": record.Comment, "ttl": 120}
	if err := p.ownedRequest(ctx, "POST", "", body, &response); err != nil {
		return "", err
	}
	if !response.Success || !cloudflareID.MatchString(response.Result.ID) {
		return "", errors.New("dns_record_identity_missing")
	}
	return response.Result.ID, nil
}

func sameOwnedTXT(actual, expected OwnedTXT) bool {
	return actual.Type == "TXT" && strings.EqualFold(strings.TrimSuffix(actual.Name, "."), strings.TrimSuffix(expected.Name, ".")) && actual.Content == expected.Content && actual.Comment == expected.Comment
}

// Recover the record ID after a crash between provider creation and local ID
// persistence. Every returned record must match name, value and ownership tag.
func (p CloudflareDNS) FindOwnedTXT(ctx context.Context, expected OwnedTXT) ([]string, error) {
	if !validOwnedTXT(expected) {
		return nil, errors.New("dns_record_invalid")
	}
	ids := []string{}
	for page := 1; page <= 10; page++ {
		var response struct {
			Success bool       `json:"success"`
			Result  []OwnedTXT `json:"result"`
			Info    struct {
				TotalPages *int `json:"total_pages"`
			} `json:"result_info"`
		}
		query := "?type=TXT&name=" + url.QueryEscape(expected.Name) + "&per_page=100&page=" + strconv.Itoa(page)
		if err := p.ownedRequest(ctx, "GET", query, nil, &response); err != nil {
			return nil, err
		}
		if !response.Success || response.Info.TotalPages == nil || *response.Info.TotalPages < 0 || *response.Info.TotalPages > 10 || len(response.Result) > 100 || *response.Info.TotalPages == 0 && len(response.Result) != 0 {
			return nil, errors.New("dns_record_inventory_incomplete")
		}
		for _, record := range response.Result {
			if sameOwnedTXT(record, expected) {
				if !cloudflareID.MatchString(record.ID) {
					return nil, errors.New("dns_record_identity_missing")
				}
				ids = append(ids, record.ID)
			}
		}
		if page >= *response.Info.TotalPages {
			return ids, nil
		}
	}
	return nil, errors.New("dns_record_inventory_incomplete")
}

func (p CloudflareDNS) DeleteOwnedTXT(ctx context.Context, expected OwnedTXT) error {
	if !validOwnedTXT(expected) || !cloudflareID.MatchString(expected.ID) {
		return errors.New("dns_record_invalid")
	}
	var response struct {
		Success bool     `json:"success"`
		Result  OwnedTXT `json:"result"`
	}
	if err := p.ownedRequest(ctx, "GET", "/"+expected.ID, nil, &response); errors.Is(err, errTXTNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if !response.Success || response.Result.ID != expected.ID || !sameOwnedTXT(response.Result, expected) {
		return errors.New("dns_record_ownership_changed")
	}
	var removed struct {
		Success bool `json:"success"`
	}
	if err := p.ownedRequest(ctx, "DELETE", "/"+expected.ID, nil, &removed); errors.Is(err, errTXTNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if !removed.Success {
		return errors.New("dns_record_delete_unconfirmed")
	}
	return nil
}
