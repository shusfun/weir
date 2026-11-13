package center

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/selfupdate"
)

type localGatePreview struct {
	Actor   int       `json:"actor"`
	Role    string    `json:"role"`
	Expires time.Time `json:"expires"`
	Action  string    `json:"action"`
}

func (s *Server) localGateState(c *gin.Context) gin.H {
	role := selfupdate.LocalRole()
	state, reason, id := "disabled", "", int64(0)
	if runtime.GOOS != "linux" || role != "center" && role != "standalone" {
		state, reason = "unavailable", "需要通过安装器部署的 Linux 中心"
	}
	if _, err := os.Stat(selfupdate.LocalGateConfig); err == nil {
		cfg, err := selfupdate.LocalIdentity()
		if err != nil {
			state, reason = "unknown", "本机设备身份无法读取，不能覆盖或重新配对"
		} else {
			g, err := s.Store.Ent.Gate.Get(c.Request.Context(), int(cfg.GateID))
			key, e := base64.StdEncoding.DecodeString(cfg.PrivateKey)
			valid := e == nil && len(key) == ed25519.PrivateKeySize
			if err != nil || !valid || !loopbackURL(cfg.Center) || cfg.Center != selfupdate.LocalListen() || valid && (g.Status != "active" || g.PublicKey != base64.StdEncoding.EncodeToString(ed25519.PrivateKey(key).Public().(ed25519.PublicKey))) {
				state, reason = "conflict", "本机设备未绑定当前中心，保留原身份并停止启用"
			} else {
				id = cfg.GateID
				if role == "standalone" && gateOnline(g) {
					state = "enabled"
				} else {
					state, reason = "pending", "本机执行端等待启动或恢复在线"
				}
			}
		}
	} else if !os.IsNotExist(err) {
		state, reason = "unknown", "本机设备文件状态未知"
	} else if role == "standalone" {
		state, reason = "pending", "等待本机自动配对"
	}
	out := gin.H{"role": role, "state": state, "reason": reason, "gate_id": id, "can_enable": state == "disabled" || state == "pending"}
	if st, ok := selfupdate.ReadStatus(); ok && st.Action == "enable-local-gate" {
		out["operation"] = st
	}
	return out
}

func (s *Server) getLocalGate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(200, s.localGateState(c))
}

func (s *Server) previewLocalGate(c *gin.Context) {
	state := s.localGateState(c)
	if state["state"] == "enabled" {
		c.JSON(200, state)
		return
	}
	if state["can_enable"] != true {
		failDetail(c, 409, "local_gate_unavailable", state["reason"].(string), nil)
		return
	}
	if why := selfUpdateBlocker(); why != "" {
		failDetail(c, 409, "local_gate_unavailable", why, nil)
		return
	}
	if err := selfupdate.PreflightLocalGate(); err != nil {
		failInternal(c, 409, "local_gate_preflight_failed", err)
		return
	}
	id := randomHex(16)
	p := localGatePreview{Actor: c.GetInt("user_id"), Role: selfupdate.LocalRole(), Expires: time.Now().Add(10 * time.Minute), Action: "enable-local-gate"}
	b, _ := json.Marshal(p)
	if err := s.Store.KV.Set(c.Request.Context(), "local-gate-preview:"+id, string(b), 10*time.Minute); err != nil {
		failInternal(c, 503, "local_gate_unavailable", err)
		return
	}
	c.JSON(200, gin.H{"id": id, "expires_at": p.Expires, "restart_required": true})
}

func (s *Server) confirmLocalGate(c *gin.Context) {
	ctx := c.Request.Context()
	err := s.Store.Lock.Do(ctx, "local-gate-enable", func(_ctx context.Context) error {
		if st := s.localGateState(c); st["state"] == "enabled" {
			c.JSON(200, st)
			return nil
		} else if st["can_enable"] != true {
			failure(c, 409, "local_gate_unavailable")
			return nil
		}
		raw, ok, err := s.Store.KV.Get(ctx, "local-gate-preview:"+c.Param("id"))
		if err != nil {
			return err
		}
		var p localGatePreview
		if !ok || json.Unmarshal([]byte(raw), &p) != nil || p.Actor != c.GetInt("user_id") || time.Now().After(p.Expires) || p.Role != selfupdate.LocalRole() || p.Action != "enable-local-gate" {
			failure(c, 409, "local_gate_preview_expired")
			return nil
		}
		id := "local-" + c.Param("id")
		if st, ok := selfupdate.ReadOperation(id); ok {
			c.JSON(200, st)
			return nil
		}
		if err = selfupdate.WriteRequest(selfupdate.Request{ID: id, Action: "enable-local-gate", TraceID: taskTrace(ctx)}); err != nil {
			return err
		}
		s.audit(c, "center.local_gate.enable", "center", "queued")
		c.JSON(202, gin.H{"id": id, "state": "queued"})
		return nil
	})
	if err != nil {
		failInternal(c, 503, "local_gate_enable_failed", err)
	}
}
