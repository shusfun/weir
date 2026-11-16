package center

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/geoip"
	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

const domainObserverKind = "gate-domain-observers"

func (s *Server) loadDomainObservers(ctx context.Context, db *ent.Client, gid int) (*ent.Resource, map[string]contracts.ConnectionObserver, error) {
	row, err := db.Resource.Query().Where(resource.KindEQ(domainObserverKind), resource.KeyEQ(itoa(gid))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, map[string]contracts.ConnectionObserver{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var data struct {
		Items map[string]contracts.ConnectionObserver `json:"items"`
	}
	if err = decodeResource(row, &data); err != nil {
		return nil, nil, err
	}
	for engine, item := range data.Items {
		if item.Secret, err = s.reveal(item.Secret); err != nil {
			return nil, nil, err
		}
		data.Items[engine] = item
	}
	return row, data.Items, nil
}

func (s *Server) saveDomainObservers(ctx context.Context, db *ent.Client, gid int, row *ent.Resource, items map[string]contracts.ConnectionObserver) error {
	sealed := map[string]contracts.ConnectionObserver{}
	for engine, item := range items {
		var err error
		if item.Secret, err = s.seal(item.Secret); err != nil {
			return err
		}
		sealed[engine] = item
	}
	data := toMap(struct {
		Items map[string]contracts.ConnectionObserver `json:"items"`
	}{sealed})
	if row != nil {
		return db.Resource.UpdateOneID(row.ID).SetData(data).Exec(ctx)
	}
	return db.Resource.Create().SetKind(domainObserverKind).SetKey(itoa(gid)).SetOwnerID(gid).SetData(data).Exec(ctx)
}

func (s *Server) compileDomainObservers(ctx context.Context, g *ent.Gate, cfg *contracts.GateConfig) (err error) {
	finish := operationStage(ctx, "traffic.domains.plan", "gate_id", g.ID)
	defer func() { finish(err) }()
	caps, err := s.Store.Tasks.Capabilities(ctx, g.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if caps.ConfigFeatures[contracts.ConfigDomainObservation] < 1 {
		return nil
	}
	wanted := []string{}
	for _, engine := range []string{"sing-box", "mihomo"} {
		if _, ok := cfg.Kernels[engine]; !ok {
			continue
		}
		d, err := kernelconf.DriverFor(*cfg, engine)
		if err != nil {
			return err
		}
		if d != nil && d.Has("native-connections") {
			wanted = append(wanted, engine)
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockActiveGates(ctx, tx.Client(), g.ID); err != nil {
		return err
	}
	current, err := tx.Gate.Get(ctx, g.ID)
	if err != nil {
		return err
	}
	row, saved, err := s.loadDomainObservers(ctx, tx.Client(), g.ID)
	if err != nil {
		return err
	}
	own, err := s.gateOwnPortsDB(ctx, tx.Client(), current)
	if err != nil {
		return err
	}
	foreign := foreignPorts(current, own)
	cfg.Observers = map[string]contracts.ConnectionObserver{}
	changed := false
	for _, engine := range wanted {
		item := saved[engine]
		if item.Port == 0 {
			if item.Port, err = pickPort(own, foreign); err != nil {
				return err
			}
			item.Secret = randomToken(32)
			saved[engine], own[item.Port], changed = item, true, true
		}
		cfg.Observers[engine] = item
	}
	if changed {
		if err = s.saveDomainObservers(ctx, tx.Client(), g.ID, row, saved); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Server) agentCountryNetworks(c *gin.Context) {
	ctx := c.Request.Context()
	finish := operationStage(ctx, "traffic.domains.country-data", "gate_id", gateFrom(c).ID)
	db, ok := s.geoDB().(interface {
		CountryNetworks(string) ([]netip.Prefix, geoip.Status, error)
	})
	if !ok {
		finish(fmt.Errorf("country data unavailable"))
		failure(c, 503, "country_data_unavailable")
		return
	}
	prefixes, status, err := db.CountryNetworks("CN")
	if err != nil || len(prefixes) == 0 || status.Fetched.IsZero() || time.Since(status.Fetched) > 45*24*time.Hour {
		finish(fmt.Errorf("country data missing or stale"))
		failure(c, 503, "country_data_unavailable")
		return
	}
	finish(nil)
	c.Header("Cache-Control", "private, max-age=21600")
	c.JSON(200, contracts.CountryNetworks{Built: status.Built, Fetched: status.Fetched, Prefixes: prefixes})
}
