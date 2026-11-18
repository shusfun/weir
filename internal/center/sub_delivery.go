package center

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// Counts remain unknown until rendering completes; failures must not look like empty deliveries.
type subscriptionDelivery struct {
	c                         *gin.Context
	started                   time.Time
	linkID, revision, members int
	provider, stage           string
}

func beginSubscriptionDelivery(c *gin.Context, linkID, revision int, provider string) *subscriptionDelivery {
	d := &subscriptionDelivery{c: c, started: time.Now(), linkID: linkID, members: -1, provider: provider, stage: "authorize"}
	d.setRevision(revision)
	return d
}

func (d *subscriptionDelivery) setRevision(revision int) {
	d.revision = revision
	d.c.Header("X-Weir-Policy-Revision", strconv.Itoa(revision))
}

func (d *subscriptionDelivery) finish() {
	result := "failed"
	if d.c.Writer.Status() == 200 && d.stage == "delivered" {
		result = "ok"
	}
	slog.InfoContext(d.c.Request.Context(), "subscription delivery",
		"trace", taskTrace(d.c.Request.Context()), "operation", "subscription.deliver", "link_id", d.linkID,
		"policy_revision", d.revision, "provider", d.provider, "member_count", d.members,
		"duration_ms", time.Since(d.started).Milliseconds(), "stage", d.stage,
		"status", d.c.Writer.Status(), "result", result,
		"code.function.name", "subscriptionDelivery.finish", "code.file.path", "internal/center/sub_delivery.go")
}
