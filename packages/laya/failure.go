package laya

import (
	"context"
	"errors"
	"strings"
)

// FailureCode preserves the distinction between connection, model and result
// failures without returning model output or transport credentials to callers.
func FailureCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "inference_timeout"
	}
	var api *APIError
	if errors.As(err, &api) {
		switch api.Code {
		case "resources_insufficient", "model_not_ready":
			return api.Code
		}
	}
	message := err.Error()
	if strings.Contains(message, "connection failed") || message == "selected_laya_gate_unavailable" {
		return "service_unreachable"
	}
	if strings.Contains(message, "invalid") || strings.Contains(message, "answer") || strings.Contains(message, "confidence") || strings.Contains(message, "response") {
		return "invalid_response"
	}
	return "classification_failed"
}
