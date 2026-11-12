package gate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/publicnet"
	"github.com/shusfun/weir/packages/contracts"
)

func (a *Agent) networkTask(ctx context.Context, t contracts.Task) (json.RawMessage, error) {
	client := a.workerHTTP(publicnet.Client(30 * time.Second))
	defer client.CloseIdleConnections()
	return executeNetworkTask(ctx, t, client)
}

func executeNetworkTask(ctx context.Context, t contracts.Task, client *http.Client) (json.RawMessage, error) {
	var response contracts.SubscriptionFetchResult
	var rawURL, ua, authorization, method string
	var content []byte
	var contentType string
	var limit int64
	method = http.MethodGet
	switch t.Kind {
	case contracts.TaskSubscriptionFetch:
		var r contracts.SubscriptionFetchTask
		if e := contracts.DecodeStrict(t.Payload, &r, contracts.MaxTaskPayload); e != nil {
			return nil, e
		}
		rawURL, ua, authorization, limit = r.Credentials.URL, r.UserAgent, r.Credentials.Authorization, r.MaxBytes
	case contracts.TaskExternalConvert:
		var r contracts.ExternalConvertTask
		if e := contracts.DecodeStrict(t.Payload, &r, contracts.MaxTaskPayload); e != nil {
			return nil, e
		}
		rawURL, authorization, limit = r.Credentials.URL, r.Credentials.Authorization, r.MaxBytes
		content, contentType = r.Content, r.ContentType
		method = http.MethodPost
		if r.Method != "" {
			method = r.Method
		}
		if method != http.MethodGet && method != http.MethodPost || method == http.MethodGet && len(content) > 0 {
			return nil, errors.New("network_method_invalid")
		}
	case contracts.TaskReleaseQuery:
		var r contracts.ReleaseQueryTask
		if e := contracts.DecodeStrict(t.Payload, &r, contracts.MaxTaskPayload); e != nil {
			return nil, e
		}
		return executeReleaseQuery(ctx, r, client)
	default:
		return nil, errors.New("network_task_unknown")
	}
	// Base64 overhead must also fit the result envelope.
	if limit < 1 || limit > 10<<20 {
		return nil, errors.New("network_result_limit_invalid")
	}
	if _, err := publicnet.URL(rawURL); err != nil {
		return nil, err
	}
	for _, value := range []string{ua, authorization, contentType} {
		if strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("network_header_invalid")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(content))
	if err != nil {
		return nil, errors.New("network_request_invalid")
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept-Encoding", "gzip")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("network_request_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("network_upstream_rejected")
	}
	response.Body, err = publicnet.ReadBody(resp, limit, limit)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(response.Body)
	response.SHA256 = hex.EncodeToString(sum[:])
	response.ContentType = resp.Header.Get("Content-Type")
	response.UserInfo = resp.Header.Get("Subscription-Userinfo")
	response.Title = resp.Header.Get("Profile-Title")
	return json.Marshal(response)
}

func executeReleaseQuery(ctx context.Context, q contracts.ReleaseQueryTask, client *http.Client) (json.RawMessage, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	checked := *client
	checked.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Scheme != "https" || req.URL.Host != "api.github.com" || req.URL.User != nil || req.URL.Fragment != "" || !strings.HasPrefix(req.URL.Path, "/repos/"+q.Repository+"/releases") {
			return errors.New("release_redirect_rejected")
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(req, via)
		}
		return nil
	}
	gh := ghrelease.Client{HTTP: &checked, MaxBody: q.MaxBytes}
	var releases []ghrelease.Release
	var err error
	if q.PerPage > 0 {
		releases, err = gh.List(ctx, q.Repository, q.PerPage)
	} else {
		var release ghrelease.Release
		if q.Tag != "" {
			release, err = gh.Tag(ctx, q.Repository, q.Tag)
		} else {
			release, _, err = gh.Latest(ctx, q.Repository)
		}
		if err == nil {
			releases = []ghrelease.Release{release}
		}
	}
	if err != nil {
		if errors.Is(err, ghrelease.ErrNotFound) {
			return nil, errors.New("release_not_found")
		}
		if errors.Is(err, ghrelease.ErrRateLimited) {
			return nil, errors.New("release_rate_limited")
		}
		return nil, errors.New("release_query_failed")
	}
	raw, err := json.Marshal(releases)
	if err != nil {
		return nil, errors.New("release_result_invalid")
	}
	result := contracts.ReleaseQueryResult{Repository: q.Repository}
	if err = json.Unmarshal(raw, &result.Releases); err != nil {
		return nil, errors.New("release_result_invalid")
	}
	if err = result.Validate(q); err != nil {
		return nil, err
	}
	body, err := json.Marshal(result)
	if err != nil || int64(len(body)) > q.MaxBytes {
		return nil, errors.New("release_result_too_large")
	}
	sum := sha256.Sum256(body)
	return json.Marshal(contracts.SubscriptionFetchResult{Body: body, SHA256: hex.EncodeToString(sum[:]), ContentType: "application/json"})
}

// Only fixed local codes cross the task boundary; transport errors may embed
// subscription URLs, authorization values or upstream-controlled text.
func networkErrorCode(err error) string {
	if err != nil {
		switch err.Error() {
		case "release_not_found", "release_rate_limited", "release_query_failed", "release_query_invalid", "release_result_invalid", "release_result_ownership_invalid", "release_asset_invalid", "release_digest_invalid", "release_latest_invalid", "release_result_too_large", "network_header_invalid", "network_result_limit_invalid", "network_upstream_rejected", "network_request_failed":
			return err.Error()
		}
	}
	return "network_task_failed"
}
