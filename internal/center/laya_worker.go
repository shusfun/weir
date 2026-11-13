package center

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

func (s *Server) validateLayaDeploymentResult(ctx context.Context, record taskqueue.Record, raw json.RawMessage) error {
	var result contracts.LayaDeploymentResult
	if err := contracts.DecodeStrict(raw, &result, 4096); err != nil {
		return err
	}
	image, err := hex.DecodeString(strings.TrimPrefix(result.ImageDigest, "sha256:"))
	if err != nil || len(image) != 32 || !strings.HasPrefix(result.ImageDigest, "sha256:") || result.Version == "" || len(result.Version) > 100 || result.Model != laya.DefaultModel || result.ModelDigest != laya.ModelManifestSHA256 {
		return errors.New("invalid laya deployment metadata")
	}
	if record.Task.Kind == contracts.TaskLayaPrepare {
		if result.State != "prepared" || result.Version != laya.OllayaVersion {
			return errors.New("invalid laya preparation state")
		}
		return nil
	}
	if record.Task.Kind == contracts.TaskLayaRemove {
		if result.State != "removed" {
			return errors.New("invalid laya removal state")
		}
		return nil
	}
	body, err := s.Crypto.Decrypt(record.Ciphertext)
	if err != nil {
		return err
	}
	var request contracts.LayaInstallTask
	if err = contracts.DecodeStrict([]byte(body), &request, contracts.MaxTaskPayload); err != nil {
		return err
	}
	var manifest struct {
		ImageDigest string `json:"image_digest"`
	}
	if contracts.DecodeStrict(request.Manifest, &manifest, 4096) != nil || result.State != "installed" || result.Version != request.DeploymentVersion || result.ImageDigest != manifest.ImageDigest {
		return errors.New("laya deployment does not match approved request")
	}
	return nil
}

func layaTaskRequest(request laya.Request) (contracts.LayaClassifyTask, error) {
	b, err := json.Marshal(request)
	var req contracts.LayaClassifyTask
	if err == nil {
		err = contracts.DecodeStrict(b, &req, contracts.MaxTaskPayload)
	}
	return req, err
}

func (s *Server) classifyOnGate(ctx context.Context, gateID int, request laya.Request, runtimeVersion string) (laya.Response, error) {
	var response laya.Response
	candidates, err := s.workerCandidates(ctx, contracts.TaskLayaClassify, gateID)
	if err != nil {
		return response, err
	}
	found := false
	for _, id := range candidates {
		if id == gateID {
			found = true
		}
	}
	if !found {
		return response, errors.New("selected_laya_gate_unavailable")
	}
	req, err := layaTaskRequest(request)
	if err != nil {
		return response, err
	}
	req.ExpectedRuntimeVersion = runtimeVersion
	raw, _, err := s.dispatchWorkerTask(ctx, contracts.TaskLayaClassify, req, contracts.TaskRequirements{TargetGateID: gateID}, []int{gateID})
	if err != nil {
		return response, err
	}
	if err = contracts.DecodeStrict(raw, &response, 256<<10); err != nil {
		return response, errors.New("invalid_laya_worker_result")
	}
	return response, laya.ValidateResponse(request, response)
}

func (s *Server) validateLayaResult(ctx context.Context, record taskqueue.Record, raw json.RawMessage) error {
	var response laya.Response
	if err := contracts.DecodeStrict(raw, &response, 256<<10); err != nil {
		return err
	}
	body, err := s.Crypto.Decrypt(record.Ciphertext)
	if err != nil {
		return err
	}
	var wire contracts.LayaClassifyTask
	if err = contracts.DecodeStrict([]byte(body), &wire, contracts.MaxTaskPayload); err != nil {
		return err
	}
	// The worker checks the expected runtime before and after inference; the
	// independent module's typed answer is validated against the same question.
	request := laya.Request{Model: wire.Model, State: wire.State, Questions: map[string]laya.Question{}}
	for name, q := range wire.Questions {
		request.Questions[name] = laya.Question{Type: laya.AnswerType(q.Type), Instructions: q.Instructions, Criteria: q.Criteria, Ordinal: q.Ordinal}
	}
	return laya.ValidateResponse(request, response)
}
