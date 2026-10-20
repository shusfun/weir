package control

import (
	"encoding/json"
	"math/big"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/internal/subscription"
)

type sourceUsageSummary struct {
	Status         string  `json:"status"`
	Source         string  `json:"source"`
	ObservedAt     string  `json:"observed_at"`
	Included       int     `json:"included"`
	Excluded       int     `json:"excluded"`
	Disabled       int     `json:"disabled"`
	UnknownSources int     `json:"unknown_sources"`
	Upload         *string `json:"upload"`
	Download       *string `json:"download"`
	Total          *string `json:"total"`
	Remaining      *string `json:"remaining"`
	Expire         *string `json:"expire"`
}

// Decimal strings and arbitrary precision sums preserve counters beyond JS and
// int64 addition. A missing/stale observation poisons its aggregate field.
func mergedSourceUsage(items []*ent.Resource, now time.Time) sourceUsageSummary {
	r := sourceUsageSummary{Status: "empty", Source: "upstream_subscription_userinfo", ObservedAt: now.UTC().Format(time.RFC3339)}
	sums := []*big.Int{new(big.Int), new(big.Int), new(big.Int), new(big.Int)}
	valid := []bool{true, true, true, true}
	expiryKnown := true
	expiry := int64(0)
	active := 0
	seen := map[int]bool{}
	for _, item := range items {
		if seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		d := item.Data
		if d["enabled"] == false {
			r.Disabled++
			continue
		}
		active++
		var u subscription.UsageMetadata
		raw, _ := json.Marshal(d["upstream_usage"])
		err := json.Unmarshal(raw, &u)
		updated, e := time.Parse(time.RFC3339, text(d["last_update"]))
		fresh := err == nil && e == nil && text(d["last_error"]) == "" && u.Status == "available" && !updated.After(now.Add(time.Minute)) && now.Sub(updated) <= sourceInterval(d)
		if !fresh || u.Expire == nil {
			expiryKnown = false
		} else if n, e := strconv.ParseInt(*u.Expire, 10, 64); e != nil || n < 0 {
			expiryKnown = false
		} else if n > 0 && (expiry == 0 || n < expiry) {
			expiry = n
		}
		if d["exclude_traffic"] == true {
			r.Excluded++
			continue
		}
		r.Included++
		values := []*string{u.Upload, u.Download, u.Total}
		numbers := make([]*big.Int, 3)
		known := true
		for i, value := range values {
			if fresh && value != nil {
				n, ok := new(big.Int).SetString(*value, 10)
				if ok && n.Sign() >= 0 {
					numbers[i] = n
					sums[i].Add(sums[i], n)
					continue
				}
			}
			valid[i] = false
			known = false
		}
		if known {
			remaining := new(big.Int).Sub(numbers[2], numbers[0])
			remaining.Sub(remaining, numbers[1])
			if remaining.Sign() < 0 {
				remaining.SetInt64(0)
			}
			sums[3].Add(sums[3], remaining)
		} else {
			valid[3] = false
			r.UnknownSources++
		}
	}
	fields := []**string{&r.Upload, &r.Download, &r.Total, &r.Remaining}
	if r.Included > 0 {
		for i, target := range fields {
			if valid[i] {
				s := sums[i].String()
				*target = &s
			}
		}
	}
	if active > 0 && expiryKnown {
		s := strconv.FormatInt(expiry, 10)
		r.Expire = &s
	}
	if r.Included > 0 {
		r.Status = "available"
		if r.UnknownSources > 0 {
			r.Status = "partial"
			if r.UnknownSources == r.Included {
				r.Status = "unknown"
			}
		}
	}
	return r
}

func (s *Server) sourceUsage(c *gin.Context) {
	ctx := c.Request.Context()
	items := []*ent.Resource{}
	after := 0
	for {
		batch, err := s.Store.Resource.Query().Where(resource.KindEQ("sources"), resource.IDGT(after)).Order(ent.Asc("id")).Limit(500).All(ctx)
		if err != nil {
			failure(c, 503, "query_failed")
			return
		}
		if len(batch) == 0 {
			break
		}
		items = append(items, batch...)
		if len(items) > 50000 {
			failure(c, 422, "source_summary_limit")
			return
		}
		after = batch[len(batch)-1].ID
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, mergedSourceUsage(items, time.Now()))
}
