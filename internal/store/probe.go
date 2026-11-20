package store

import (
	"context"
	"strings"
)

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
