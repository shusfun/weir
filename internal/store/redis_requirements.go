package store

import (
	"context"
	"strings"
	"time"
)

// RedisRequirements is an observation, never an attempt to change the shared
// server. Restricted ACLs may prevent inspecting deployment policy.
type RedisRequirements struct {
	Status      string `json:"status"`
	Eviction    string `json:"eviction"`
	Persistence string `json:"persistence"`
	Reason      string `json:"reason,omitempty"`
}

func (s *Store) RedisRequirements(ctx context.Context) *RedisRequirements {
	if s.redis == nil {
		return nil
	}
	r := &RedisRequirements{Status: "unknown", Eviction: "unknown", Persistence: "unknown"}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	pipe := s.redis.Pipeline()
	policy := pipe.ConfigGet(ctx, "maxmemory-policy")
	aof := pipe.ConfigGet(ctx, "appendonly")
	rdb := pipe.ConfigGet(ctx, "save")
	_, err := pipe.Exec(ctx)
	if err != nil {
		r.Reason = "redis_policy_unobservable"
		return r
	}
	r.Eviction = policy.Val()["maxmemory-policy"]
	if r.Eviction == "" {
		r.Eviction = "unknown"
	}
	switch {
	case aof.Val()["appendonly"] == "yes":
		r.Persistence = "aof"
	case strings.TrimSpace(rdb.Val()["save"]) != "":
		r.Persistence = "rdb"
	case aof.Val()["appendonly"] == "no":
		r.Persistence = "disabled"
	}
	if r.Eviction == "unknown" || r.Persistence == "unknown" {
		return r
	}
	r.Status = "ready"
	if r.Eviction != "noeviction" || r.Persistence == "disabled" {
		r.Status, r.Reason = "needs_attention", "security_state_requires_noeviction_and_persistence"
	}
	return r
}
