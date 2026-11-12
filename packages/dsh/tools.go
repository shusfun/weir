package dsh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
)

// Tool is the single declaration used by direct calls and MCP discovery.
// Sources is an allowlist; an empty list permits every authenticated source.
// Secret names must be supplied by the host outside the model conversation.
type Tool struct {
	Name        string                                                             `json:"name"`
	Description string                                                             `json:"description"`
	InputSchema map[string]any                                                     `json:"inputSchema"`
	Sources     []string                                                           `json:"-"`
	Secret      []string                                                           `json:"-"`
	Handle      func(context.Context, string, json.RawMessage) (ToolResult, error) `json:"-"`
}

type ToolResult struct {
	Text    string
	IsError bool
}

type Registry struct {
	tools    []Tool
	observer Observer
}

var toolName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func NewRegistry(observer Observer, tools ...Tool) (*Registry, error) {
	r := &Registry{observer: observer}
	seen := map[string]bool{}
	for _, t := range tools {
		if !toolName.MatchString(t.Name) || t.Handle == nil || seen[t.Name] {
			return nil, errors.New("invalid or duplicate tool declaration")
		}
		// Freeze declarations so callers cannot change authorization while a call runs.
		b, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &t.InputSchema); err != nil {
			return nil, err
		}
		t.Sources, t.Secret = slices.Clone(t.Sources), slices.Clone(t.Secret)
		r.tools = append(r.tools, t)
		seen[t.Name] = true
	}
	return r, nil
}

func (r *Registry) Tools(source string) []Tool {
	out := []Tool{}
	for _, t := range r.tools {
		if len(t.Sources) != 0 && !slices.Contains(t.Sources, source) {
			continue
		}
		b, _ := json.Marshal(t.InputSchema)
		var schema map[string]any
		_ = json.Unmarshal(b, &schema)
		if props, ok := schema["properties"].(map[string]any); ok {
			for _, key := range t.Secret {
				delete(props, key)
			}
		}
		if req, ok := schema["required"].([]any); ok {
			kept := []any{}
			for _, key := range req {
				if s, ok := key.(string); ok && !slices.Contains(t.Secret, s) {
					kept = append(kept, s)
				}
			}
			schema["required"] = kept
		}
		out = append(out, Tool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return out
}

func (r *Registry) Call(ctx context.Context, source, name string, args json.RawMessage) (result ToolResult, err error) {
	end := observe(ctx, "dsh.tool", r.observer)
	defer func() {
		e := err
		if e == nil && result.IsError {
			e = errors.New("tool failed")
		}
		end(e)
	}()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	for _, t := range r.tools {
		if t.Name != name {
			continue
		}
		if len(t.Sources) != 0 && !slices.Contains(t.Sources, source) {
			return result, errors.New("tool source forbidden")
		}
		var obj map[string]json.RawMessage
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		if json.Unmarshal(args, &obj) != nil || obj == nil {
			return result, errors.New("tool arguments must be an object")
		}
		for _, key := range t.Secret {
			if _, ok := obj[key]; ok {
				return result, fmt.Errorf("secret argument %s must be supplied by host", key)
			}
		}
		return t.Handle(ctx, source, args)
	}
	return result, errors.New("unknown tool")
}

// DraftReceiver validates and stores proposals. It must never approve them.
type DraftReceiver interface {
	SubmitDraft(context.Context, string, json.RawMessage) (ToolResult, error)
}

func DraftTool(name, description string, schema map[string]any, sources []string, receiver DraftReceiver) Tool {
	return Tool{Name: name, Description: description, InputSchema: schema, Sources: sources, Handle: receiver.SubmitDraft}
}
