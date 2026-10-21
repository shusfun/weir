package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/user"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

// AdminAccount 是菜单里可见的管理员。不带密码哈希、订阅令牌或授权。
type AdminAccount struct {
	ID      int
	Email   string
	Enabled bool
}

// AdminChangeResult 记录账号行已经提交后的附带动作。账号修改不因审计或会话失败回滚。
type AdminChangeResult struct {
	AuditWritten    bool
	SessionsRevoked bool
}

func (r AdminChangeResult) Summary(done string) string {
	text := "[成功] " + done
	if !r.SessionsRevoked {
		text += "。会话撤销失败"
	}
	if !r.AuditWritten {
		text += "。审计未写入"
	}
	return text
}

func ListAdmins(ctx context.Context, cfg Config) ([]AdminAccount, error) {
	db, store, err := openControlDB(cfg)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	defer store.Close()
	rows, err := store.User.Query().Where(user.RoleEQ("admin")).Order(user.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AdminAccount, 0, len(rows))
	for _, row := range rows {
		out = append(out, AdminAccount{ID: row.ID, Email: row.Email, Enabled: row.Enabled})
	}
	return out, nil
}

// ForceAdminEmail 只改这一行的登录邮箱，不要求旧密码，也不动授权和订阅令牌。
func ForceAdminEmail(ctx context.Context, cfg Config, id int, email string) (AdminChangeResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !validEmail(email) {
		return AdminChangeResult{}, errors.New("登录邮箱格式无效")
	}
	return forceAdminField(ctx, cfg, id, func(ctx context.Context, store *ent.Client) error {
		taken, err := store.User.Query().Where(user.EmailEqualFold(email), user.IDNEQ(id)).Exist(ctx)
		if err != nil {
			return err
		}
		if taken {
			return errors.New("登录邮箱已被占用")
		}
		n, err := store.User.Update().Where(user.IDEQ(id), user.RoleEQ("admin")).SetEmail(email).Save(ctx)
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("管理员不存在")
		}
		return nil
	})
}

// ForceAdminPassword 只替换密码哈希。调用方不得把明文密码写入结果或审计。
func ForceAdminPassword(ctx context.Context, cfg Config, id int, password string) (AdminChangeResult, error) {
	if !validAccountPassword(password) {
		return AdminChangeResult{}, accountPasswordError()
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return AdminChangeResult{}, errors.New("密码保存失败")
	}
	return forceAdminField(ctx, cfg, id, func(ctx context.Context, store *ent.Client) error {
		n, err := store.User.Update().Where(user.IDEQ(id), user.RoleEQ("admin")).SetPasswordHash(string(hash)).Save(ctx)
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("管理员不存在")
		}
		return nil
	})
}

func forceAdminField(ctx context.Context, cfg Config, id int, update func(context.Context, *ent.Client) error) (AdminChangeResult, error) {
	db, store, err := openControlDB(cfg)
	if err != nil {
		return AdminChangeResult{}, err
	}
	defer db.Close()
	defer store.Close()
	if err = update(ctx, store); err != nil {
		return AdminChangeResult{}, err
	}
	var result AdminChangeResult
	rdb, redisErr := openControlRedis(cfg)
	if redisErr == nil {
		defer rdb.Close()
		result.SessionsRevoked = revokeUserSessions(ctx, rdb, id) == nil
	}
	if _, auditErr := store.Audit.Create().SetActor(id).SetAction("admin.credentials.force").SetTarget("user:" + strconv.Itoa(id)).SetResult("ok").SetIP("cli").Save(ctx); auditErr == nil {
		result.AuditWritten = true
	}
	return result, nil
}

func openControlDB(cfg Config) (*sql.DB, *ent.Client, error) {
	if !cfg.Installed {
		return nil, nil, errors.New("尚未完成初始化")
	}
	db, store, err := openDatabase(cfg.Database)
	if err != nil {
		return nil, nil, err
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err = db.PingContext(pingCtx); err != nil {
		db.Close()
		_ = store.Close()
		return nil, nil, errors.New("PostgreSQL connection failed")
	}
	return db, store, nil
}

func openControlRedis(cfg Config) (*redis.Client, error) {
	rdb := redisClient(cfg.Redis)
	pingCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, errors.New("Redis connection failed")
	}
	return rdb, nil
}

// revokeUserSessions 只删除该用户的会话。其他用户的键保持不动。
func revokeUserSessions(ctx context.Context, rdb *redis.Client, id int) error {
	if rdb == nil {
		return errors.New("redis unavailable")
	}
	var cursor uint64
	for {
		keys, next, err := rdb.Scan(ctx, cursor, "migate:session:*", 100).Result()
		if err != nil {
			return err
		}
		for _, key := range keys {
			body, err := rdb.Get(ctx, key).Bytes()
			if err != nil {
				continue
			}
			var session Session
			if json.Unmarshal(body, &session) != nil || session.UserID != id {
				continue
			}
			if err = rdb.Del(ctx, key).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
