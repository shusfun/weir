package center

import (
	"context"
	"errors"
	"testing"

	baseent "entgo.io/ent"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/subgrant"
)

func TestProfileGrantReplacementIsAtomic(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	a := s.Store.Ent.User.Create().SetEmail("grant-a@example.test").SetPasswordHash("fixture").SaveX(ctx)
	b := s.Store.Ent.User.Create().SetEmail("grant-b@example.test").SetPasswordHash("fixture").SaveX(ctx)
	p := s.Store.Ent.SubProfile.Create().SetName("atomic grants").SaveX(ctx)
	if err := s.replaceProfileGrants(ctx, p.ID, []int{a.ID}); err != nil {
		t.Fatal(err)
	}
	old := s.Store.Ent.SubGrant.Query().Where(subgrant.ProfileIDEQ(p.ID)).OnlyX(ctx)
	failCreate := true
	s.Store.Ent.SubGrant.Use(func(next baseent.Mutator) baseent.Mutator {
		return baseent.MutateFunc(func(ctx context.Context, mutation baseent.Mutation) (baseent.Value, error) {
			if m, ok := mutation.(*ent.SubGrantMutation); ok && m.Op().Is(baseent.OpCreate) && failCreate {
				return nil, errors.New("injected grant persistence failure")
			}
			return next.Mutate(ctx, mutation)
		})
	})
	if err := s.replaceProfileGrants(ctx, p.ID, []int{b.ID}); err == nil {
		t.Fatal("persistence failure hidden")
	}
	got := s.Store.Ent.SubGrant.Query().Where(subgrant.ProfileIDEQ(p.ID)).OnlyX(ctx)
	if got.UserID != a.ID || got.Token != old.Token {
		t.Fatal("failed replacement revoked the existing grant")
	}
	failCreate = false
	if err := s.replaceProfileGrants(ctx, p.ID, []int{a.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	if n := s.Store.Ent.SubGrant.Query().Where(subgrant.ProfileIDEQ(p.ID)).CountX(ctx); n != 2 {
		t.Fatalf("duplicate grant count=%d", n)
	}
	got = s.Store.Ent.SubGrant.Query().Where(subgrant.ProfileIDEQ(p.ID), subgrant.UserIDEQ(a.ID)).OnlyX(ctx)
	if got.Token != old.Token {
		t.Fatal("retained grant token rotated")
	}
}
