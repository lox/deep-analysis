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

	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	harness "github.com/lox/agent-harness"
	harnessanthropic "github.com/lox/agent-harness/provider/anthropic"
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

	provider := harnessanthropic.New(
		harnessanthropic.WithAPIKey("test-key"),
		harnessanthropic.WithBaseURL(server.URL),
		harnessanthropic.WithDefaultModel("claude-sonnet-5"),
		harnessanthropic.WithRequestOption(anthropicoption.WithJSONSet("tools.0.strict", true)),
		harnessanthropic.WithRequestOption(anthropicoption.WithJSONSet("tools.1.strict", true)),
		harnessanthropic.WithRequestOption(anthropicoption.WithJSONSet("tools.2.strict", true)),
	)
	analysisClient := newAnthropicWithProvider(
		provider,
		"claude-sonnet-5",
		"claude-sonnet-5",
		agent.NewAnthropicScout("test-key", "claude-sonnet-5", "low", anthropicTestFileOps{}),
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
	for i, request := range requests {
		cacheControl, ok := request["cache_control"].(map[string]any)
		if !ok || cacheControl["type"] != "ephemeral" {
			t.Fatalf("request %d cache_control = %#v, want ephemeral", i+1, request["cache_control"])
		}
		if _, ok := cacheControl["ttl"]; ok {
			t.Fatalf("request %d cache_control = %#v, want default five-minute TTL", i+1, cacheControl)
		}
		outputConfig := request["output_config"].(map[string]any)
		if outputConfig["effort"] != "xhigh" {
			t.Fatalf("request %d effort = %#v, want xhigh", i+1, outputConfig["effort"])
		}
		thinking := request["thinking"].(map[string]any)
		if thinking["type"] != "adaptive" {
			t.Fatalf("request %d thinking = %#v, want adaptive", i+1, thinking)
		}
		for j, tool := range request["tools"].([]any) {
			if tool.(map[string]any)["strict"] != true {
				t.Fatalf("request %d tool %d strict = %#v, want true", i+1, j, tool.(map[string]any)["strict"])
			}
		}
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

func TestAnthropicAnalyzeReturnsToolErrorsToResearcher(t *testing.T) {
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
			fmt.Fprint(w, anthropicToolStream("msg_tool", "tool_bad", "read_files", `{"paths":42}`))
			return
		}
		fmt.Fprint(w, anthropicTextStream("msg_final", "Recovered", "end_turn", "null"))
	}))
	defer server.Close()

	result, err := newTestAnthropicAnalysisClient(server.URL).Analyze(context.Background(), "Analyze", AnalysisOptions{ReasoningEffort: "xhigh"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.Text != "Recovered" || result.ResponseID != "msg_final" {
		t.Fatalf("result = %+v", result)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	messages := requests[1]["messages"].([]any)
	toolResultMessage := messages[len(messages)-1].(map[string]any)
	content := toolResultMessage["content"].([]any)
	toolResult := content[0].(map[string]any)
	encodedToolResult, err := json.Marshal(toolResult["content"])
	if err != nil {
		t.Fatalf("encode tool result content: %v", err)
	}
	if toolResult["tool_use_id"] != "tool_bad" || toolResult["is_error"] != true || !strings.Contains(string(encodedToolResult), "Error: invalid arguments:") {
		t.Fatalf("tool result = %#v", toolResult)
	}
}

func TestAnthropicAnalyzeStopsAtMaxIterations(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicToolStream(
			fmt.Sprintf("msg_%d", requests),
			fmt.Sprintf("tool_%d", requests),
			"read_files",
			`{"paths":42}`,
		))
	}))
	defer server.Close()

	analysisClient := newTestAnthropicAnalysisClient(server.URL)
	toolExecutions := 0
	for i := range analysisClient.runtime.tools {
		execute := analysisClient.runtime.tools[i].Execute
		analysisClient.runtime.tools[i].Execute = func(ctx context.Context, call harness.ToolCall) (*harness.ToolResult, error) {
			toolExecutions++
			return execute(ctx, call)
		}
	}
	_, err := analysisClient.Analyze(context.Background(), "Analyze", AnalysisOptions{ReasoningEffort: "xhigh"})
	if err == nil || err.Error() != fmt.Sprintf("max function call iterations (%d) reached", maxIterations) {
		t.Fatalf("Analyze error = %v", err)
	}
	if requests != maxIterations+1 {
		t.Fatalf("requests = %d, want %d", requests, maxIterations+1)
	}
	if toolExecutions != maxIterations {
		t.Fatalf("tool executions = %d, want %d", toolExecutions, maxIterations)
	}
}

func TestAnthropicAnalyzeCompletesOnLastStepAndPreservesTextBlocks(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		if requests <= maxIterations {
			fmt.Fprint(w, anthropicToolStream(
				fmt.Sprintf("msg_%d", requests),
				fmt.Sprintf("tool_%d", requests),
				"read_files",
				`{"paths":42}`,
			))
			return
		}
		fmt.Fprint(w, anthropicMultiTextStream("msg_final", "first block", "second block"))
	}))
	defer server.Close()

	result, err := newTestAnthropicAnalysisClient(server.URL).Analyze(context.Background(), "Analyze", AnalysisOptions{ReasoningEffort: "xhigh"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if requests != maxIterations+1 || result.Text != "first block\nsecond block" || result.ResponseID != "msg_final" {
		t.Fatalf("requests = %d, result = %+v", requests, result)
	}
}

func TestEstimateAnthropicCostIncludesCacheWritePremiums(t *testing.T) {
	usage := anthropicUsageTotals{
		inputTokens:                1_000_000,
		cacheCreation5mInputTokens: 1_000_000,
		cacheCreation1hInputTokens: 1_000_000,
		cacheReadInputTokens:       1_000_000,
		outputTokens:               1_000_000,
	}

	const want = 93.5 // $10 input + $12.50 5m write + $20 1h write + $1 read + $50 output
	if got := estimateAnthropicCost("claude-fable-5", usage); got != want {
		t.Fatalf("estimateAnthropicCost = %v, want %v", got, want)
	}
}

func TestAnthropicUsageFromHarnessTracksCacheCategories(t *testing.T) {
	usage := anthropicUsageFromHarness(harness.Usage{
		InputTokens:                100,
		CacheCreationInputTokens:   200,
		CacheCreation5mInputTokens: 125,
		CacheCreation1hInputTokens: 75,
		CacheReadInputTokens:       300,
		OutputTokens:               400,
	}, 1)

	if usage.inputTokens != 100 ||
		usage.cacheCreation5mInputTokens != 125 ||
		usage.cacheCreation1hInputTokens != 75 ||
		usage.cacheReadInputTokens != 300 ||
		usage.outputTokens != 400 ||
		usage.apiCalls != 1 ||
		usage.cacheCreationInputTokens() != 200 ||
		usage.totalInputTokens() != 600 {
		t.Fatalf("usage = %+v", usage)
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
	provider := harnessanthropic.New(
		harnessanthropic.WithAPIKey("test-key"),
		harnessanthropic.WithBaseURL(baseURL),
		harnessanthropic.WithDefaultModel("claude-fable-5"),
		harnessanthropic.WithRequestOption(anthropicoption.WithJSONSet("tools.0.strict", true)),
		harnessanthropic.WithRequestOption(anthropicoption.WithJSONSet("tools.1.strict", true)),
		harnessanthropic.WithRequestOption(anthropicoption.WithJSONSet("tools.2.strict", true)),
	)
	return newAnthropicWithProvider(
		provider,
		"claude-fable-5",
		"claude-sonnet-5",
		agent.NewAnthropicScout("test-key", "claude-sonnet-5", "low", anthropicTestFileOps{}),
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

func anthropicToolStream(messageID, toolID, name, input string) string {
	return sseEvent("message_start", fmt.Sprintf(`{"type":"message_start","message":{"id":%q,"type":"message","role":"assistant","model":"claude-fable-5","content":[],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`, messageID)) +
		sseEvent("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":%q,"name":%q,"input":%s}}`, toolID, name, input)) +
		sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null,"stop_details":null},"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`) +
		sseEvent("message_stop", `{"type":"message_stop"}`)
}

func anthropicMultiTextStream(id string, texts ...string) string {
	stream := sseEvent("message_start", fmt.Sprintf(`{"type":"message_start","message":{"id":%q,"type":"message","role":"assistant","model":"claude-fable-5","content":[],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`, id))
	for i, text := range texts {
		stream += sseEvent("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, i))
		stream += sseEvent("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%q}}`, i, text))
		stream += sseEvent("content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i))
	}
	return stream +
		sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null,"stop_details":null},"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`) +
		sseEvent("message_stop", `{"type":"message_stop"}`)
}

func sseEvent(name, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", name, data)
}
