package layahost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/packages/contracts"
)

func (c *Controller) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /laya", func(w http.ResponseWriter, r *http.Request) {
		ctx := releasefiles.WithTrace(r.Context(), r.Header.Get("traceparent"))
		w.Header().Set("traceparent", releasefiles.Traceparent(ctx))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		var req Request
		raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
		if err != nil || contracts.DecodeStrict(raw, &req, 4096) != nil {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_laya_operation"})
			return
		}
		started := time.Now()
		result, err := c.Operation(ctx, req)
		operation := req.Operation
		if operation != "ensure" && operation != "stop" && operation != "remove" && operation != "status" && operation != "model-storage" {
			operation = "invalid"
		}
		code, state := "", "succeeded"
		if err != nil {
			code, state = ErrorCode(err), "failed"
		}
		slog.InfoContext(ctx, "laya privileged operation", "trace", releasefiles.Trace(ctx), "operation", operation, "status", state, "code", code, "ms", time.Since(started).Milliseconds())
		if err != nil {
			code, status := ErrorCode(err), 422
			switch code {
			case "unsupported_laya_operation":
				status = 400
			case "approved_laya_deployment_unavailable", "laya_deployment_policy_mismatch":
				status = 409
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	return mux
}

func ErrorCode(err error) string {
	if errors.Is(err, context.Canceled) {
		return "laya_operation_cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "laya_operation_timeout"
	}
	var detail *contracts.TaskError
	if errors.As(err, &detail) {
		switch detail.Code {
		case "unsupported_laya_operation", "invalid_laya_operation", "approved_laya_deployment_unavailable", "laya_deployment_policy_mismatch", "laya_container_missing", "laya_container_policy_mismatch", "laya_container_operation_failed", "laya_operation_cancelled", "laya_operation_timeout", "laya_docker_unavailable", "laya_image_pull_failed", "laya_docker_output_limit", "laya_resources_unknown", "laya_resources_insufficient":
			return detail.Code
		}
	}
	return "laya_helper_unavailable"
}

// Client can only address the dedicated Unix socket, with no proxy or redirect.
type Client struct{ HTTP *http.Client }

func NewClient(socket string) *Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &Client{HTTP: &http.Client{Transport: transport, Timeout: 12 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) Operation(ctx context.Context, req Request) (Response, error) {
	var out Response
	raw, err := json.Marshal(req)
	if err != nil || c == nil || c.HTTP == nil {
		return out, failure("invalid_laya_operation", "layahost.Client")
	}
	r, err := http.NewRequestWithContext(ctx, "POST", "http://helper/laya", bytes.NewReader(raw))
	if err != nil {
		return out, failure("invalid_laya_operation", "layahost.Client")
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("traceparent", releasefiles.Traceparent(ctx))
	response, err := c.HTTP.Do(r)
	if ctx.Err() != nil {
		if response != nil {
			response.Body.Close()
		}
		return out, ctx.Err()
	}
	if err != nil {
		return out, failure("laya_helper_unavailable", "layahost.Client")
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return out, failure("laya_helper_unavailable", "layahost.Client")
	}
	if response.StatusCode != 200 {
		var body struct {
			Error string `json:"error"`
		}
		if contracts.DecodeStrict(raw, &body, 8192) == nil {
			code := ErrorCode(&contracts.TaskError{Code: body.Error})
			return out, failure(code, "layahost.Client")
		}
		return out, failure("laya_helper_unavailable", "layahost.Client")
	}
	if contracts.DecodeStrict(raw, &out, 8192) != nil || out.Version != req.ExpectedVersion || out.Model == "" {
		return out, failure("laya_helper_unavailable", "layahost.Client")
	}
	return out, nil
}
