package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/shusfun/weir/packages/dsh"
	"github.com/shusfun/weir/packages/laya"
)

type drafts struct{}

func (drafts) SubmitDraft(context.Context, string, json.RawMessage) (dsh.ToolResult, error) {
	return dsh.ToolResult{Text: "proposal received; pending host approval"}, nil
}

func main() {
	endpoint := flag.String("laya", "http://127.0.0.1:11435", "existing official ollaya endpoint")
	node := flag.String("node", "", "installed native Node executable")
	bin := flag.String("dsh", "", "official installed dsh lib/bin.js")
	dir := flag.String("state", "", "isolated dsh state directory")
	patchFile := flag.String("patch", "", "host-validated restricted headless patch")
	version := flag.String("version", "", "installed dsh version")
	verified := flag.String("verified", "", "host-verified dsh version range")
	flag.Parse()
	if err := run(*endpoint, *node, *bin, *dir, *patchFile, *version, *verified); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(endpoint, node, bin, dir, patchFile, version, verified string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	answer, err := (laya.Client{BaseURL: endpoint}).Classify(ctx, laya.Request{Model: laya.DefaultModel, State: "请把上个月的发票发给我", Questions: map[string]laya.Question{"intent": {Type: laya.AnswerChoice, Instructions: "客户想做什么？", Criteria: map[string]string{"invoice": "索要发票", "refund": "要求退款", "other": "其他事项"}}}})
	if err != nil {
		return err
	}
	if err = json.NewEncoder(os.Stdout).Encode(answer); err != nil {
		return err
	}
	registry, err := dsh.NewRegistry(nil,
		dsh.Tool{Name: "status.read", Description: "Read status", InputSchema: map[string]any{"type": "object"},
			Sources: []string{"analysis"}, Handle: func(context.Context, string, json.RawMessage) (dsh.ToolResult, error) {
				return dsh.ToolResult{Text: "status"}, nil
			}},
		dsh.DraftTool("draft.submit", "Submit a draft", map[string]any{"type": "object"}, []string{"analysis"}, drafts{}),
	)
	if err != nil {
		return err
	}
	for _, name := range []string{"status.read", "draft.submit"} {
		result, err := registry.Call(ctx, "analysis", name, json.RawMessage(`{}`))
		if err != nil {
			return err
		}
		fmt.Println(result.Text)
	}
	if node == "" || bin == "" || dir == "" || patchFile == "" || version == "" || verified == "" {
		fmt.Println("background task: not executed (supply native runtime, isolated state and host-validated patch)")
		return nil
	}
	patch, err := os.ReadFile(patchFile)
	if err != nil {
		return err
	}
	job := dsh.Job{Paths: dsh.Paths{Node: node, Bin: bin, Dir: dir}, Namespace: "example", Patch: patch, Compatibility: dsh.Compatibility{InstalledVersion: version, VerifiedRange: verified, OutOfRange: dsh.DegradeReject}}
	result, err := job.Run(ctx, "Read host status using the configured read-only tool; submit any proposed change as a draft for host approval.")
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
