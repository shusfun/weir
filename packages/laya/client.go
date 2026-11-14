package laya

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"
)

type AnswerType string

const (
	AnswerChoice AnswerType = "choice"
	AnswerScore  AnswerType = "score"
	AnswerNoul   AnswerType = "noul"
)

type Question struct {
	Type         AnswerType `json:"type"`
	Instructions any        `json:"instructions,omitempty"`
	Criteria     any        `json:"criteria,omitempty"`
	Ordinal      *bool      `json:"ordinal,omitempty"`
}

type Request struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

type Answer struct {
	Type          AnswerType         `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

type Response struct {
	Answers map[string]Answer `json:"answers"`
}

type Event struct {
	Operation, TraceID, Status, Code string
	Started, Finished                time.Time
}
type Observer func(context.Context, Event)

type Client struct {
	BaseURL  string
	APIKey   string
	HTTP     *http.Client
	Observer Observer
}

// Warmup uses the deployment budget, independently of interactive request limits.
// It retains the same transport policy and result validation as real inference.
func (c Client) Warmup(ctx context.Context, input Request, timeout time.Duration) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if c.HTTP == nil {
		c.HTTP = runtimeHTTP(timeout)
	} else {
		h := *c.HTTP
		h.Timeout = timeout
		c.HTTP = &h
	}
	return c.Classify(ctx, input)
}

func (c Client) Classify(ctx context.Context, input Request) (response Response, err error) {
	started := time.Now().UTC()
	defer func() {
		if c.Observer == nil {
			return
		}
		status, code := "succeeded", ""
		if err != nil {
			status, code = "failed", FailureCode(err)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			status, code = "cancelled", "context_cancelled"
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status, code = "timeout", "deadline_exceeded"
		}
		c.Observer(ctx, Event{Operation: "laya.decision", TraceID: traceID(ctx), Started: started, Finished: time.Now().UTC(), Status: status, Code: code})
	}()
	if strings.TrimSpace(input.Model) == "" || len(input.Questions) == 0 {
		return response, errors.New("model and questions are required")
	}
	base, err := c.endpoint("/v1/systemone")
	if err != nil {
		return response, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return response, errors.New("invalid decision request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base, bytes.NewReader(body))
	if err != nil {
		return response, errors.New("invalid laya endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = runtimeHTTP(10 * time.Second)
	}
	bounded := *httpClient
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := bounded.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return response, ctx.Err()
		}
		return response, errors.New("laya connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&failure)
		switch strings.ToLower(failure.Code) {
		case "resources_insufficient", "out_of_memory", "insufficient_memory":
			return response, &APIError{Code: "resources_insufficient", HTTPStatus: resp.StatusCode}
		case "model_not_ready", "model_not_found", "operation_in_progress":
			return response, &APIError{Code: "model_not_ready", HTTPStatus: resp.StatusCode}
		}
		return response, fmt.Errorf("laya returned HTTP %d", resp.StatusCode)
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (256<<10)+1))
	if readErr != nil || len(raw) > 256<<10 {
		return response, errors.New("laya response too large or interrupted")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err = dec.Decode(&response); err != nil {
		return response, errors.New("invalid laya response")
	}
	if dec.Decode(new(any)) != io.EOF {
		return response, errors.New("invalid trailing laya response")
	}
	return response, ValidateResponse(input, response)
}

// ValidateResponse checks typed answers against the original request after a
// host transports a response through its own worker protocol.
func ValidateResponse(input Request, response Response) error {
	if len(input.Questions) == 0 || len(response.Answers) != len(input.Questions) {
		return errors.New("answer set does not match request")
	}
	for key, q := range input.Questions {
		a, ok := response.Answers[key]
		if !ok {
			return fmt.Errorf("missing answer %q", key)
		}
		if a.Type != "" && a.Type != q.Type {
			return fmt.Errorf("answer type mismatch %q", key)
		}
		switch q.Type {
		case AnswerChoice:
			if a.Choice == "" {
				return fmt.Errorf("invalid choice answer %q", key)
			}
			encoded, _ := json.Marshal(q.Criteria)
			var labels map[string]json.RawMessage
			if json.Unmarshal(encoded, &labels) == nil && len(labels) > 0 {
				if _, valid := labels[a.Choice]; !valid {
					return fmt.Errorf("choice outside criteria %q", key)
				}
			} else {
				var choices []string
				if json.Unmarshal(encoded, &choices) != nil || !slices.Contains(choices, a.Choice) {
					return fmt.Errorf("invalid choice criteria %q", key)
				}
			}
		case AnswerScore:
			if a.Score == nil || math.IsNaN(*a.Score) || math.IsInf(*a.Score, 0) || *a.Score < 0 {
				return fmt.Errorf("invalid score answer %q", key)
			}
		case AnswerNoul:
			if a.Noul == nil || math.IsNaN(*a.Noul) || math.IsInf(*a.Noul, 0) || *a.Noul < 0 || *a.Noul > 1 {
				return fmt.Errorf("invalid noul answer %q", key)
			}
		default:
			return fmt.Errorf("unsupported answer type %q", q.Type)
		}

		if a.Confidence != nil && (math.IsNaN(*a.Confidence) || math.IsInf(*a.Confidence, 0) || *a.Confidence < 0 || *a.Confidence > 1) {
			return fmt.Errorf("invalid confidence %q", key)
		}
		for _, probability := range a.Probabilities {
			if math.IsNaN(probability) || math.IsInf(probability, 0) || probability < 0 || probability > 1 {
				return fmt.Errorf("invalid probability %q", key)
			}
		}
	}
	return nil
}
