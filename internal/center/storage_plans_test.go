package center

import (
	"fmt"
	"os"
	"testing"

	"github.com/shusfun/weir/internal/store/ent"
)

func TestStorageQueryPlans(t *testing.T) {
	if os.Getenv("WEIR_STORAGE_SCALE") != "1" {
		t.Skip("opt-in storage execution plans")
	}
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	for first := 0; first < 10000; first += 100 {
		grants := make([]*ent.SubGrantCreate, 0, 100)
		messages := make([]*ent.ResourceCreate, 0, 100)
		for i := first; i < first+100; i++ {
			grants = append(grants, s.Store.Ent.SubGrant.Create().SetProfileID(i/10+1).SetUserID(i+1).SetToken(fmt.Sprintf("isolated-%d", i)))
			messages = append(messages, s.Store.Ent.Resource.Create().SetKind(kindGuestbook).SetOwnerID(i/100+1).SetKey(fmt.Sprint(i)).SetData(map[string]any{"status": "approved", "hidden": false}))
		}
		if err := s.Store.Ent.SubGrant.CreateBulk(grants...).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s.Store.Ent.Resource.CreateBulk(messages...).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	prefix := "EXPLAIN QUERY PLAN "
	if s.Store.Driver == "postgres" {
		prefix = "EXPLAIN (ANALYZE, BUFFERS) "
		if _, err := s.Store.DB.ExecContext(ctx, "ANALYZE sub_grants, resources"); err != nil {
			t.Fatal(err)
		}
	} else if _, err := s.Store.DB.ExecContext(ctx, "ANALYZE"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"SELECT profile_id,user_id FROM sub_grants WHERE profile_id IN (1,100,500,1000)",
		"SELECT id,data FROM resources WHERE kind='guestbook' AND owner_id=50 ORDER BY id DESC LIMIT 20 OFFSET 20",
		"SELECT count(*) FROM resources WHERE kind='guestbook' AND owner_id=50",
	} {
		rows, err := s.Store.DB.QueryContext(ctx, prefix+q)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		t.Logf("PLAN driver=%s query=%s", s.Store.Driver, q)
		for rows.Next() {
			values := make([]any, len(columns))
			targets := make([]any, len(columns))
			for i := range values {
				targets[i] = &values[i]
			}
			if err := rows.Scan(targets...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			t.Logf("%v", values)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
