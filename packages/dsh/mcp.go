package dsh

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// MCPConfig provides authentication and the source-specific host capability set.
// The caller owns the listener and must restrict it to loopback for managed dsh.
type MCPConfig struct {
	Name, Version, Instructions string
	Authenticate                func(*http.Request) (string, bool)
	Registry                    func(context.Context, string) (*Registry, error)
	Context                     func(*http.Request) context.Context
}

func MCPHandler(config MCPConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if config.Authenticate == nil || config.Registry == nil {
			http.Error(w, "unavailable", 503)
			return
		}
		source, ok := config.Authenticate(r)
		if !ok {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", 405)
			return
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  struct {
				ProtocolVersion string          `json:"protocolVersion"`
				Name            string          `json:"name"`
				Arguments       json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "invalid request", 400)
			return
		}
		if len(req.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		ctx := r.Context()
		if config.Context != nil {
			ctx = config.Context(r)
		}
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		var result any
		var rpcErr any
		switch req.Method {
		case "initialize":
			v := mcpVersions[0]
			for _, s := range mcpVersions {
				if s == req.Params.ProtocolVersion {
					v = s
				}
			}
			result = map[string]any{"protocolVersion": v, "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": config.Name, "version": config.Version}, "instructions": config.Instructions}
		case "server/discover":
			result = map[string]any{"supportedVersions": mcpVersions, "capabilities": map[string]any{"tools": map[string]any{}}, "instructions": config.Instructions}
		case "ping":
			result = map[string]any{}
		case "tools/list", "tools/call":
			registry, err := config.Registry(ctx, source)
			if err != nil || registry == nil {
				rpcErr = map[string]any{"code": -32603, "message": "tool registry unavailable"}
				break
			}
			if req.Method == "tools/list" {
				result = map[string]any{"tools": registry.Tools(source)}
				break
			}
			res, err := registry.Call(ctx, source, req.Params.Name, req.Params.Arguments)
			if err != nil {
				res = ToolResult{Text: err.Error(), IsError: true}
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": res.Text}}, "isError": res.IsError}
		default:
			rpcErr = map[string]any{"code": -32601, "message": "method not found"}
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}
