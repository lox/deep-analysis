package client

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	harness "github.com/lox/agent-harness"
	harnessopenai "github.com/lox/agent-harness/provider/openai"
	"github.com/lox/deep-analysis/internal/agent"
)

func TestEstimateCostSupportsCurrentAndLegacyModels(t *testing.T) {
	testCases := []struct {
		name         string
		model        string
		inputTokens  int64
		outputTokens int64
		want         float64
	}{
		{
			name:         "claude-fable-5",
			model:        "claude-fable-5",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         60.0,
		},
		{
			name:         "claude-opus-4-8",
			model:        "claude-opus-4-8",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         30.0,
		},
		{
			name:         "claude-sonnet-5",
			model:        "claude-sonnet-5",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         18.0,
		},
		{
			name:         "gpt-5.6-sol",
			model:        "gpt-5.6-sol",
			inputTokens:  100_000,
			outputTokens: 100_000,
			want:         3.5,
		},
		{
			name:         "gpt-5.6-sol snapshot",
			model:        "gpt-5.6-sol-2026-07-16",
			inputTokens:  100_000,
			outputTokens: 100_000,
			want:         3.5,
		},
		{
			name:         "gpt-5.6-terra",
			model:        "gpt-5.6-terra",
			inputTokens:  100_000,
			outputTokens: 100_000,
			want:         1.75,
		},
		{
			name:         "gpt-5.6-luna",
			model:        "gpt-5.6-luna",
			inputTokens:  100_000,
			outputTokens: 100_000,
			want:         0.7,
		},
		{
			name:         "gpt-5.5-pro",
			model:        "gpt-5.5-pro",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         210.0,
		},
		{
			name:         "gpt-5.5-pro snapshot",
			model:        "gpt-5.5-pro-2026-06-18",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         210.0,
		},
		{
			name:         "gpt-5.5",
			model:        "gpt-5.5",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         35.0,
		},
		{
			name:         "gpt-5.5 snapshot",
			model:        "gpt-5.5-2026-06-18",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         35.0,
		},
		{
			name:         "gpt-5.4",
			model:        "gpt-5.4",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         17.5,
		},
		{
			name:         "gpt-5.4-mini",
			model:        "gpt-5.4-mini",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         5.25,
		},
		{
			name:         "gpt-5.4-pro",
			model:        "gpt-5.4-pro",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         210.0,
		},
		{
			name:         "gpt-5.4 snapshot",
			model:        "gpt-5.4-2026-03-10",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         17.5,
		},
		{
			name:         "gpt-5.4-pro snapshot",
			model:        "gpt-5.4-pro-2026-03-10",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         210.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := estimateCost(tc.model, tc.inputTokens, 0, 0, tc.outputTokens)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("estimateCost(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

func TestEstimateCostUsesCachedInputPricingWhenAvailable(t *testing.T) {
	got := estimateCost("gpt-5.5", 1_000_000, 400_000, 0, 1_000_000)

	const want = 33.2 // 600k uncached @ $5 + 400k cached @ $0.50 + 1M output @ $30
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("estimateCost(gpt-5.5) with cached input = %v, want %v", got, want)
	}
}

func TestEstimateCostIncludesGPT56CacheWrites(t *testing.T) {
	got := estimateCost("gpt-5.6-sol", 200_000, 0, 80_000, 100_000)

	const want = 4.1 // 120k uncached @ $5 + 80k cache write @ $6.25 + 100k output @ $30
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("estimateCost(gpt-5.6-sol) with cache writes = %v, want %v", got, want)
	}
}

func TestEstimateCostAppliesGPT56LongContextPricingPerResponse(t *testing.T) {
	longRequestCost := estimateCost("gpt-5.6-sol", 300_000, 100_000, 50_000, 100_000)
	const wantLongRequest = 6.725 // 150k uncached @ $10 + 100k cached @ $1 + 50k write @ $12.50 + 100k output @ $45
	if math.Abs(longRequestCost-wantLongRequest) > 1e-9 {
		t.Fatalf("long GPT-5.6 request cost = %v, want %v", longRequestCost, wantLongRequest)
	}

	gotSeparateRequests := estimateHarnessRunCost("gpt-5.6-sol", []harness.Usage{
		{InputTokens: 200_000, OutputTokens: 100_000},
		{InputTokens: 200_000, OutputTokens: 100_000},
	})
	const wantSeparateRequests = 8.0 // Each request stays below the 272K threshold.
	if math.Abs(gotSeparateRequests-wantSeparateRequests) > 1e-9 {
		t.Fatalf("separate GPT-5.6 request cost = %v, want %v", gotSeparateRequests, wantSeparateRequests)
	}
}

func TestEstimateHarnessCallCostIncludesCacheCategories(t *testing.T) {
	got := estimateHarnessCallCost("gpt-5.6-sol", harness.Usage{
		InputTokens:              120_000,
		CacheCreationInputTokens: 80_000,
		OutputTokens:             100_000,
	})
	const want = 4.1
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("harness call cost = %v, want %v", got, want)
	}
}

func TestNewUsesConfiguredResearcherAndScoutModels(t *testing.T) {
	c := New("test-key", nil, "gpt-5.4", "gpt-5.4-pro")

	if c.researcherModel != "gpt-5.4" {
		t.Fatalf("researcherModel = %q, want %q", c.researcherModel, "gpt-5.4")
	}
	if c.scoutModel != "gpt-5.4-pro" {
		t.Fatalf("scoutModel = %q, want %q", c.scoutModel, "gpt-5.4-pro")
	}
}

func TestNewUsesDefaultModelsWhenUnset(t *testing.T) {
	c := New("test-key", nil, "", "")

	if c.researcherModel != DefaultResearcherModel {
		t.Fatalf("researcherModel = %q, want %q", c.researcherModel, DefaultResearcherModel)
	}
	if c.scoutModel != agent.DefaultScoutModel {
		t.Fatalf("scoutModel = %q, want %q", c.scoutModel, agent.DefaultScoutModel)
	}
}

func TestDefaultModelsForAnthropic(t *testing.T) {
	researcher, scout, err := DefaultModelsForProvider(AnthropicProvider)
	if err != nil {
		t.Fatalf("DefaultModelsForProvider: %v", err)
	}
	if researcher != DefaultAnthropicResearcherModel || scout != DefaultAnthropicScoutModel {
		t.Fatalf("models = %q, %q", researcher, scout)
	}
}

func TestNewForProvidersMixesResearcherAndScout(t *testing.T) {
	testCases := []struct {
		name               string
		researcherProvider string
		scoutProvider      string
		wantAnalyzer       string
	}{
		{"anthropic researcher with openai scout", AnthropicProvider, OpenAIProvider, "anthropic"},
		{"openai researcher with anthropic scout", OpenAIProvider, AnthropicProvider, "openai"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			analyzer, err := NewForProviders(tc.researcherProvider, "researcher-key", tc.scoutProvider, "scout-key", nil, "", "", "low")
			if err != nil {
				t.Fatalf("NewForProviders: %v", err)
			}

			switch typed := analyzer.(type) {
			case *AnthropicDeepAnalysisClient:
				if tc.wantAnalyzer != "anthropic" || typed.runtime.scout.Provider() != tc.scoutProvider {
					t.Fatalf("analyzer/scout providers = anthropic/%s", typed.runtime.scout.Provider())
				}
			case *DeepAnalysisClient:
				if tc.wantAnalyzer != "openai" || typed.scout.Provider() != tc.scoutProvider {
					t.Fatalf("analyzer/scout providers = openai/%s", typed.scout.Provider())
				}
			default:
				t.Fatalf("unexpected analyzer type %T", analyzer)
			}
		})
	}
}

func TestProviderToolsMatchResearcherWorkflow(t *testing.T) {
	openAITools := New("test-key", nil, "", "").tools
	anthropicTools := NewAnthropic("test-key", nil, "", "").runtime.tools
	for _, tc := range []struct {
		provider    string
		tools       []harness.Tool
		definitions []researcherToolDefinition
	}{
		{OpenAIProvider, openAITools, researcherToolDefinitions(OpenAIProvider)},
		{AnthropicProvider, anthropicTools, researcherToolDefinitions(AnthropicProvider)},
	} {
		if len(tc.definitions) != 3 || len(tc.tools) != len(tc.definitions) {
			t.Fatalf("%s tool counts = definitions:%d runtime:%d, want 3 each", tc.provider, len(tc.definitions), len(tc.tools))
		}
		for i, definition := range tc.definitions {
			tool := tc.tools[i].ToolDef
			if tool.Name != definition.name || tool.Description != definition.description {
				t.Fatalf("%s tool %d = %q (%q), want %q (%q)", tc.provider, i, tool.Name, tool.Description, definition.name, definition.description)
			}
			var parameters map[string]any
			if err := json.Unmarshal(tool.Parameters, &parameters); err != nil {
				t.Fatalf("decode %s tool %d parameters: %v", tc.provider, i, err)
			}
			expectedJSON, err := json.Marshal(definition.parameters())
			if err != nil {
				t.Fatalf("encode %s tool %d definition: %v", tc.provider, i, err)
			}
			var expectedParameters map[string]any
			if err := json.Unmarshal(expectedJSON, &expectedParameters); err != nil {
				t.Fatalf("decode %s tool %d definition: %v", tc.provider, i, err)
			}
			if !reflect.DeepEqual(parameters, expectedParameters) {
				t.Fatalf("%s tool %d properties differ from definition", tc.provider, i)
			}
		}
	}
}

func TestOpenAIAnalyzePreservesContinuationAndToolErrors(t *testing.T) {
	for _, tc := range []struct {
		name               string
		previousResponseID string
	}{
		{name: "continue", previousResponseID: "resp_previous"},
		{name: "reset"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				mu.Lock()
				requests = append(requests, request)
				requestNumber := len(requests)
				mu.Unlock()

				w.Header().Set("Content-Type", "application/json")
				if requestNumber == 1 {
					fmt.Fprint(w, openAIResponseJSON("resp_1", `{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read_files","arguments":"{\"paths\":42}","status":"completed"}`))
					return
				}
				fmt.Fprint(w, openAIResponseJSON("resp_2", `{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Final analysis","annotations":[]}]}`))
			}))
			defer server.Close()

			result, err := newTestOpenAIAnalysisClient(server.URL).Analyze(context.Background(), "Analyze", AnalysisOptions{
				PreviousResponseID: tc.previousResponseID,
				ReasoningEffort:    "xhigh",
			})
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if result != (AnalysisResult{Text: "Final analysis", ResponseID: "resp_2"}) {
				t.Fatalf("result = %+v", result)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 2 {
				t.Fatalf("requests = %d, want 2", len(requests))
			}
			previous, hasPrevious := requests[0]["previous_response_id"]
			if tc.previousResponseID == "" && hasPrevious {
				t.Fatalf("reset request carried previous_response_id = %#v", previous)
			}
			if tc.previousResponseID != "" && previous != tc.previousResponseID {
				t.Fatalf("previous_response_id = %#v, want %q", previous, tc.previousResponseID)
			}
			if requests[0]["prompt_cache_key"] != "deep-analysis:researcher:gpt-5.6-sol:v4" {
				t.Fatalf("prompt_cache_key = %#v", requests[0]["prompt_cache_key"])
			}
			for i, tool := range requests[0]["tools"].([]any) {
				if tool.(map[string]any)["strict"] != true {
					t.Fatalf("tool %d strict = %#v, want true", i, tool.(map[string]any)["strict"])
				}
			}
			reasoning := requests[0]["reasoning"].(map[string]any)
			if reasoning["effort"] != "xhigh" || reasoning["mode"] != "pro" {
				t.Fatalf("reasoning = %#v", reasoning)
			}
			if requests[1]["previous_response_id"] != "resp_1" {
				t.Fatalf("tool continuation response id = %#v", requests[1]["previous_response_id"])
			}
			if requests[1]["prompt_cache_key"] != requests[0]["prompt_cache_key"] ||
				requests[1]["instructions"] != requests[0]["instructions"] ||
				!reflect.DeepEqual(requests[1]["reasoning"], requests[0]["reasoning"]) ||
				!reflect.DeepEqual(requests[1]["tools"], requests[0]["tools"]) {
				t.Fatal("follow-up request did not preserve the cacheable researcher prefix")
			}
			input := requests[1]["input"].([]any)
			output := input[0].(map[string]any)
			if output["type"] != "function_call_output" || output["call_id"] != "call_1" || !strings.Contains(output["output"].(string), "Error: invalid arguments:") {
				t.Fatalf("tool error output = %#v", output)
			}
		})
	}
}

func TestOpenAIAnalyzeStopsAtMaxIterations(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requests++
		requestNumber := requests
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, openAIResponseJSON(
			fmt.Sprintf("resp_%d", requestNumber),
			fmt.Sprintf(`{"id":"fc_%d","type":"function_call","call_id":"call_%d","name":"read_files","arguments":"{\"paths\":42}","status":"completed"}`, requestNumber, requestNumber),
		))
	}))
	defer server.Close()

	analysisClient := newTestOpenAIAnalysisClient(server.URL)
	toolExecutions := 0
	for i := range analysisClient.tools {
		execute := analysisClient.tools[i].Execute
		analysisClient.tools[i].Execute = func(ctx context.Context, call harness.ToolCall) (*harness.ToolResult, error) {
			toolExecutions++
			return execute(ctx, call)
		}
	}
	_, err := analysisClient.Analyze(context.Background(), "Analyze", AnalysisOptions{})
	if err == nil || err.Error() != fmt.Sprintf("max function call iterations (%d) reached", maxIterations) {
		t.Fatalf("Analyze error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != maxIterations+1 {
		t.Fatalf("requests = %d, want %d", requests, maxIterations+1)
	}
	if toolExecutions != maxIterations {
		t.Fatalf("tool executions = %d, want %d", toolExecutions, maxIterations)
	}
}

func TestOpenAIAnalyzeCompletesOnLastStepAndPreservesTextBlocks(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests <= maxIterations {
			fmt.Fprint(w, openAIResponseJSON(
				fmt.Sprintf("resp_%d", requests),
				fmt.Sprintf(`{"id":"fc_%d","type":"function_call","call_id":"call_%d","name":"read_files","arguments":"{\"paths\":42}","status":"completed"}`, requests, requests),
			))
			return
		}
		fmt.Fprint(w, openAIResponseJSON("resp_final", `{"id":"msg_final","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"first block","annotations":[]},{"type":"output_text","text":"second block","annotations":[]}]}`))
	}))
	defer server.Close()

	result, err := newTestOpenAIAnalysisClient(server.URL).Analyze(context.Background(), "Analyze", AnalysisOptions{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if requests != maxIterations+1 || result.Text != "first block\nsecond block" || result.ResponseID != "resp_final" {
		t.Fatalf("requests = %d, result = %+v", requests, result)
	}
}

func TestOpenAIAnalyzeRejectsIncompleteTerminalResponses(t *testing.T) {
	testCases := []struct {
		name      string
		response  string
		wantError string
	}{
		{
			name:      "refusal",
			response:  `{"id":"resp_refusal","object":"response","created_at":1,"model":"gpt-5.6-sol","status":"completed","output":[{"id":"msg_refusal","type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"declined"}]}]}`,
			wantError: "OpenAI model refused the request (declined)",
		},
		{
			name:      "max tokens",
			response:  `{"id":"resp_max","object":"response","created_at":1,"model":"gpt-5.6-sol","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}`,
			wantError: "OpenAI response incomplete (max_output_tokens)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.response)
			}))
			defer server.Close()

			_, err := newTestOpenAIAnalysisClient(server.URL).Analyze(context.Background(), "Analyze", AnalysisOptions{})
			if err == nil || err.Error() != tc.wantError {
				t.Fatalf("Analyze error = %v, want %q", err, tc.wantError)
			}
		})
	}
}

func TestLegacyOpenAIResearcherDoesNotReceiveGPT56ProMode(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, openAIResponseJSON("resp_final", `{"id":"msg_final","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[]}]}`))
	}))
	defer server.Close()

	_, err := newTestOpenAIAnalysisClientWithModel(server.URL, "gpt-5.5").Analyze(context.Background(), "Analyze", AnalysisOptions{ReasoningEffort: "high"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	reasoning := request["reasoning"].(map[string]any)
	if _, ok := reasoning["mode"]; ok {
		t.Fatalf("legacy reasoning mode = %#v, want omitted", reasoning["mode"])
	}
}

func TestResearcherToolOutputsRemainCachedAcrossHarnessCalls(t *testing.T) {
	fileOps := &countingFileOps{}
	client := New("test-key", fileOps, "gpt-5.5-pro", "gpt-5.5")
	arguments := `{"paths":["example.go"]}`

	first, err := client.executeFunction(context.Background(), "read_files", arguments)
	if err != nil {
		t.Fatalf("first executeFunction: %v", err)
	}
	second, err := client.executeFunction(context.Background(), "read_files", arguments)
	if err != nil {
		t.Fatalf("second executeFunction: %v", err)
	}
	if first != second || fileOps.reads != 1 {
		t.Fatalf("results equal = %t, reads = %d, want 1", first == second, fileOps.reads)
	}
}

type countingFileOps struct {
	anthropicTestFileOps
	reads int
}

func (f *countingFileOps) ReadFile(context.Context, string) (string, error) {
	f.reads++
	return "package example\n", nil
}

func newTestOpenAIAnalysisClient(baseURL string) *DeepAnalysisClient {
	return newTestOpenAIAnalysisClientWithModel(baseURL, "gpt-5.6-sol")
}

func newTestOpenAIAnalysisClientWithModel(baseURL, model string) *DeepAnalysisClient {
	provider := harnessopenai.New(
		harnessopenai.WithAPIKey("test-key"),
		harnessopenai.WithBaseURL(baseURL),
		harnessopenai.WithDefaultModel(model),
	)
	return newOpenAIWithProvider(
		provider,
		model,
		"gpt-5.5",
		agent.NewScout("test-key", "gpt-5.5", "low", nil),
	)
}

func openAIResponseJSON(id, output string) string {
	return fmt.Sprintf(`{
		"id":%q,
		"object":"response",
		"created_at":1,
		"model":"gpt-5.6-sol",
		"status":"completed",
		"output":[%s],
		"usage":{
			"input_tokens":10,
			"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":3},
			"output_tokens":5,
			"output_tokens_details":{"reasoning_tokens":1},
			"total_tokens":15
		}
	}`, id, output)
}
