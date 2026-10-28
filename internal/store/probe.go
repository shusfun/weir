package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// PostgresDatabase 是探测到的一个数据库：HasData 表示其中已有 Weir 账户，不能在上面重新初始化。
type PostgresDatabase struct {
	Name    string `json:"name"`
	HasData bool   `json:"has_data"`
	Unknown bool   `json:"unknown,omitempty"`
}

// ProbePostgres 用给定账号连上实例，读取版本并列出可连接的数据库及其中是否已有 Weir 账户。只读。
func ProbePostgres(ctx context.Context, p PostgresConfig) (string, []PostgresDatabase, error) {
	db, err := sql.Open("pgx", postgresDSN(p))
	if err != nil {
		return "", nil, err
	}
	defer db.Close()
	var version string
	if err = db.QueryRowContext(ctx, "SHOW server_version").Scan(&version); err != nil {
		return "", nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT datname FROM pg_database
		WHERE NOT datistemplate AND datallowconn AND has_database_privilege(datname, 'CONNECT')
		ORDER BY datname LIMIT 50`)
	if err != nil {
		return version, nil, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return version, nil, err
		}
		names = append(names, name)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return version, nil, err
	}
	out := make([]PostgresDatabase, 0, len(names))
	for _, name := range names {
		entry := PostgresDatabase{Name: name}
		q := p
		q.Database = name
		one, cancel := context.WithTimeout(ctx, 3*time.Second)
		if dbn, err := sql.Open("pgx", postgresDSN(q)); err != nil {
			entry.Unknown = true
		} else {
			entry.HasData, err = postgresHasAccounts(one, dbn)
			entry.Unknown = err != nil
			dbn.Close()
		}
		cancel()
		out = append(out, entry)
	}
	return version, out, nil
}

// Redis 探测结果。
const (
	RedisOK            = "ok"
	RedisAuthRequired  = "auth_required"
	RedisWrongPassword = "wrong_password"
	RedisUnreachable   = "unreachable"
)

// ProbeRedis 用给定账号 PING 一次，并在成功时读取服务器版本。
func ProbeRedis(ctx context.Context, c RedisConfig) (status, version string) {
	rdb := newRedisClient(c)
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		msg := err.Error()
		switch {
		case strings.HasPrefix(msg, "NOAUTH"):
			return RedisAuthRequired, ""
		case strings.HasPrefix(msg, "WRONGPASS"), strings.HasPrefix(msg, "ERR invalid password"), strings.Contains(msg, "AUTH"):
			return RedisWrongPassword, ""
		}
		return RedisUnreachable, ""
	}
	if info, err := rdb.Info(ctx, "server").Result(); err == nil {
		for _, line := range strings.Split(info, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "redis_version:"); ok {
				version = v
			}
		}
	}
	return RedisOK, version
}
