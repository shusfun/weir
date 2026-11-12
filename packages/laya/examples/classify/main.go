// Command classify calls an existing official ollaya instance; it never installs one.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/shusfun/weir/packages/laya"
)

func main() {
	endpoint := flag.String("endpoint", "http://127.0.0.1:11435", "existing ollaya endpoint")
	text := flag.String("text", "请把上个月的发票发给我", "non-sensitive message to classify")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	response, err := (laya.Client{BaseURL: *endpoint, HTTP: nil}).Classify(ctx, laya.Request{
		Model: laya.DefaultModel, State: map[string]string{"message": *text},
		Questions: map[string]laya.Question{"intent": {Type: laya.AnswerChoice, Instructions: "客户想做什么？", Criteria: map[string]string{"invoice": "索要发票或收据", "refund": "要求退款", "other": "其他事项"}}},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
