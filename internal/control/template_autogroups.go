package control

import (
	"fmt"
	"strings"

	"github.com/imzyb/MiGate/internal/subscription"
	"github.com/imzyb/MiGate/services/subscriptions"
)

type templateAutoBucket struct {
	name  string
	nodes []int
}

// The explicit preset follows MiSub's region/protocol grouping intent, using
// the same pinned region metadata. Only the supplied authorized nodes enter
// buckets. Generated names cannot overwrite an administrator's existing group.
func templateAutoBuckets(links []string, t outputTemplate) ([]templateAutoBucket, error) {
	if t.AutoGroups == "" {
		return nil, nil
	}
	buckets := []templateAutoBucket{}
	byLabel := map[string]int{}
	used := map[string]bool{"DIRECT": true, "REJECT": true, "direct": true}
	for _, g := range t.Groups {
		used[g.Name] = true
	}
	for index, link := range links {
		node, err := subscription.ParseLink(link)
		if err != nil {
			return nil, err
		}
		label := strings.ToUpper(node.Protocol) + " 节点"
		if t.AutoGroups == "region" {
			label = subscriptions.RegionLabel(node.Remark, node.Address) + " 节点"
			for _, r := range t.regionOverrides {
				re, e := compileRegionOverride(r)
				if e != nil {
					return nil, e
				}
				if re.MatchString(node.Remark) {
					label = r.Region + " 节点"
					break
				}
			}
		}
		bucket, ok := byLabel[label]
		if !ok {
			bucket = len(buckets)
			byLabel[label] = bucket
			buckets = append(buckets, templateAutoBucket{name: uniqueNodeName(label, used)})
		}
		buckets[bucket].nodes = append(buckets[bucket].nodes, index)
	}
	if len(buckets)+len(t.Groups) > 64 {
		return nil, fmt.Errorf("automatic group limit exceeded")
	}
	return buckets, nil
}
