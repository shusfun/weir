package center

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/kerneldriver"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/packages/contracts"
)

// 驱动管理：列表、详情与历史、保存（新增或覆盖内置）、停用、恢复内置、退回。每次保存新增一个版本。
// 保存前检查：严格解析与展开 → 为每个方案生成样例配置并用对应版本内核检查 → 改的是在用驱动时在一台 VPS 上实测。
// 规则见 design.html「版本与驱动管理」。

func (s *Server) driverRoutes(admin *gin.RouterGroup) {
	admin.GET("/drivers", s.listDrivers)
	admin.GET("/drivers/:name", s.getDriver)
	admin.POST("/drivers/check", s.checkDriverHandler)
	admin.PUT("/drivers/:name", s.saveDriverHandler)
	admin.POST("/drivers/:name/disable", s.toggleDriver(true))
	admin.POST("/drivers/:name/enable", s.toggleDriver(false))
	admin.POST("/drivers/:name/restore", s.restoreDriver)
	admin.POST("/drivers/:name/rollback", s.rollbackDriver)
}

// driverUse 是用到某驱动的内核版本与 VPS。
type driverUse struct {
	Versions []string  `json:"versions"`
	Gates    []string  `json:"gates"`
	GateRefs []gateRef `json:"gate_refs"`
}

func (s *Server) driverUsage(ctx context.Context, d kernels.Drivers) map[string]driverUse {
	out := map[string]driverUse{}
	pick := func(kernel, version string) string {
		if drv, _, err := d.Select(kernel, version); err == nil {
			return drv.Name
		}
		return ""
	}
	for _, kernel := range kernelconf.Kernels {
		seen := map[string]bool{}
		for _, arch := range []string{"amd64", "arm64"} {
			for _, v := range s.kernelCache().Versions(kernel, arch) {
				if seen[v] {
					continue
				}
				seen[v] = true
				if n := pick(kernel, v); n != "" {
					u := out[n]
					u.Versions = append(u.Versions, v)
					out[n] = u
				}
			}
		}
	}
	gates, _ := s.Store.Ent.Gate.Query().All(ctx)
	for _, g := range gates {
		for kernel, v := range g.KernelsRunning {
			if n := pick(kernel, v); n != "" {
				u := out[n]
				u.Gates = append(u.Gates, g.Name)
				u.GateRefs = append(u.GateRefs, gateRef{g.ID, g.Name})
				out[n] = u
			}
		}
	}
	return out
}

func (s *Server) listDrivers(c *gin.Context) {
	ctx := c.Request.Context()
	d := s.drivers(ctx)
	usage := s.driverUsage(ctx, d)
	list := d.List()
	items := make([]gin.H, 0, len(list))
	for _, e := range list {
		u := usage[e.Name]
		items = append(items, gin.H{"name": e.Name, "kernel": e.Kernel, "min_version": e.MinVersion, "max_version": e.MaxVersion, "extends": e.Extends,
			"source": e.Source, "disabled": e.Disabled, "error": e.Error, "versions": nonNil(u.Versions), "gates": nonNil(u.Gates), "gate_refs": nonNilRefs(u.GateRefs)})
	}
	c.JSON(200, gin.H{"drivers": items})
}

func nonNilRefs(s []gateRef) []gateRef {
	if s == nil {
		return []gateRef{}
	}
	return s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// driverHistory 是某名称的保存记录（新到旧）。
func (s *Server) driverHistory(ctx context.Context, name string) ([]*ent.KernelDriver, error) {
	return s.Store.Ent.KernelDriver.Query().Where(kerneldriver.NameEQ(name)).Order(ent.Desc(kerneldriver.FieldRevision)).Limit(50).All(ctx)
}

func (s *Server) getDriver(c *gin.Context) {
	ctx := c.Request.Context()
	name := c.Param("name")
	d := s.drivers(ctx)
	var entry *kernels.Entry
	for _, e := range d.List() {
		if e.Name == name {
			e := e
			entry = &e
		}
	}
	if entry == nil {
		failure(c, 404, "driver_not_found")
		return
	}
	_, full, rerr := d.Resolve(name)
	rows, _ := s.driverHistory(ctx, name)
	history := []gin.H{}
	for i, r := range rows {
		prev := ""
		if i+1 < len(rows) {
			prev = rows[i+1].Text
		} else if b, ok := kernels.Builtin()[name]; ok {
			prev = b
		}
		cur := r.Text
		if cur == "" {
			cur = kernels.Builtin()[name]
		}
		history = append(history, gin.H{"revision": r.Revision, "disabled": r.Disabled, "note": r.Note, "source": r.Source, "at": r.CreatedAt,
			"restored": r.Text == "", "diff": diffLines(prev, cur)})
	}
	out := gin.H{"driver": entry, "resolved": full, "history": history, "builtin": kernels.Builtin()[name]}
	if rerr != nil {
		out["resolve_error"] = rerr.Error()
	}
	out["usage"] = s.driverUsage(ctx, d)[name]
	c.JSON(200, out)
}

// DriverCheck 是保存前检查的结果。State：passed / failed / unknown（中心没有这个内核或不是 Linux，只做了生成检查）。
type DriverCheck struct {
	Name    string           `json:"name"`
	Kernel  string           `json:"kernel"`
	State   string           `json:"state"`
	Error   string           `json:"error,omitempty"`
	Version string           `json:"version,omitempty"` // 检查用的内核版本
	Samples []DriverSample   `json:"samples"`
	Live    *DriverLiveCheck `json:"live,omitempty"`
	Diff    []diffLine       `json:"diff,omitempty"`
}

// DriverSample 是一个方案的样例检查。
type DriverSample struct {
	Preset string `json:"preset"`
	State  string `json:"state"`
	Log    string `json:"log,omitempty"`
}

// DriverLiveCheck 是在 VPS 上的实测：改的是在用驱动时，找一台运行着它覆盖的内核版本、有启用节点的 VPS。
type DriverLiveCheck struct {
	State string `json:"state"` // passed / failed / skipped
	Gate  string `json:"gate,omitempty"`
	Node  string `json:"node,omitempty"`
	Error string `json:"error,omitempty"`
}

// checkDriver 检查一份驱动原文（name 为它的名称）：展开、样例配置、内核检查、VPS 实测。
func (s *Server) checkDriver(ctx context.Context, name, text string, live bool) DriverCheck {
	res := DriverCheck{Name: name, State: "failed", Samples: []DriverSample{}}
	cur := s.drivers(ctx)
	before, _ := cur.Saved[name]
	old := before.Text
	if old == "" {
		old = kernels.Builtin()[name]
	}
	res.Diff = diffLines(old, text)
	draft := kernels.Drivers{Saved: map[string]kernels.Saved{}}
	for k, v := range cur.Saved {
		draft.Saved[k] = v
	}
	draft.Saved[name] = kernels.Saved{Name: name, Text: text}
	drv, full, err := draft.Resolve(name)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Kernel = drv.Kernel
	// 继承了它的驱动也要能展开。
	for _, e := range draft.List() {
		if e.Error != "" {
			res.Error = "保存后 " + e.Name + " 无法展开：" + e.Error
			return res
		}
	}
	dir, err := os.MkdirTemp(s.dataDir(), "driver-check-")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer os.RemoveAll(dir)
	certPEM, keyPEM, err := selfSigned("check.weir.local")
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "1.crt"), []byte(certPEM), 0o600)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "1.key"), []byte(keyPEM), 0o600)
	}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	bin, version := s.checkBinary(drv)
	res.Version = version
	failed, unknown := false, bin == ""
	for _, p := range drv.SortedPresets() {
		sample := DriverSample{Preset: p.ID, State: "passed"}
		body, err := samplePreset(drv, full, Preset{Preset: p, Kernel: drv.Kernel}, dir)
		switch {
		case err != nil:
			sample.State, sample.Log = "failed", err.Error()
		case bin == "":
			sample.State = "unknown"
		default:
			if out, err := runCheck(ctx, bin, drv, body, dir, p.ID); err != nil {
				sample.State, sample.Log = "failed", out
			}
		}
		failed = failed || sample.State == "failed"
		res.Samples = append(res.Samples, sample)
	}
	if len(drv.Presets) == 0 {
		res.Error = "驱动没有任何方案，无法生成样例配置检查"
		return res
	}
	switch {
	case failed:
		res.Error = "样例配置检查未通过"
		return res
	case unknown:
		res.State = "unknown"
		if runtime.GOOS != "linux" {
			res.Error = "中心不是 Linux，只做了生成检查"
		} else {
			res.Error = "中心没有 " + drv.Kernel + " 在这个驱动版本区间里的程序，只做了生成检查；先在内核页下载一个版本"
		}
	default:
		res.State = "passed"
	}
	if live {
		res.Live = s.liveCheck(ctx, drv, full)
		if res.Live.State == "failed" {
			res.State, res.Error = "failed", "VPS 实测未通过"
		}
	}
	return res
}

// checkBinary 选中心本机架构、在驱动区间里的最新已下载版本（没有或不是 Linux 时为空）。
func (s *Server) checkBinary(d kernelconf.Driver) (string, string) {
	if runtime.GOOS != "linux" {
		return "", ""
	}
	for _, v := range s.kernelCache().Versions(d.Kernel, runtime.GOARCH) {
		if d.Covers(v) {
			if path, _, err := s.kernelCache().Get(d.Kernel, v, runtime.GOARCH); err == nil {
				return path, v
			}
		}
	}
	return "", ""
}

// samplePreset 按方案生成一个样例节点与两个凭据，用驱动生成配置。
func samplePreset(drv kernelconf.Driver, full string, p Preset, certDir string) ([]byte, error) {
	settings, err := presetSettings(p)
	if err != nil {
		return nil, err
	}
	in := contracts.Inbound{ID: 1, Tag: "sample", Kernel: drv.Kernel, Protocol: p.Protocol, Port: 20001, Network: p.Network, Security: p.Security, Settings: settings}
	if p.Cert != "none" {
		in.CertID = 1
	}
	var creds []contracts.Credential
	for i := int64(1); i <= 2; i++ {
		id, pw := newSecret(p.Protocol)
		creds = append(creds, contracts.Credential{ID: i, InboundID: 1, StatsKey: fmt.Sprintf("s%d", i), UserKey: fmt.Sprintf("u%d", i), UUID: id, Password: pw})
	}
	cfg := contracts.GateConfig{Ports: contracts.PortPlan{XrayStats: 20002, SingboxStats: 20003}, Inbounds: []contracts.Inbound{in}, Credentials: creds,
		Drivers: map[string]string{drv.Kernel: full}}
	out, err := kernelconf.Build(cfg, kernelconf.Options{CertDir: certDir})
	if err != nil {
		return nil, err
	}
	if drv.Kernel == "xray" {
		return out.Xray, nil
	}
	return out.Singbox, nil
}

func runCheck(ctx context.Context, bin string, drv kernelconf.Driver, body []byte, dir, id string) (string, error) {
	path := filepath.Join(dir, id+".json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return err.Error(), err
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, kernelconf.Args(drv.Check, path)...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if len(text) > 1200 {
		text = text[len(text)-1200:]
	}
	if err != nil && text == "" {
		text = err.Error() // 内核根本没能运行（如程序损坏、架构不对）时没有输出，交回启动错误本身
	}
	return text, err
}

// liveCheck 在一台运行着这个驱动区间里内核版本、有启用节点的 VPS 上，用草稿驱动实测一个节点。
func (s *Server) liveCheck(ctx context.Context, drv kernelconf.Driver, full string) *DriverLiveCheck {
	gates, err := s.Store.Ent.Gate.Query().All(ctx)
	if err != nil {
		return &DriverLiveCheck{State: "skipped", Error: err.Error()}
	}
	for _, g := range gates {
		if !gateOnline(g) || !drv.Covers(g.KernelsRunning[drv.Kernel]) {
			continue
		}
		nodes, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID)).All(ctx)
		if err != nil {
			continue
		}
		for _, n := range nodes {
			if !n.Enabled || n.Kernel != drv.Kernel {
				continue
			}
			res, err := s.testNodeWith(ctx, n, n.Settings, map[string]string{drv.Kernel: full})
			if err != nil {
				msg := err.Error()
				if log, ok := res["log"]; ok {
					msg += "：" + fmt.Sprint(log)
				}
				return &DriverLiveCheck{State: "failed", Gate: g.Name, Node: n.Name, Error: msg}
			}
			return &DriverLiveCheck{State: "passed", Gate: g.Name, Node: n.Name}
		}
	}
	return &DriverLiveCheck{State: "skipped", Error: "没有在线的 VPS 运行着这个驱动覆盖的内核版本并有启用的节点，未做 VPS 实测"}
}

func (s *Server) checkDriverHandler(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
		Text string `json:"text"`
		Live bool   `json:"live"`
	}
	if !bind(c, &req) {
		return
	}
	c.JSON(200, gin.H{"check": s.checkDriver(c.Request.Context(), req.Name, req.Text, req.Live)})
}

// saveDriverVersion 新增一个保存版本并让 Gate 重新编译配置。
func (s *Server) saveDriverVersion(ctx context.Context, name, kernel, text string, disabled bool, note, source string, actor int) (int, error) {
	if len(text) > 256<<10 {
		return 0, errors.New("驱动太大")
	}
	rev := 1
	if last, err := s.Store.Ent.KernelDriver.Query().Where(kerneldriver.NameEQ(name)).Order(ent.Desc(kerneldriver.FieldRevision)).First(ctx); err == nil {
		rev = last.Revision + 1
	}
	_, err := s.Store.Ent.KernelDriver.Create().SetName(name).SetKernel(kernel).SetRevision(rev).SetText(text).SetDisabled(disabled).
		SetNote(trimTo(note, 500)).SetSource(source).SetActor(actor).Save(ctx)
	if err != nil {
		return 0, err
	}
	s.dropDriverCache()
	if ids, err := s.Store.Ent.Gate.Query().IDs(ctx); err == nil {
		_ = s.bumpGates(ctx, ids)
	}
	return rev, nil
}

func (s *Server) saveDriverHandler(c *gin.Context) {
	var req struct {
		Text string `json:"text"`
		Note string `json:"note"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	name := c.Param("name")
	if got := kernelconf.RawField(req.Text, "name"); got != name {
		failDetail(c, 400, "driver_invalid", fmt.Sprintf("驱动里写的名称是 %q，与 %s 不一致", got, name), nil)
		return
	}
	check := s.checkDriver(ctx, name, req.Text, s.driverInUse(ctx, name))
	if check.State == "failed" {
		c.AbortWithStatusJSON(422, gin.H{"error": gin.H{"code": "driver_check_failed", "message": check.Error}, "check": check})
		return
	}
	rev, err := s.saveDriverVersion(ctx, name, check.Kernel, req.Text, false, req.Note, "admin", c.GetInt("user_id"))
	if err != nil {
		failDetail(c, 500, "save_failed", err.Error(), nil)
		return
	}
	s.audit(c, "driver.save", fmt.Sprintf("%s #%d", name, rev), "ok")
	c.JSON(200, gin.H{"revision": rev, "check": check})
}

// driverInUse：有已下载的内核版本或运行中的 VPS 用到它。
func (s *Server) driverInUse(ctx context.Context, name string) bool {
	u := s.driverUsage(ctx, s.drivers(ctx))[name]
	return len(u.Versions)+len(u.Gates) > 0
}

func (s *Server) toggleDriver(disable bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		name := c.Param("name")
		d := s.drivers(ctx)
		text, ok := d.Saved[name]
		if _, builtin := kernels.Builtin()[name]; !ok && !builtin {
			failure(c, 404, "driver_not_found")
			return
		}
		kernel := ""
		if drv, _, err := d.Resolve(name); err == nil {
			kernel = drv.Kernel
		}
		if disable && len(s.driverUsage(ctx, d)[name].Gates) > 0 {
			failDetail(c, 409, "driver_in_use", "仍有 VPS 正在用这个驱动，先换内核版本或改用其他驱动", nil)
			return
		}
		rev, err := s.saveDriverVersion(ctx, name, kernel, text.Text, disable, "", "admin", c.GetInt("user_id"))
		if err != nil {
			failure(c, 500, "save_failed")
			return
		}
		action := "driver.enable"
		if disable {
			action = "driver.disable"
		}
		s.audit(c, action, fmt.Sprintf("%s #%d", name, rev), "ok")
		c.JSON(200, gin.H{"revision": rev})
	}
}

// restoreDriver 恢复内置版本（只对内置驱动）。
func (s *Server) restoreDriver(c *gin.Context) {
	ctx := c.Request.Context()
	name := c.Param("name")
	if _, ok := kernels.Builtin()[name]; !ok {
		failDetail(c, 404, "driver_not_builtin", "这不是内置驱动，没有可以恢复的内置版本", nil)
		return
	}
	drv, _, err := kernels.Drivers{}.Resolve(name)
	if err != nil {
		failDetail(c, 500, "driver_invalid", err.Error(), nil)
		return
	}
	rev, err := s.saveDriverVersion(ctx, name, drv.Kernel, "", false, "恢复内置", "admin", c.GetInt("user_id"))
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "driver.restore", fmt.Sprintf("%s #%d", name, rev), "ok")
	c.JSON(200, gin.H{"revision": rev})
}

// rollbackDriver 把某个历史版本的原文作为新版本保存（同样先检查）。
func (s *Server) rollbackDriver(c *gin.Context) {
	var req struct {
		Revision int `json:"revision"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	name := c.Param("name")
	row, err := s.Store.Ent.KernelDriver.Query().Where(kerneldriver.NameEQ(name), kerneldriver.RevisionEQ(req.Revision)).Only(ctx)
	if err != nil {
		failure(c, 404, "revision_not_found")
		return
	}
	text := row.Text
	if text == "" {
		text = kernels.Builtin()[name]
	}
	check := s.checkDriver(ctx, name, text, s.driverInUse(ctx, name))
	if check.State == "failed" {
		c.AbortWithStatusJSON(422, gin.H{"error": gin.H{"code": "driver_check_failed", "message": check.Error}, "check": check})
		return
	}
	rev, err := s.saveDriverVersion(ctx, name, check.Kernel, row.Text, row.Disabled, fmt.Sprintf("退回到 #%d", req.Revision), "admin", c.GetInt("user_id"))
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "driver.rollback", fmt.Sprintf("%s #%d → #%d", name, req.Revision, rev), "ok")
	c.JSON(200, gin.H{"revision": rev, "check": check})
}

// gateOnline：90 秒内有心跳（与 VPS 列表一致）。
func gateOnline(g *ent.Gate) bool {
	return g.LastSeenAt != nil && time.Since(*g.LastSeenAt) < 90*time.Second
}
