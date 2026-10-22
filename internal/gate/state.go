package gate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imzyb/MiGate/packages/contracts"
)

// Counter 是某凭据的累计字节数。
type Counter struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// State 是 Agent 的本地状态，原子写入 <Data>/state.json。中心离线时依靠它继续执行额度与到期。
type State struct {
	Config    *contracts.GateConfig `json:"config,omitempty"` // 最后一份成功应用的配置
	Applied   int64                 `json:"applied"`
	Epoch     string                `json:"epoch"` // 本地计量纪元；状态丢失时换新，中心据此从零起算
	Seq       int64                 `json:"seq"`
	Totals    map[string]Counter    `json:"totals"`   // 本纪元内各凭据累计（单调递增）
	Last      map[string]Counter    `json:"last"`     // 上一次的内核原始读数
	Baseline  map[string]int64      `json:"baseline"` // 收到配置时各用户的本地累计
	Signature string                `json:"signature"`
}

func newEpoch() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102") + "-" + hex.EncodeToString(b)
}

func loadState(path string) State {
	var st State
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if st.Epoch == "" {
		st = State{Epoch: newEpoch()}
	}
	if st.Totals == nil {
		st.Totals = map[string]Counter{}
	}
	if st.Last == nil {
		st.Last = map[string]Counter{}
	}
	if st.Baseline == nil {
		st.Baseline = map[string]int64{}
	}
	return st
}

func (st *State) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err = os.WriteFile(path+".new", b, 0600); err != nil {
		return err
	}
	return os.Rename(path+".new", path)
}

// absorb 把内核原始累计读数并入本地累计。restarted 为真（内核 PID 变化）或读数回退时，
// 说明内核已重启、计数从零开始，新读数整份计入。
func (st *State) absorb(raw map[string]Counter, restarted bool) {
	for key, r := range raw {
		last, tot := st.Last[key], st.Totals[key]
		if restarted || r.Up < last.Up || r.Down < last.Down {
			tot.Up += r.Up
			tot.Down += r.Down
		} else {
			tot.Up += r.Up - last.Up
			tot.Down += r.Down - last.Down
		}
		st.Totals[key], st.Last[key] = tot, r
	}
}

// localUsage 是该用户在本 Gate 上全部凭据的本地累计。
func (st *State) localUsage(cfg contracts.GateConfig, user string) int64 {
	var sum int64
	for _, c := range cfg.Credentials {
		if c.UserKey == user {
			t := st.Totals[c.StatsKey]
			sum += t.Up + t.Down
		}
	}
	return sum
}

// resetBaseline 在应用新配置时记录各用户的本地累计：此后本地新增用量叠加在中心下发的 Used 之上。
func (st *State) resetBaseline(cfg contracts.GateConfig) {
	st.Baseline = map[string]int64{}
	for user := range cfg.Budgets {
		st.Baseline[user] = st.localUsage(cfg, user)
	}
}

// exhausted 返回在本 Gate 上额度已用尽的用户（中心已记账用量 + 收到配置后的本地新增 ≥ 已分配额度）。
func (st *State) exhausted(cfg contracts.GateConfig) map[string]bool {
	out := map[string]bool{}
	for user, b := range cfg.Budgets {
		if b.Unlimited {
			continue
		}
		used := b.Used + st.localUsage(cfg, user) - st.Baseline[user]
		if used >= b.Allocated {
			out[user] = true
		}
	}
	return out
}

// signature 标识「此刻应下发的凭据集合」：到期或额度变化导致集合变化时，Agent 在本地重建配置。
func signature(cfg contracts.GateConfig, now time.Time, exhausted map[string]bool) string {
	var keys []string
	for _, c := range cfg.Credentials {
		if c.ExpiresAt != nil && !now.Before(*c.ExpiresAt) {
			continue
		}
		if !c.Internal && exhausted[c.UserKey] {
			continue
		}
		keys = append(keys, c.StatsKey)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
