package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/lox/deep-analysis/internal/agent"
)

type anthropicTestFileOps struct{}

func (anthropicTestFileOps) ReadFile(context.Context, string) (string, error) {
	return "package example\n", nil
}
func (anthropicTestFileOps) GrepFiles(context.Context, string, string, bool) (string, error) {
	return "", nil
}
func (anthropicTestFileOps) GlobFiles(context.Context, string) (string, error) { return "", nil }
func (anthropicTestFileOps) ListFiles(context.Context, string) (string, error) { return "", nil }

func TestAnthropicAnalyzeRunsToolLoop(t *testing.T) {
	var mu sync.Mutex
	var requests []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		requests = append(requests, request)
		requestNumber := len(requests)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if requestNumber == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w,
				sseEvent("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-fable-5","content":[],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`)+
					sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"read_files","input":{"paths":["example.go"]}}}`)+
					sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`)+
					sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null,"stop_details":null},"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`)+
					sseEvent("message_stop", `{"type":"message_stop"}`),
			)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w,
			sseEvent("message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","model":"claude-fable-5","content":[],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":20,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`)+
				sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)+
				sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Final analysis"}}`)+
				sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`)+
				sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null,"stop_details":null},"usage":{"input_tokens":20,"output_tokens":7,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`)+
				sseEvent("message_stop", `{"type":"message_stop"}`),
		)
	}))
	defer server.Close()

	apiClient := anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL),
	)
	analysisClient := newAnthropicWithClient(
		&apiClient,
		"claude-fable-5",
		"claude-sonnet-5",
		agent.NewAnthropicScout("test-key", "claude-sonnet-5", anthropicTestFileOps{}),
	)

	result, err := analysisClient.Analyze(context.Background(), "Analyze example.go", AnalysisOptions{ReasoningEffort: "xhigh"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.Text != "Final analysis" || result.ResponseID != "msg_2" {
		t.Fatalf("result = %+v", result)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if requests[0]["max_tokens"] != float64(maxAnthropicOutputTokens) {
		t.Fatalf("max_tokens = %#v, want %d", requests[0]["max_tokens"], maxAnthropicOutputTokens)
	}
	messages, ok := requests[1]["messages"].([]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("follow-up messages = %#v", requests[1]["messages"])
	}
	toolResultMessage := messages[2].(map[string]any)
	content := toolResultMessage["content"].([]any)
	toolResult := content[0].(map[string]any)
	if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "tool_1" {
		t.Fatalf("tool result = %#v", toolResult)
	}
}

func TestAnthropicAnalyzeRejectsIncompleteTerminalResponses(t *testing.T) {
	testCases := []struct {
		name       string
		stopReason string
		stopDetail string
		wantError  string
	}{
		{
			name:       "refusal",
			stopReason: "refusal",
			stopDetail: `{"type":"refusal","category":"cyber","explanation":"declined"}`,
			wantError:  "anthropic model refused the request (cyber: declined)",
		},
		{
			name:       "max tokens",
			stopReason: "max_tokens",
			stopDetail: "null",
			wantError:  "reached the 65536-token output limit",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, anthropicTextStream("msg_terminal", "partial output", tc.stopReason, tc.stopDetail))
			}))
			defer server.Close()

			analysisClient := newTestAnthropicAnalysisClient(server.URL)
			_, err := analysisClient.Analyze(context.Background(), "Analyze", AnalysisOptions{ReasoningEffort: "xhigh"})
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("Analyze error = %v, want substring %q", err, tc.wantError)
			}
		})
	}
}

func TestAnthropicAnalyzeContinuesPauseTurn(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "text/event-stream")
		if len(requests) == 1 {
			fmt.Fprint(w, anthropicTextStream("msg_pause", "working", "pause_turn", "null"))
			return
		}
		fmt.Fprint(w, anthropicTextStream("msg_final", "complete", "end_turn", "null"))
	}))
	defer server.Close()

	result, err := newTestAnthropicAnalysisClient(server.URL).Analyze(context.Background(), "Analyze", AnalysisOptions{ReasoningEffort: "xhigh"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.Text != "complete" || len(requests) != 2 {
		t.Fatalf("result = %+v, requests = %d", result, len(requests))
	}
	messages := requests[1]["messages"].([]any)
	if len(messages) != 2 || messages[1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("pause continuation messages = %#v", messages)
	}
}

func newTestAnthropicAnalysisClient(baseURL string) *AnthropicDeepAnalysisClient {
	apiClient := anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(baseURL),
	)
	return newAnthropicWithClient(
		&apiClient,
		"claude-fable-5",
		"claude-sonnet-5",
		agent.NewAnthropicScout("test-key", "claude-sonnet-5", anthropicTestFileOps{}),
	)
}

func anthropicTextStream(id, text, stopReason, stopDetails string) string {
	return sseEvent("message_start", fmt.Sprintf(`{"type":"message_start","message":{"id":%q,"type":"message","role":"assistant","model":"claude-fable-5","content":[],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`, id)) +
		sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		sseEvent("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, text)) +
		sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		sseEvent("message_delta", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null,"stop_details":%s},"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`, stopReason, stopDetails)) +
		sseEvent("message_stop", `{"type":"message_stop"}`)
}

func sseEvent(name, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", name, data)
}
