package subgroup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCombineRuntimeRuleErrorDoesNotBecomeEmptySuccess(t *testing.T) {
	bad := trojan(1, "%5B")
	ops := []any{map[string]any{"type": "script", "params": map[string]any{"action": "rename", "template": "{replace(name, name, 'x')}"}}}
	for _, fallback := range []bool{false, true} {
		cache := &memCache{m: map[int][]string{1: {bad}}}
		fetcher := Fetcher{Fetch: func(context.Context, string, string) (Response, error) {
			if fallback {
				return Response{}, errors.New("offline")
			}
			return Response{Body: []byte(bad)}, nil
		}}
		output, results, err := CombineBounded(t.Context(), fetcher, cache, []Source{{ID: 1, URL: "https://source.example", NodeCache: true, Operators: ops}}, nil, Options{}, 1<<20)
		if err == nil || output != "" || len(results) != 1 || !strings.Contains(results[0].Error, "source_rules_invalid") || results[0].FromCache != fallback {
			t.Fatalf("processing failure hidden: fallback=%v err=%v results=%+v", fallback, err, results)
		}
		if raw, ok := cache.Load(t.Context(), 1); !ok || len(raw) != 1 || raw[0] != bad {
			t.Fatal("runtime failure mutated the raw cache")
		}
	}
	_, _, err := CombineBounded(t.Context(), Fetcher{}, nil, nil, []ManualNode{{ID: 1, URL: bad, Operators: ops}}, Options{}, 1<<20)
	if err == nil || !strings.Contains(err.Error(), "node_rules_invalid") {
		t.Fatalf("manual runtime failure hidden: %v", err)
	}
}

func TestTemplateRegexIsValidatedWithoutNodes(t *testing.T) {
	text := "[custom]\ncustom_proxy_group=select`select`(bad[\n"
	if err := CheckTemplate("ini", text); err == nil {
		t.Fatal("invalid template filter accepted without nodes")
	}
	if _, _, err := Render("", "clash", RenderOptions{}, Template{INI: true, Text: text}); err == nil {
		t.Fatal("render silently ignored an invalid template filter")
	}
}
