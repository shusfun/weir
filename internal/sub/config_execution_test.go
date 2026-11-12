package sub

import (
	"strings"
	"testing"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

func TestCheckedDSLReportsDynamicRegexAndBounds(t *testing.T) {
	ctx := js.Obj("name", "HK", "pattern", "secret-not-for-errors(")
	if _, err := EvaluateDslExpressionChecked("match(name, pattern)", ctx); err == nil || strings.Contains(err.Error(), "secret-not-for-errors") {
		t.Fatalf("dynamic regex error missing or contains input: %v", err)
	}
	if _, err := RenderDslTemplateChecked("{replace(name, pattern, 'x')}", ctx); err == nil {
		t.Fatal("invalid dynamic replacement appeared successful")
	}
	ctx.Set("name", strings.Repeat("x", 40000))
	if _, err := RenderDslTemplateChecked("{name}{name}", ctx); err == nil {
		t.Fatal("template expansion exceeded the node limit")
	}
	expression := strings.Repeat("upper(", 40) + "name" + strings.Repeat(")", 40)
	if _, err := RenderDslTemplateChecked("{"+expression+"}", ctx); err == nil {
		t.Fatal("recursive expression limit missing")
	}
}

func TestDSLBooleanTokensInsideArgumentsAreData(t *testing.T) {
	ctx := js.Obj("name", "a || b && c")
	for _, expression := range []string{
		"contains(name, 'a || b && c')",
		"contains(name, 'a || b') && contains(name, 'b && c')",
	} {
		value, err := EvaluateDslExpressionChecked(expression, ctx)
		if err != nil || !value {
			t.Fatalf("quoted boolean tokens misparsed: %q value=%v err=%v", expression, value, err)
		}
	}
}

func TestCheckedOperatorRejectsRuntimeRegex(t *testing.T) {
	ops := js.Arr(js.Obj("type", "script", "params", js.Obj("action", "rename", "template", "{replace(name, name, 'x')}")))
	if _, err := RunOperatorChainChecked([]string{"trojan://pw@node.example:443#%5B"}, ops, OperatorContext{}); err == nil {
		t.Fatal("operator swallowed a runtime regex error")
	}
	filter := js.Arr(js.Obj("type", "filter", "params", js.Obj("script", js.Obj("enabled", true, "expression", "match(name, name)"))))
	if _, err := RunOperatorChainChecked([]string{"trojan://pw@node.example:443#%5B"}, filter, OperatorContext{}); err == nil {
		t.Fatal("legacy filter script swallowed a runtime regex error")
	}
}

func TestOperatorRulesDoNotUseSubscriptionFilterPrefixes(t *testing.T) {
	ops := []any{map[string]any{"params": map[string]any{"regex": map[string]any{"rules": []any{"proto:("}}}}}
	if err := ValidateOperatorPatterns(ops); err == nil {
		t.Fatal("regex rule bypassed validation through a subscription filter prefix")
	}
	if err := ValidateFilterRules("proto:trojan\nkeep:HK"); err != nil {
		t.Fatalf("subscription filter grammar rejected: %v", err)
	}
}
