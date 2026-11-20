package center

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/exitbinding"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/route"
)

// exitDeletion 是删除代理的级联计划：引用它的节点（连同以节点为入口的链式代理）、
// 用户链式代理和分流规则一并删除。链式代理的 exit_id 必填，不能只停用后留着悬空引用。
type exitDeletion struct {
	exit          *ent.Exit
	nodes         []*ent.Node
	nodeChain     map[int][]*ent.Chain
	chains        []*ent.Chain
	routes        []*ent.Route
	creds         int
	credentialIDs []int
	gateIDs       []int
	// blocked：要删的节点还被其他代理用作出口节点（exit_node_id），删掉会破坏那些代理
	blocked []int
}

var errExitDeleteStale = errors.New("exit_delete_stale")

type exitDeleteClaim struct {
	ExitID   int
	Actor    int
	Revision string
	Expires  time.Time
}

func exitDeletionPlan(ctx context.Context, db *ent.Client, e *ent.Exit) (*exitDeletion, error) {
	p := &exitDeletion{exit: e, nodeChain: map[int][]*ent.Chain{}}
	var err error
	if p.nodes, err = db.Node.Query().Where(node.ExitIDEQ(e.ID)).Order(ent.Asc(node.FieldID)).All(ctx); err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var nodeIDs, chainIDs, entryIDs []int
	for _, n := range p.nodes {
		nodeIDs = append(nodeIDs, n.ID)
		p.gateIDs = append(p.gateIDs, n.GateID)
		blocked, err := db.Exit.Query().Where(entexit.ExitNodeIDEQ(n.ID), entexit.IDNEQ(e.ID)).IDs(ctx)
		if err != nil {
			return nil, err
		}
		p.blocked = append(p.blocked, blocked...)
		chs, err := db.Chain.Query().Where(chain.EntryNodeIDEQ(n.ID)).Order(ent.Asc(chain.FieldID)).All(ctx)
		if err != nil {
			return nil, err
		}
		p.nodeChain[n.ID] = chs
		for _, ch := range chs {
			seen[ch.ID] = true
			chainIDs = append(chainIDs, ch.ID)
		}
	}
	chs, err := db.Chain.Query().Where(chain.ExitIDEQ(e.ID)).Order(ent.Asc(chain.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, ch := range chs {
		if !seen[ch.ID] {
			p.chains = append(p.chains, ch)
			chainIDs = append(chainIDs, ch.ID)
			entryIDs = append(entryIDs, ch.EntryNodeID)
		}
	}
	if p.routes, err = db.Route.Query().Where(route.Or(route.ExitIDEQ(e.ID), route.NodeIDIn(nodeIDs...))).Order(ent.Asc(route.FieldID)).All(ctx); err != nil {
		return nil, err
	}
	for _, rule := range p.routes {
		p.gateIDs = append(p.gateIDs, rule.GateID)
	}
	credentials, err := db.Credential.Query().Where(credential.Or(credential.NodeIDIn(nodeIDs...), credential.ChainIDIn(chainIDs...), credential.And(credential.InternalEQ(true), credential.StatsKeyEQ(exitCredentialKey(e.ID))))).Order(ent.Asc(credential.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range credentials {
		p.credentialIDs = append(p.credentialIDs, item.ID)
		entryIDs = append(entryIDs, item.NodeID)
		if !item.Internal && item.ProbeParentID == nil {
			p.creds++
		}
	}
	entryNodes, err := db.Node.Query().Where(node.IDIn(entryIDs...)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, entry := range entryNodes {
		p.gateIDs = append(p.gateIDs, entry.GateID)
	}
	p.gateIDs = dedupe(p.gateIDs)
	slices.Sort(p.gateIDs)
	p.blocked = dedupe(p.blocked)
	slices.Sort(p.blocked)
	return p, nil
}

func (p *exitDeletion) preview(name string) []string {
	out := []string{"删除代理「" + name + "」"}
	if len(p.nodes) > 0 {
		out = append(out, fmt.Sprintf("一并删除使用它的 %d 个节点：%d 份用户凭据失效，已导入客户端的这些节点随即不可用", len(p.nodes), p.creds))
	}
	if len(p.chains) > 0 {
		out = append(out, fmt.Sprintf("一并删除落地到它的 %d 条用户链式代理，并从客户端分组里移除", len(p.chains)))
	}
	if len(p.routes) > 0 {
		out = append(out, fmt.Sprintf("一并删除走它的 %d 条分流规则", len(p.routes)))
	}
	return out
}

func (p *exitDeletion) apply(ctx context.Context, tx *ent.Tx) (err error) {
	stage := operationStage(ctx, "exit.delete.cascade", "exit.id", p.exit.ID, "nodes", len(p.nodes), "chains", len(p.chains), "routes", len(p.routes))
	defer func() { stage(err) }()
	if _, err = tx.Route.Delete().Where(route.ExitIDEQ(p.exit.ID)).Exec(ctx); err != nil {
		return err
	}
	for _, ch := range p.chains {
		if err = removeChainTx(ctx, tx, ch); err != nil {
			return err
		}
	}
	for _, n := range p.nodes {
		if err = removeNodeTx(ctx, tx, n, p.nodeChain[n.ID]); err != nil {
			return err
		}
	}
	if _, err = tx.ExitBinding.Delete().Where(exitbinding.ExitIDEQ(p.exit.ID)).Exec(ctx); err != nil {
		return err
	}
	if _, err = tx.Credential.Delete().Where(credential.InternalEQ(true), credential.StatsKeyEQ(exitCredentialKey(p.exit.ID))).Exec(ctx); err != nil {
		return err
	}
	if err = tx.Exit.DeleteOne(p.exit).Exec(ctx); err != nil {
		return err
	}
	return advanceRevocations(ctx, tx, p.gateIDs)
}

// gates 是需要推进配置版本的 VPS：被删节点、链式入口与规则所在的 VPS。
func (p *exitDeletion) gates() []int {
	return p.gateIDs
}

// exitDeletePreview 给删除确认框用：列出会随代理一起删除的节点、链式代理和规则。
func (p *exitDeletion) objects() []gin.H {
	ref := func(typ string, id int, name string) gin.H { return gin.H{"type": typ, "id": id, "name": name} }
	items := []gin.H{}
	for _, n := range p.nodes {
		items = append(items, ref("node", n.ID, n.Name))
		for _, ch := range p.nodeChain[n.ID] {
			items = append(items, ref("chain", ch.ID, ch.Name))
		}
	}
	for _, ch := range p.chains {
		items = append(items, ref("chain", ch.ID, ch.Name))
	}
	for _, r := range p.routes {
		items = append(items, ref("route", r.ID, firstNonEmpty(r.Domain, r.IP)))
	}
	return items
}

func (p *exitDeletion) revision() string {
	data, _ := json.Marshal([]any{p.exit.OwnerUserID, p.exit.Remark, p.objects(), p.blocked, p.credentialIDs, p.gateIDs})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func (s *Server) exitDeletePreview(c *gin.Context, admin bool) {
	ctx := c.Request.Context()
	tx, err := s.Store.Ent.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	defer tx.Rollback()
	e, ok := exitParamWith(c, tx.Client(), admin)
	if !ok {
		return
	}
	p, err := exitDeletionPlan(ctx, tx.Client(), e)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	claim := exitDeleteClaim{ExitID: e.ID, Actor: c.GetInt("user_id"), Revision: p.revision(), Expires: time.Now().Add(10 * time.Minute)}
	data, err := json.Marshal(claim)
	if err != nil {
		failInternal(c, 500, "preview_failed", err)
		return
	}
	token, err := s.Crypto.Encrypt(string(data))
	if err != nil {
		failInternal(c, 500, "preview_failed", err)
		return
	}
	c.JSON(200, gin.H{"preview": p.preview(firstNonEmpty(e.Remark, e.Tag)), "removes": p.objects(), "blocked_by_exits": append([]int{}, p.blocked...), "preview_token": token})
}
