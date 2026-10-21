package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/imzyb/MiGate/ent/grant"
	"github.com/imzyb/MiGate/ent/user"
	"golang.org/x/crypto/bcrypt"
)

func TestForceAdminKeepsGrantAndSubscriptionToken(t *testing.T) {
	resetInstallDatabase(t)
	ctx := context.Background()
	cfg := Config{
		Installed:     true,
		Listen:        "127.0.0.1:19810",
		EncryptionKey: strings.Repeat("ab", 32),
		Database:      DatabaseConfig{Host: "127.0.0.1", Port: 25432, User: "gate", Password: "local-test-only", Database: "gate", SSLMode: "disable"},
		Redis:         RedisConfig{Address: "127.0.0.1:26379", Username: "migate", Password: "local-test-only"},
	}
	db, store, err := openControlDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer store.Close()
	if _, err = db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS migate"); err != nil {
		t.Fatal(err)
	}
	if err = store.Schema.Create(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := store.User.Create().SetEmail("admin@example.test").SetPasswordHash("old-hash").SetRole("admin").SetEnabled(true).SetQuota(42).SetUsed(7).SetExpires(99).SetSubscriptionToken("keep-subscription-token").SetTotpSecret("keep-totp").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.User.Create().SetEmail("other@example.test").SetPasswordHash("other-hash").SetRole("user").SetSubscriptionToken("other-subscription-token").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := store.Grant.Create().SetUserID(admin.ID).SetDeviceID(1001).SetInboundID(7).SetCredential(map[string]any{"uuid": "keep-grant"}).SetStatus("applied").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rdb, err := openControlRedis(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Close()
	adminKey := "migate:session:force-admin-test"
	otherKey := "migate:session:force-other-test"
	adminBody, _ := json.Marshal(Session{UserID: admin.ID, CreatedAt: time.Now()})
	otherBody, _ := json.Marshal(Session{UserID: other.ID, CreatedAt: time.Now()})
	if err = rdb.Set(ctx, adminKey, adminBody, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	if err = rdb.Set(ctx, otherKey, otherBody, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rdb.Del(context.Background(), adminKey, otherKey) })

	if _, err = ForceAdminEmail(ctx, cfg, admin.ID, "other@example.test"); err == nil || err.Error() != "登录邮箱已被占用" {
		t.Fatalf("collision = %v", err)
	}
	emailResult, err := ForceAdminEmail(ctx, cfg, admin.ID, "renamed@example.test")
	if err != nil || !emailResult.AuditWritten || !emailResult.SessionsRevoked {
		t.Fatalf("email result=%+v err=%v", emailResult, err)
	}
	passwordResult, err := ForceAdminPassword(ctx, cfg, admin.ID, "replacement-password")
	if err != nil || !passwordResult.AuditWritten || !passwordResult.SessionsRevoked {
		t.Fatalf("password result=%+v err=%v", passwordResult, err)
	}
	updated, err := store.User.Get(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Email != "renamed@example.test" || updated.SubscriptionToken != "keep-subscription-token" || updated.Quota != 42 || updated.Used != 7 || updated.Expires != 99 || updated.TotpSecret != "keep-totp" || updated.Role != "admin" || !updated.Enabled {
		t.Fatalf("account mutated: %+v", updated)
	}
	if bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("replacement-password")) != nil {
		t.Fatal("new password does not match")
	}
	still, err := store.Grant.Query().Where(grant.UserIDEQ(admin.ID)).Only(ctx)
	if err != nil || still.ID != kept.ID || still.Credential["uuid"] != "keep-grant" || still.Status != "applied" {
		t.Fatalf("grant = %+v err=%v", still, err)
	}
	if n, err := store.User.Query().Where(user.IDEQ(other.ID), user.EmailEQ("other@example.test")).Count(ctx); err != nil || n != 1 {
		t.Fatal("other account changed")
	}
	if n, err := rdb.Exists(ctx, adminKey).Result(); err != nil || n != 0 {
		t.Fatalf("admin session remains: %d %v", n, err)
	}
	if n, err := rdb.Exists(ctx, otherKey).Result(); err != nil || n != 1 {
		t.Fatalf("other session = %d err=%v", n, err)
	}
	audits, err := store.Audit.Query().All(ctx)
	if err != nil || len(audits) < 2 {
		t.Fatalf("audits=%v err=%v", audits, err)
	}
	for _, item := range audits {
		if strings.Contains(item.Action+item.Target+item.Result, "replacement-password") {
			t.Fatal("audit contains the password")
		}
	}
}
