package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/exitbinding"
)

// migrateExitBindings 把仍标着入口 VPS 的目录出口改成独立资料，并为原来的 VPS 留下生效快照。
// 系统直连继续属于各自的 VPS。重复执行时已清空入口的出口会被跳过。
func (s *Store) migrateExitBindings(ctx context.Context) error {
	exits, err := s.Ent.Exit.Query().Where(exit.GateIDNotNil(), exit.TagNEQ("direct")).All(ctx)
	if err != nil {
		return err
	}
	for _, e := range exits {
		if e.GateID == nil {
			continue
		}
		gid := *e.GateID
		create := s.Ent.ExitBinding.Create().
			SetExitID(e.ID).
			SetGateID(gid).
			SetStatus(exitbinding.StatusApplied).
			SetKind(exitbinding.Kind(e.Kind)).
			SetRemark(e.Remark).
			SetAddress(e.Address).
			SetPort(e.Port).
			SetUsername(e.Username).
			SetPassword(e.Password).
			SetLink(e.Link).
			SetDesiredDigest("migrated")
		if e.ExitNodeID != nil {
			create.SetExitNodeID(*e.ExitNodeID)
		}
		if e.Source != nil {
			create.SetSource(e.Source)
		}
		if err = create.OnConflictColumns(exitbinding.FieldExitID, exitbinding.FieldGateID).Ignore().Exec(ctx); err != nil {
			return err
		}
		if s.KV != nil {
			owner := "exit-probe:exit:" + strconv.Itoa(e.ID)
			raw, ok, err := s.KV.Get(ctx, owner)
			if err != nil {
				return err
			}
			if ok {
				if err = s.KV.Set(ctx, owner+":gate:"+strconv.Itoa(gid), raw, 0); err != nil {
					return err
				}
				if err = s.KV.Del(ctx, owner); err != nil {
					return err
				}
			}
			sourceKey := "source-exit:exit:" + strconv.Itoa(e.ID)
			raw, ok, err = s.KV.Get(ctx, sourceKey)
			if err != nil {
				return err
			}
			if ok {
				if !json.Valid([]byte(raw)) {
					return fmt.Errorf("exit %d: invalid stored source snapshot", e.ID)
				}
				if err = s.Ent.ExitBinding.Update().Where(exitbinding.ExitIDEQ(e.ID), exitbinding.GateIDEQ(gid)).SetResolved(json.RawMessage(raw)).Exec(ctx); err != nil {
					return err
				}
				if err = s.KV.Set(ctx, sourceKey+":gate:"+strconv.Itoa(gid), raw, 0); err != nil {
					return err
				}
			}
		}
		if err = s.Ent.Exit.UpdateOneID(e.ID).ClearGateID().Exec(ctx); err != nil {
			return err
		}
	}
	// 已经迁移为独立资料的开发安装也保留其逐 VPS 快照。
	bindings, err := s.Ent.ExitBinding.Query().Where(exitbinding.KindEQ(exitbinding.KindSource)).All(ctx)
	if err != nil {
		return err
	}
	for _, b := range bindings {
		if len(b.Resolved) > 0 {
			continue
		}
		raw, ok, err := s.KV.Get(ctx, "source-exit:exit:"+strconv.Itoa(b.ExitID)+":gate:"+strconv.Itoa(b.GateID))
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if !json.Valid([]byte(raw)) {
			return fmt.Errorf("exit %d: invalid stored source snapshot", b.ExitID)
		}
		if err := s.Ent.ExitBinding.UpdateOneID(b.ID).SetResolved(json.RawMessage(raw)).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
