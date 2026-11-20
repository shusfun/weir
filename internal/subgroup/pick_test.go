package subgroup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/subscription"
)

func TestPickRegionsMultiplierKeywordsAlive(t *testing.T) {
	raw := []string{
		trojan(1, "香港 01 x2"),
		trojan(2, "香港 02 x0.5"),
		trojan(3, "香港 03 IPLC"),
		trojan(4, "日本 01 x1"),
		trojan(5, "美国 01 x0.5"),
		trojan(6, "香港 04 IPLC x0.5"),
	}
	cache := &memCache{m: map[int][]string{1: raw}}
	failed := subscription.InventoryIdentity(raw[1])
	var gotBasis []string
	health := func(_ context.Context, id int, basis []string) func(string) NodeHealth {
		gotBasis = basis
		return func(identity string) NodeHealth {
			if identity == failed {
				return NodeHealth{Status: HealthFailed}
			}
			if identity == subscription.InventoryIdentity(raw[5]) {
				ms := 80
				return NodeHealth{Status: HealthAlive, LatencyMS: &ms}
			}
			return NodeHealth{}
		}
	}
	never := Fetcher{Fetch: func(context.Context, string, string) (Response, error) {
		t.Fatal("cache-only combination must not fetch")
		return Response{}, errors.New("unreachable")
	}}
	limit := 1.0
	o := Options{UserAgent: "clash-meta/2.5.0", CacheOnly: true, Health: health,
		Pick: Pick{Regions: []string{"香港"}, MaxMultiplier: &limit, IncludeUnlabeledMultiplier: true, Keywords: []string{"iplc", "02"}, OnlyAlive: true}}
	src := Source{ID: 1, Name: "机场A", URL: "https://example.com/sub"}
	out, results, report, err := CombineReport(context.Background(), never, cache, []Source{src}, nil, o, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].FromCache || len(gotBasis) != len(raw) {
		t.Fatalf("cache basis not used: %+v %d", results[0], len(gotBasis))
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// 地区去掉日本、美国；倍率去掉 x2；关键词去掉没有 IPLC / 02 的；可用性去掉检测失败的 02。
	if len(lines) != 2 || !strings.Contains(out, "03") || !strings.Contains(out, "04") {
		t.Fatalf("unexpected pick result: %q", out)
	}
	want := map[string]int{StepRegion: 2, StepMultiplier: 1, StepKeyword: 0, StepAlive: 1}
	for _, step := range report.Steps {
		if n, ok := want[step.Step]; ok && step.Removed != n {
			t.Fatalf("step %s removed %d, want %d (%+v)", step.Step, step.Removed, n, report.Steps)
		}
	}
	if report.Merged != len(raw) || len(report.Nodes) != 2 {
		t.Fatalf("report = %+v", report)
	}
	for _, node := range report.Nodes {
		if node.Kind != "source" || node.SourceID != 1 {
			t.Fatalf("origin lost: %+v", node)
		}
		if strings.Contains(node.Link, "04") && (node.Health.Status != HealthAlive || node.Health.LatencyMS == nil) {
			t.Fatalf("health not attached: %+v", node)
		}
		if strings.Contains(node.Link, "03") && node.Health.Status != HealthUnchecked {
			t.Fatalf("unchecked node should stay unchecked: %+v", node)
		}
	}

	// 不收未标倍率的节点时，IPLC 03 也被去掉。
	o.Pick.IncludeUnlabeledMultiplier = false
	out, _, _, err = CombineReport(context.Background(), never, cache, []Source{src}, nil, o, 1<<20)
	if err != nil || strings.Contains(out, "03") || !strings.Contains(out, "04") {
		t.Fatalf("unlabeled multiplier should be excluded: %q %v", out, err)
	}

	// 没有缓存的源不拉取，明确报告未缓存。
	_, results, _, err = CombineReport(context.Background(), never, &memCache{m: map[int][]string{}}, []Source{src}, nil, o, 1<<20)
	if err != nil || results[0].Error != "source_not_cached" {
		t.Fatalf("missing cache result = %+v %v", results, err)
	}

	bad := 0.0
	if _, _, _, err = CombineReport(context.Background(), never, cache, []Source{src}, nil, Options{Pick: Pick{MaxMultiplier: &bad}}, 1<<20); err == nil {
		t.Fatal("invalid multiplier accepted")
	}
}
