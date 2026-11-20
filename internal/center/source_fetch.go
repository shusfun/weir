package center

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/subgroup"
	"github.com/shusfun/weir/packages/contracts"
)

// 订阅源拉取的唯一选路入口：这个源指定的 Gate → 全局默认 Gate → 其他在线 Gate → 中心直连。
// 每台 Gate 单独派发（TargetGateID），失败再换下一台；没用上首选时把原因写进 RouteReason，页面如实显示。

// 单台 Gate 的拉取预算：一台卡住不拖垮整次拉取，留时间给后面的 Gate 和中心兜底。
const sourceFetchPerGate = 45 * time.Second

type fetchRoute struct {
	gates []int
	// notes 是选路时跳过首选的原因，按发生顺序。
	notes []string
}

// sourceFetchRoute 按订阅源与全局设置排出候选 Gate。sourceID 为 0 时只用全局默认与在线 Gate。
func (s *Server) sourceFetchRoute(ctx context.Context, sourceID int) (fetchRoute, error) {
	gate := 0
	if sourceID > 0 {
		if src, err := s.Store.Ent.SubSource.Get(ctx, sourceID); err == nil {
			gate = src.GateID
		}
	}
	return s.fetchRouteFor(ctx, gate)
}

// fetchRouteFor 以 preferred（0 表示没有单独指定）为首选排出候选 Gate；试拉也用它。
func (s *Server) fetchRouteFor(ctx context.Context, preferredGate int) (fetchRoute, error) {
	var route fetchRoute
	eligible, err := s.workerCandidates(ctx, contracts.TaskSubscriptionFetch, 0)
	if err != nil && !errors.Is(err, errNoWorker) {
		return route, err
	}
	ok := map[int]bool{}
	for _, id := range eligible {
		ok[id] = true
	}
	settings, err := s.subscriptionSettings(ctx)
	if err != nil {
		return route, err
	}
	preferred := []int{}
	if preferredGate > 0 {
		preferred = append(preferred, preferredGate)
	}
	if settings.DefaultGateID > 0 {
		preferred = append(preferred, settings.DefaultGateID)
	}
	seen := map[int]bool{}
	for _, id := range preferred {
		if seen[id] {
			continue
		}
		seen[id] = true
		if !ok[id] {
			route.notes = append(route.notes, "首选 VPS「"+s.gateLabel(ctx, id)+"」离线或不支持拉取订阅")
			continue
		}
		route.gates = append(route.gates, id)
	}
	for _, id := range eligible {
		if !seen[id] {
			seen[id] = true
			route.gates = append(route.gates, id)
		}
	}
	return route, nil
}

func (s *Server) gateLabel(ctx context.Context, id int) string {
	if g, err := s.Store.Ent.Gate.Get(ctx, id); err == nil && strings.TrimSpace(g.Name) != "" {
		return g.Name
	}
	return "VPS #" + itoa(id)
}

// fetchSubscriptionRouted 按 route 逐台 Gate 拉取，全部失败后中心直连兜底。
func (s *Server) fetchSubscriptionRouted(ctx context.Context, raw, ua string, route fetchRoute) (subgroup.Response, error) {
	notes := append([]string(nil), route.notes...)
	payload := contracts.SubscriptionFetchTask{Credentials: contracts.FetchSecrets{URL: raw}, UserAgent: ua, MaxBytes: 10 << 20}
	for i, gid := range route.gates {
		attempt, cancel := context.WithTimeout(ctx, sourceFetchPerGate)
		started := time.Now().UTC()
		body, _, err := s.dispatchWorkerTask(attempt, contracts.TaskSubscriptionFetch, payload, contracts.TaskRequirements{TargetGateID: gid}, []int{gid})
		cancel()
		recordSubscriptionStage(ctx, 0, subgroup.Stage{Operation: "subscription.source.fetch_route", Started: started, Finished: time.Now().UTC(), Error: errorCode(err)}, "center.fetchSubscriptionRouted")
		if err == nil {
			var result contracts.SubscriptionFetchResult
			if err = json.Unmarshal(body, &result); err == nil {
				if i > 0 || len(route.notes) > 0 {
					notes = append(notes, "改用「"+s.gateLabel(ctx, gid)+"」拉取")
				}
				return subgroup.Response{Body: result.Body, UserInfo: result.UserInfo, Title: result.Title, Location: "gate:" + itoa(gid), RouteReason: strings.Join(notes, "；")}, nil
			}
		}
		if ctx.Err() != nil {
			return subgroup.Response{}, ctx.Err()
		}
		notes = append(notes, "「"+s.gateLabel(ctx, gid)+"」拉取失败："+fetchErrorText(err))
	}
	if len(route.gates) == 0 {
		notes = append(notes, "没有可用的 VPS")
	}
	notes = append(notes, "由中心直接下载，未做节点检测")
	fallback, err := (subgroup.Fetcher{AllowPrivate: s.AllowPrivateSources}).Get(ctx, raw, ua)
	fallback.Location, fallback.RouteReason = "center-fallback", strings.Join(notes, "；")
	return fallback, err
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	var te *contracts.TaskError
	if errors.As(err, &te) && te.Code != "" {
		return te.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "fetch_failed"
}

func fetchErrorText(err error) string {
	switch code := errorCode(err); code {
	case "timeout":
		return "超时"
	case "fetch_failed":
		return "拉取请求失败"
	default:
		return code
	}
}
