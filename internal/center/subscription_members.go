package center

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/packages/contracts"
)

const maxSubscriptionMembers = 5000

func profileMembers(p *ent.SubProfile) []contracts.SubscriptionMember {
	if p.Members != nil {
		return p.Members
	}
	var out []contracts.SubscriptionMember
	for _, id := range p.NodeIds {
		out = append(out, contracts.SubscriptionMember{Kind: "node", ID: id})
	}
	for _, id := range p.SourceIds {
		out = append(out, contracts.SubscriptionMember{Kind: "source", ID: id})
	}
	return out
}

// Validate every root: changing a child can make a previously valid ancestor
// exceed the depth limit even when the edited child's own subtree is shallow.
func validateProfileGraph(profiles []*ent.SubProfile) error {
	rows := map[int]*ent.SubProfile{}
	for _, p := range profiles {
		rows[p.ID] = p
	}
	visiting, heights, expanded := map[int]bool{}, map[int]int{}, map[int]int{}
	var visit func(int) (int, error)
	visit = func(id int) (int, error) {
		if visiting[id] {
			return 0, errors.New("profile_cycle")
		}
		if h, ok := heights[id]; ok {
			return h, nil
		}
		p := rows[id]
		if p == nil {
			return 0, errors.New("unknown_profile")
		}
		if len(p.Members) > maxSubscriptionMembers || p.AllSources && p.Members != nil {
			return 0, errors.New("invalid_profile_members")
		}
		visiting[id] = true
		h := 0
		cost := len(profileMembers(p))
		if cost > maxSubscriptionMembers {
			return 0, errors.New("profile_expansion_exceeded")
		}
		seen := map[contracts.SubscriptionMember]bool{}
		for _, m := range p.Members {
			if m.ID <= 0 || seen[m] || m.Kind != "source" && m.Kind != "node" && m.Kind != "profile" {
				return 0, errors.New("invalid_profile_members")
			}
			seen[m] = true
			if m.Kind != "profile" {
				continue
			}
			child, err := visit(m.ID)
			if err != nil {
				return 0, err
			}
			if child+1 > h {
				h = child + 1
			}
			if h > 3 {
				return 0, errors.New("profile_depth_exceeded")
			}
			cost += expanded[m.ID]
			if cost > maxSubscriptionMembers {
				return 0, errors.New("profile_expansion_exceeded")
			}
		}
		visiting[id] = false
		heights[id] = h
		expanded[id] = cost
		return h, nil
	}
	for id := range rows {
		if _, err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func validateMemberResources(ctx context.Context, db *ent.Client, p *ent.SubProfile) error {
	for _, m := range p.Members {
		switch m.Kind {
		case "source":
			r, err := db.SubSource.Get(ctx, m.ID)
			if err != nil || !r.Enabled {
				return errors.New("unknown_source")
			}
		case "node":
			r, err := db.SubNode.Get(ctx, m.ID)
			if err != nil || !r.Enabled {
				return errors.New("unknown_node")
			}
		case "profile":
			r, err := db.SubProfile.Get(ctx, m.ID)
			if err != nil || !r.Enabled || r.ExpiresAt != nil && !time.Now().Before(*r.ExpiresAt) {
				return errors.New("profile_member_inactive")
			}
		default:
			return errors.New("invalid_profile_members")
		}
	}
	return nil
}

func (s *Server) checkProfileGraph(ctx context.Context, candidate *ent.SubProfile) error {
	rows, err := s.Store.Ent.SubProfile.Query().All(ctx)
	if err != nil {
		return err
	}
	found := false
	for i, p := range rows {
		if p.ID == candidate.ID {
			rows[i], found = candidate, true
		}
	}
	if !found {
		rows = append(rows, candidate)
	}
	if err := validateProfileGraph(rows); err != nil {
		return err
	}
	if err := validateMemberResources(ctx, s.Store.Ent, candidate); err != nil {
		return err
	}
	return nil
}

func (s *Server) profileParents(ctx context.Context, id int) ([]string, error) {
	rows, err := s.Store.Ent.SubProfile.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, p := range rows {
		for _, m := range p.Members {
			if m.Kind == "profile" && m.ID == id {
				names = append(names, fmt.Sprintf("%s (#%d)", p.Name, p.ID))
				break
			}
		}
	}
	return names, nil
}
