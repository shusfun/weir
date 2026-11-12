// Command tools demonstrates a host-owned read capability and draft receiver.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/shusfun/weir/packages/dsh"
)

type drafts struct{}

func (drafts) SubmitDraft(_ context.Context, _ string, raw json.RawMessage) (dsh.ToolResult, error) {
	// The host would validate, preview and persist this proposal for approval.
	return dsh.ToolResult{Text: "proposal pending host approval"}, nil
}
func main() {
	registry, err := dsh.NewRegistry(nil,
		dsh.Tool{Name: "status.read", Description: "Read host status", InputSchema: map[string]any{"type": "object", "additionalProperties": false}, Sources: []string{"analysis"}, Handle: func(context.Context, string, json.RawMessage) (dsh.ToolResult, error) {
			return dsh.ToolResult{Text: "status available"}, nil
		}},
		dsh.DraftTool("draft.submit", "Submit a proposal", map[string]any{"type": "object"}, []string{"analysis"}, drafts{}),
	)
	if err == nil {
		for _, name := range []string{"status.read", "draft.submit"} {
			var result dsh.ToolResult
			result, err = registry.Call(context.Background(), "analysis", name, json.RawMessage(`{}`))
			if err != nil {
				break
			}
			fmt.Println(result.Text)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
