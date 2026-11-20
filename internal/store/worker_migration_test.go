package store

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 旧版本由 taskqueue 手写 DDL 建立的 worker_* 表与订阅缓存预占表，含后来用 ALTER 追加的列。
var legacyWorkerDDL = []string{
	`CREATE TABLE worker_capabilities (gate_id BIGINT PRIMARY KEY,hash TEXT NOT NULL,document TEXT NOT NULL)`,
	`CREATE TABLE worker_tasks (id TEXT PRIMARY KEY, kind TEXT NOT NULL, version INTEGER NOT NULL, trace_id TEXT NOT NULL, idem TEXT NOT NULL UNIQUE, payload_hash TEXT NOT NULL, body TEXT NOT NULL, requirements TEXT NOT NULL, candidates TEXT NOT NULL, deadline BIGINT NOT NULL, created_at BIGINT NOT NULL, status TEXT NOT NULL, gate_id BIGINT NOT NULL DEFAULT 0, attempt INTEGER NOT NULL DEFAULT 0, lease_token TEXT NOT NULL DEFAULT '', lease_until BIGINT NOT NULL DEFAULT 0, result TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', legacy_id BIGINT NOT NULL DEFAULT 0)`,
	`CREATE INDEX worker_tasks_dispatch ON worker_tasks(status, created_at)`,
	`CREATE INDEX worker_tasks_gate ON worker_tasks(gate_id, status)`,
	`CREATE INDEX worker_tasks_legacy ON worker_tasks(legacy_id)`,
	`CREATE TABLE worker_attempts (task_id TEXT NOT NULL, attempt INTEGER NOT NULL, gate_id BIGINT NOT NULL, started_at BIGINT NOT NULL, finished_at BIGINT NOT NULL DEFAULT 0, outcome TEXT NOT NULL, PRIMARY KEY(task_id, attempt))`,
	`CREATE INDEX worker_attempts_window ON worker_attempts(started_at,gate_id)`,
	`CREATE TABLE worker_activity (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, gate_id BIGINT NOT NULL, attempt INTEGER NOT NULL, at BIGINT NOT NULL, reason TEXT NOT NULL)`,
	`CREATE INDEX worker_activity_task ON worker_activity(task_id, at)`,
	`CREATE TABLE worker_communication (bucket BIGINT NOT NULL,gate_id BIGINT NOT NULL,route TEXT NOT NULL,requests BIGINT NOT NULL,received BIGINT NOT NULL,sent BIGINT NOT NULL,errors BIGINT NOT NULL,PRIMARY KEY(bucket,gate_id,route))`,
	`ALTER TABLE worker_tasks ADD COLUMN candidate_at BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE worker_tasks ADD COLUMN progress TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE worker_attempts ADD COLUMN result_bytes BIGINT`,
	`ALTER TABLE worker_tasks ADD COLUMN projection_after BIGINT NOT NULL DEFAULT 0`,
	`CREATE INDEX worker_tasks_projection ON worker_tasks(projection_after,id) WHERE status IN ('succeeded','failed','expired','unknown','cancelled') AND projection_after>=0 AND (legacy_id<>0 OR kind IN ('laya.prepare','laya.install','laya.remove'))`,
	`CREATE TABLE subscription_cache_entries (key TEXT PRIMARY KEY, bytes BIGINT NOT NULL, expires_at BIGINT NOT NULL)`,
	`CREATE INDEX subscription_cache_entries_expiry ON subscription_cache_entries(expires_at)`,
}

var legacyTables = []string{"worker_capabilities", "worker_tasks", "worker_attempts", "worker_activity", "worker_communication", "subscription_cache_entries"}

func TestLegacyWorkerTablesSurviveEntMigration(t *testing.T) {
	for name, open := range backends(t) {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			ctx := t.Context()
			for _, table := range legacyTables {
				if _, err := s.DB.ExecContext(ctx, "DROP TABLE "+table); err != nil {
					t.Fatal(err)
				}
			}
			for _, statement := range legacyWorkerDDL {
				if _, err := s.DB.ExecContext(ctx, statement); err != nil {
					t.Fatalf("%s: %v", statement, err)
				}
			}
			now := time.Now().Add(time.Hour).UnixMilli()
			fixtures := []struct {
				query string
				args  []any
			}{
				{`INSERT INTO worker_capabilities(gate_id,hash,document) VALUES($1,$2,$3)`, []any{7, "hash", `{"protocol":2}`}},
				{`INSERT INTO worker_tasks(id,kind,version,trace_id,idem,payload_hash,body,requirements,candidates,deadline,created_at,status,gate_id,attempt,lease_token,lease_until,result,error,legacy_id,candidate_at,progress,projection_after) VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9,$10,'succeeded',7,2,'lease',$9,'{"ok":true}','',42,$10,'',0)`,
					[]any{"task-1", "source.probe", "0123456789abcdef0123456789abcdef", "idem-1", "payload", "sealed", `{}`, `[7]`, now, now - 1000}},
				{`INSERT INTO worker_tasks(id,kind,version,trace_id,idem,payload_hash,body,requirements,candidates,deadline,created_at,status) VALUES($1,$2,1,'',$3,'p','b','{}','[7,8]',$4,$5,'pending')`,
					[]any{"task-2", "source.probe", "idem-2", now, now - 500}},
				{`INSERT INTO worker_attempts(task_id,attempt,gate_id,started_at,finished_at,outcome,result_bytes) VALUES('task-1',1,7,$1,$2,'failed',NULL)`, []any{now - 900, now - 800}},
				{`INSERT INTO worker_attempts(task_id,attempt,gate_id,started_at,finished_at,outcome,result_bytes) VALUES('task-1',2,7,$1,$2,'succeeded',12)`, []any{now - 700, now - 600}},
				{`INSERT INTO worker_activity(id,task_id,gate_id,attempt,at,reason) VALUES('activity-1','task-1',7,2,$1,'lease_claimed')`, []any{now - 700}},
				{`INSERT INTO worker_communication(bucket,gate_id,route,requests,received,sent,errors) VALUES($1,7,'/worker/poll',3,10,20,1)`, []any{now - 60000}},
				{`INSERT INTO subscription_cache_entries(key,bytes,expires_at) VALUES('cache-key',128,$1)`, []any{now}},
			}
			for _, f := range fixtures {
				if _, err := s.DB.ExecContext(ctx, f.query, f.args...); err != nil {
					t.Fatalf("%s: %v", f.query, err)
				}
			}
			beforeRows, beforeSchema := snapshotTables(t, s), describeTables(t, s)
			if err := s.migrate(ctx, ""); err != nil {
				t.Fatal(err)
			}
			if after := snapshotTables(t, s); !reflect.DeepEqual(beforeRows, after) {
				t.Fatalf("rows changed by migration:\nbefore=%v\nafter=%v", beforeRows, after)
			}
			if after := describeTables(t, s); !reflect.DeepEqual(beforeSchema, after) {
				t.Fatalf("schema changed by migration:\nbefore=%v\nafter=%v", beforeSchema, after)
			}
			r, err := s.Tasks.Get(ctx, "task-1")
			if err != nil || r.Status != "succeeded" || r.GateID != 7 || r.LegacyID != 42 || r.Task.Attempt != 2 || string(r.Result) != `{"ok":true}` || r.Ciphertext != "sealed" {
				t.Fatalf("legacy task=%+v err=%v", r, err)
			}
			activity, err := s.Tasks.Activity(ctx, "task-1")
			if err != nil || len(activity) != 1 || activity[0].Reason != "lease_claimed" {
				t.Fatalf("legacy activity=%+v err=%v", activity, err)
			}
			stats, err := s.Tasks.Statistics(ctx, time.UnixMilli(now-1000), time.UnixMilli(now))
			if err != nil || len(stats.Items) != 1 || stats.Items[0].Attempts != 2 || stats.Items[0].Measured != 1 || stats.Items[0].ResultBytes == nil || *stats.Items[0].ResultBytes != 12 {
				t.Fatalf("legacy statistics=%+v err=%v", stats, err)
			}
			traffic, err := s.Tasks.Communication(ctx, time.UnixMilli(now-120000))
			if err != nil || len(traffic) != 1 || traffic[0].Requests != 3 || traffic[0].Errors != 1 {
				t.Fatalf("legacy communication=%+v err=%v", traffic, err)
			}
		})
	}
}

// snapshotTables 读出旧表的全部行，按主键排序后转成文本，便于逐字段比较。
func snapshotTables(t *testing.T, s *Store) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, table := range legacyTables {
		rows, err := s.DB.QueryContext(context.Background(), "SELECT * FROM "+table+" ORDER BY 1, 2")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err = rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			fields := make([]string, len(columns))
			for i, v := range values {
				if b, ok := v.([]byte); ok {
					v = string(b)
				}
				fields[i] = fmt.Sprintf("%s=%v", columns[i], v)
			}
			out[table] = append(out[table], strings.Join(fields, ","))
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	return out
}

// describeTables 读出旧表的列定义与索引，迁移前后必须一致（SQLite 上表被重建时建表语句也会变）。
func describeTables(t *testing.T, s *Store) []string {
	t.Helper()
	query := `SELECT type || ':' || name || ':' || COALESCE(sql, '') FROM sqlite_master WHERE tbl_name IN ('worker_capabilities','worker_tasks','worker_attempts','worker_activity','worker_communication','subscription_cache_entries') ORDER BY type, name`
	if s.Driver == DriverPostgres {
		query = `SELECT 'column:' || table_name || '.' || column_name || ':' || data_type || ':' || is_nullable || ':' || COALESCE(column_default, '') FROM information_schema.columns WHERE table_schema = 'weir' AND table_name IN ('worker_capabilities','worker_tasks','worker_attempts','worker_activity','worker_communication','subscription_cache_entries')
UNION ALL SELECT 'index:' || indexname || ':' || indexdef FROM pg_indexes WHERE schemaname = 'weir' AND tablename IN ('worker_capabilities','worker_tasks','worker_attempts','worker_activity','worker_communication','subscription_cache_entries')
ORDER BY 1`
	}
	rows, err := s.DB.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		out = append(out, line)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
