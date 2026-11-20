package center

import (
	"context"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

// 订阅源节点检测结果的存储契约：订阅源检测负责写，订阅组「只要可用」与预览负责读。
// 机场节点会改名、增减，所以逐节点结果按连接身份（subscription.InventoryIdentity）记录；
// Generation 是这次拉取内容的摘要，内容一变旧结果作废，读的一方只认同一 generation 的结果。
const sourceHealthKind = "subscription-source-health"

// 节点检测状态：alive 可用、failed 不可用、unchecked 本轮没测（中心兜底、超出上限、让路推迟）。
const (
	nodeAlive     = "alive"
	nodeFailed    = "failed"
	nodeUnchecked = "unchecked"
)

type sourceNodeHealth struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	LatencyMS *int   `json:"latency_ms,omitempty"`
	Why       string `json:"why,omitempty"`
}

type sourceHealth struct {
	SourceID   int                         `json:"source_id"`
	Generation string                      `json:"generation"`
	CheckedAt  *time.Time                  `json:"checked_at,omitempty"`
	Location   string                      `json:"location"`
	Reason     string                      `json:"reason,omitempty"`
	TraceID    string                      `json:"trace_id,omitempty"`
	Nodes      map[string]sourceNodeHealth `json:"nodes"`
}

func sourceHealthKey(id int) string { return "source:" + itoa(id) }

// loadSourceHealth 读一个订阅源最近的检测结果；没有记录时返回 nil。
func (s *Server) loadSourceHealth(ctx context.Context, id int) (*sourceHealth, error) {
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(sourceHealthKind), resource.KeyEQ(sourceHealthKey(id))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var h sourceHealth
	if err = decodeResource(row, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// saveSourceHealth 覆盖写入一个订阅源的检测结果。
func (s *Server) saveSourceHealth(ctx context.Context, h sourceHealth) error {
	key := sourceHealthKey(h.SourceID)
	n, err := s.Store.Ent.Resource.Update().Where(resource.KindEQ(sourceHealthKind), resource.KeyEQ(key)).SetData(toMap(h)).Save(ctx)
	if err != nil || n > 0 {
		return err
	}
	return s.Store.Ent.Resource.Create().SetKind(sourceHealthKind).SetKey(key).SetOwnerID(h.SourceID).SetData(toMap(h)).Exec(ctx)
}

// nodeStatusFor 给出某节点在当前内容版本下的检测状态；版本不符或没有记录都算未检测。
func (h *sourceHealth) nodeStatusFor(generation, identity string) sourceNodeHealth {
	if h == nil || h.Generation != generation {
		return sourceNodeHealth{Status: nodeUnchecked}
	}
	if n, ok := h.Nodes[identity]; ok {
		return n
	}
	return sourceNodeHealth{Status: nodeUnchecked}
}

// sourceGeneration 是一份原始节点内容的版本：检测写入与订阅组读取都用它对齐，内容一变旧结果即作废。
func sourceGeneration(raw []string) string { return version(raw) }
