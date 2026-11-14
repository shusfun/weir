package center

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/subgroup"
	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

var errNoWorker = errors.New("no_capable_gate")

func (s *Server) workerCandidates(ctx context.Context, kind string, preferred int) ([]int, error) {
	return s.workerCandidatesFor(ctx, kind, contracts.TaskRequirements{PreferredGateID: preferred})
}

func (s *Server) workerCandidatesFor(ctx context.Context, kind string, requirements contracts.TaskRequirements) ([]int, error) {
	if err := requirements.Validate(); err != nil {
		return nil, err
	}
	if !contracts.KnownTask(kind) {
		return nil, errors.New("unknown_task_type")
	}
	gates, err := s.Store.Ent.Gate.Query().Where(gate.StatusEQ(gate.StatusActive)).All(ctx)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		id     int
		load   *float64
		active int
	}
	var ranked []candidate
	for _, g := range gates {
		if !gateOnline(g) || requirements.TargetGateID != 0 && requirements.TargetGateID != g.ID {
			continue
		}
		caps, e := s.Store.Tasks.Capabilities(ctx, g.ID)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if caps.Tasks[kind] == contracts.TaskVersion {
			var active int
			if e = s.Store.DB.QueryRowContext(ctx, `SELECT count(*) FROM worker_tasks WHERE gate_id=$1 AND status IN ('leased','cancelling')`, g.ID).Scan(&active); e != nil {
				return nil, e
			}
			resource, e := s.workerResources(ctx, g.ID)
			if e != nil {
				return nil, e
			}
			var load *float64
			var resources contracts.WorkerResources
			if resource != nil {
				load = resource.Resources.Load
				resources = resource.Resources
			}
			if !requirements.Matches(g.ID, kind, contracts.TaskVersion, caps, resources) {
				continue
			}
			ranked = append(ranked, candidate{g.ID, load, active})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].id == ranked[j].id {
			return false
		}
		if ranked[i].id == requirements.PreferredGateID {
			return true
		}
		if ranked[j].id == requirements.PreferredGateID {
			return false
		}
		if ranked[i].active != ranked[j].active {
			return ranked[i].active < ranked[j].active
		}
		if ranked[i].load != nil && ranked[j].load != nil && *ranked[i].load != *ranked[j].load {
			return *ranked[i].load < *ranked[j].load
		}
		if (ranked[i].load == nil) != (ranked[j].load == nil) {
			return ranked[i].load != nil
		}
		return ranked[i].id < ranked[j].id
	})
	var out []int
	for _, c := range ranked {
		out = append(out, c.id)
	}
	if len(out) == 0 {
		return nil, errNoWorker
	}
	return out, nil
}

func (s *Server) dispatchNetwork(ctx context.Context, kind string, payload any, preferred int) (contracts.SubscriptionFetchResult, int, error) {
	var out contracts.SubscriptionFetchResult
	candidates, err := s.workerCandidates(ctx, kind, preferred)
	if err != nil {
		return out, 0, err
	}
	body, gid, err := s.dispatchWorkerTask(ctx, kind, payload, contracts.TaskRequirements{PreferredGateID: preferred}, candidates)
	if err == nil {
		if json.Unmarshal(body, &out) != nil {
			err = errors.New("task_result_invalid")
		}
	}
	return out, gid, err
}

func (s *Server) dispatchWorkerTask(ctx context.Context, kind string, payload any, requirements contracts.TaskRequirements, candidates []int) (json.RawMessage, int, error) {
	var out json.RawMessage
	if requirements.Source == "" {
		requirements.Source = networkTaskSource(kind, payload)
	}
	eligible, err := s.workerCandidatesFor(ctx, kind, requirements)
	if err != nil {
		return out, 0, err
	}
	allowed := make(map[int]bool, len(eligible))
	for _, id := range eligible {
		allowed[id] = true
	}
	filtered := make([]int, 0, len(candidates))
	for _, id := range candidates {
		if allowed[id] {
			filtered = append(filtered, id)
			delete(allowed, id)
		}
	}
	if len(filtered) == 0 {
		return out, 0, errNoWorker
	}
	candidates = filtered
	raw, err := json.Marshal(payload)
	if err != nil {
		return out, 0, err
	}
	sealed, err := s.Crypto.Encrypt(string(raw))
	if err != nil {
		return out, 0, err
	}
	deadline := time.Now().Add(2 * time.Minute)
	if kind == contracts.TaskLayaClassify {
		if d, ok := ctx.Deadline(); ok {
			deadline = d
		}
	}
	if kind == contracts.TaskLayaInstall || kind == contracts.TaskLayaPrepare {
		deadline = time.Now().Add(30 * time.Minute)
	}
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	r, err := s.Store.Tasks.Enqueue(ctx, taskqueue.Request{Kind: kind, TraceID: taskTrace(ctx), Key: "worker:" + randomHex(16), Payload: raw, Requirements: requirements, Candidates: candidates, Deadline: deadline}, sealed, time.Now())
	if err != nil {
		return out, 0, err
	}
	_ = s.Store.Notifications.Publish(ctx, "workers")
	check := time.NewTicker(time.Second)
	defer check.Stop()
	wake, unsubscribe := s.Store.Notifications.Subscribe("task:" + r.Task.ID)
	defer unsubscribe()
	lastStage := ""
	leased := false
	timeoutError := func() error {
		if kind == contracts.TaskLayaClassify {
			code := "queue_timeout"
			if leased {
				code = "inference_timeout"
			}
			return &contracts.TaskError{Code: code, Origin: "center.dispatchWorkerTask"}
		}
		return context.DeadlineExceeded
	}
	for {
		current, e := s.Store.Tasks.Get(ctx, r.Task.ID)
		if e != nil {
			if ctx.Err() != nil {
				_ = s.Store.Tasks.Cancel(context.WithoutCancel(ctx), r.Task.ID)
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return out, 0, timeoutError()
				}
			}
			return out, 0, e
		}
		if kind == contracts.TaskLayaClassify {
			stage := "等待 VPS 领取 Laya 任务"
			if current.Status == "leased" {
				leased = true
				stage = "VPS 执行 Laya 推理"
				if current.Progress != nil && current.Progress.Stage != "" {
					stage = current.Progress.Stage
				}
			}
			if stage != lastStage {
				jobStep(ctx, stage, time.Until(deadline))
				lastStage = stage
			}
		}
		switch current.Status {
		case "succeeded":
			body, e := s.workerResult(current.Result)
			if e != nil {
				return out, current.GateID, e
			}
			return json.RawMessage(body), current.GateID, nil
		case "failed", "expired", "cancelled", "unknown":
			if current.Status == "expired" {
				return out, current.GateID, timeoutError()
			}
			if kind == contracts.TaskReleaseQuery && current.Error != nil {
				switch current.Error.Code {
				case "release_not_found":
					return out, current.GateID, ghrelease.ErrNotFound
				case "release_rate_limited":
					return out, current.GateID, ghrelease.ErrRateLimited
				}
			}
			if current.Error != nil {
				return out, current.GateID, current.Error
			}
			return out, current.GateID, errors.New("network_task_" + current.Status)
		}
		select {
		case <-ctx.Done():
			_ = s.Store.Tasks.Cancel(context.WithoutCancel(ctx), r.Task.ID)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return out, 0, timeoutError()
			}
			return out, 0, ctx.Err()
		case <-time.After(time.Until(deadline)):
			_ = s.Store.Tasks.Cancel(context.WithoutCancel(ctx), r.Task.ID)
			return out, 0, timeoutError()
		case <-wake:
		case <-check.C:
		}
	}
}

// Only registered origins can add a reachability constraint. URLs stay in the
// encrypted payload; scheduling records retain the category, never the URL.
func networkTaskSource(kind string, payload any) string {
	if kind == contracts.TaskReleaseQuery {
		return "github"
	}
	var raw string
	switch request := payload.(type) {
	case contracts.SubscriptionFetchTask:
		raw = request.Credentials.URL
	case contracts.ExternalConvertTask:
		raw = request.Credentials.URL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	for _, source := range []struct{ domain, name string }{{"github.com", "github"}, {"githubusercontent.com", "github"}, {"huggingface.co", "huggingface"}, {"hf-mirror.com", "hf_mirror"}, {"modelscope.cn", "modelscope"}} {
		if host == source.domain || strings.HasSuffix(host, "."+source.domain) {
			return source.name
		}
	}
	return ""
}

func (s *Server) fetchSubscription(ctx context.Context, raw, ua string) (subgroup.Response, error) {
	key := digest(raw + "\x00" + ua)
	started := time.Now().UTC()
	ch := s.sourceFetches.DoChan(key, func() (any, error) {
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		s.sourceFetchOnce.Do(func() { s.sourceFetchSlots = make(chan struct{}, s.Config.Subscriptions.effective().FetchConcurrency) })
		select {
		case s.sourceFetchSlots <- struct{}{}:
			defer func() { <-s.sourceFetchSlots }()
		case <-work.Done():
			return nil, work.Err()
		}
		return s.fetchSubscriptionOnce(work, raw, ua)
	})
	select {
	case <-ctx.Done():
		return subgroup.Response{}, ctx.Err()
	case result := <-ch:
		recordSubscriptionStage(ctx, 0, subgroup.Stage{Operation: "subscription.fetch_shared", Started: started, Finished: time.Now().UTC()}, "center.fetchSubscription")
		if result.Err != nil {
			return subgroup.Response{}, result.Err
		}
		response := result.Val.(subgroup.Response)
		response.Body = append([]byte(nil), response.Body...)
		return response, nil
	}
}

func (s *Server) fetchSubscriptionOnce(ctx context.Context, raw, ua string) (subgroup.Response, error) {
	result, gid, err := s.dispatchNetwork(ctx, contracts.TaskSubscriptionFetch, contracts.SubscriptionFetchTask{Credentials: contracts.FetchSecrets{URL: raw}, UserAgent: ua, MaxBytes: 10 << 20}, 0)
	if err == nil {
		return subgroup.Response{Body: result.Body, UserInfo: result.UserInfo, Title: result.Title, Location: "gate:" + itoa(gid)}, nil
	}
	if ctx.Err() != nil {
		return subgroup.Response{}, ctx.Err()
	}
	// The center remains the final fallback and explicitly reports its position.
	fallback, e := (subgroup.Fetcher{AllowPrivate: s.AllowPrivateSources}).Get(ctx, raw, ua)
	fallback.Location = "center-fallback"
	return fallback, e
}

func (s *Server) validateNetworkResult(ctx context.Context, r taskqueue.Record, raw json.RawMessage) error {
	var result contracts.SubscriptionFetchResult
	if err := contracts.DecodeStrict(raw, &result, contracts.MaxTaskResult); err != nil {
		return err
	}
	body, err := s.Crypto.Decrypt(r.Ciphertext)
	if err != nil {
		return err
	}
	var limit int64
	switch r.Task.Kind {
	case contracts.TaskSubscriptionFetch:
		var req contracts.SubscriptionFetchTask
		if err = json.Unmarshal([]byte(body), &req); err != nil {
			return err
		}
		limit = req.MaxBytes
	case contracts.TaskExternalConvert:
		var req contracts.ExternalConvertTask
		if err = json.Unmarshal([]byte(body), &req); err != nil {
			return err
		}
		limit = req.MaxBytes
	case contracts.TaskReleaseQuery:
		var req contracts.ReleaseQueryTask
		if err = json.Unmarshal([]byte(body), &req); err != nil {
			return err
		}
		limit = req.MaxBytes
		var releases contracts.ReleaseQueryResult
		if err = contracts.DecodeStrict(result.Body, &releases, int(limit)); err != nil {
			return err
		}
		if err = releases.Validate(req); err != nil {
			return err
		}
	default:
		return errors.New("network_task_type_invalid")
	}
	if limit < 1 || limit > 10<<20 || int64(len(result.Body)) > limit || len(result.ContentType) > 256 || len(result.UserInfo) > 4096 || len(result.Title) > 4096 {
		return errors.New("network_result_size_invalid")
	}
	sum := sha256.Sum256(result.Body)
	if hex.EncodeToString(sum[:]) != result.SHA256 {
		return errors.New("network_result_digest_invalid")
	}
	return nil
}

func (s *Server) queryReleases(ctx context.Context, repo, tag string, perPage int) ([]ghrelease.Release, error) {
	query := contracts.ReleaseQueryTask{Repository: repo, Tag: tag, PerPage: perPage, MaxBytes: 8 << 20}
	if err := query.Validate(); err != nil {
		return nil, err
	}
	result, _, err := s.dispatchNetwork(ctx, contracts.TaskReleaseQuery, query, 0)
	if err != nil {
		return nil, err
	}
	var response contracts.ReleaseQueryResult
	if err = contracts.DecodeStrict(result.Body, &response, 8<<20); err != nil {
		return nil, err
	}
	if err = response.Validate(query); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(response.Releases)
	var releases []ghrelease.Release
	if err = json.Unmarshal(raw, &releases); err != nil {
		return nil, err
	}
	if repo == "XTLS/Xray-core" || repo == "MetaCubeX/mihomo" || repo == "SagerNet/sing-box" || repo == "shusfun/weir" {
		for _, release := range releases {
			if repo == "shusfun/weir" && !strings.HasPrefix(release.Tag, "sing-box-v") {
				continue
			}
			_, observeErr := s.enqueueObservation(ctx, observationInput{Point: "kernel.release", Resource: repo, Identity: release.Tag, Text: release.Body, Origin: "center.queryReleases"})
			s.tasks.note("observation-producer", observeErr)
		}
	}
	return releases, nil
}
