package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/boeing-ai-gateway/providers/openai-model-provider/proxy"
)

const (
	PersonalAPIKeyHeader = "X-Boeing-BOEING_AI_MODEL_PROVIDER_API_KEY"
)

// Server handles Boeing AI specific proxy logic
type Server struct {
	cfg     *proxy.Config
	baseURL string // The root UDAL base URL (e.g., https://udal-test.web.boeing.com)
}

func NewServer(cfg *proxy.Config, baseURL string) *Server {
	return &Server{
		cfg:     cfg,
		baseURL: baseURL,
	}
}

// getAPIKey extracts the per-user API key from the request header, or falls back to global.
func (s *Server) getAPIKey(req *http.Request) string {
	if key := req.Header.Get(PersonalAPIKeyHeader); key != "" {
		return key
	}
	return s.cfg.APIKey
}

// ChatCompletionsDirector rewrites /v1/chat/completions requests to Boeing's /bcai-public-api/bcai-public-api/conversation
func (s *Server) ChatCompletionsDirector(req *http.Request) {
	apiKey := s.getAPIKey(req)
	req.Header.Del(PersonalAPIKeyHeader)

	// Read the original body
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		fmt.Println("failed to read request body:", err)
		return
	}

	// Parse the OpenAI chat completions request
	var openAIReq map[string]any
	if err := json.Unmarshal(bodyBytes, &openAIReq); err != nil {
		// If we can't parse, just forward as-is
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		s.setTargetURL(req, "/bcai-public-api/bcai-public-api/conversation")
		req.Header.Set("Authorization", "basic "+apiKey)
		return
	}

	// Transform to BCAI conversation format
	bcaiReq := transformToBCAI(openAIReq)

	modifiedBody, err := json.Marshal(bcaiReq)
	if err != nil {
		fmt.Println("failed to marshal BCAI request:", err)
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	} else {
		req.Body = io.NopCloser(bytes.NewBuffer(modifiedBody))
		req.ContentLength = int64(len(modifiedBody))
	}

	// Set target URL and auth
	s.setTargetURL(req, "/bcai-public-api/bcai-public-api/conversation")
	req.Header.Set("Authorization", "basic "+apiKey)
	req.Header.Set("Content-Type", "application/json")
}

// HandleChatCompletions is a full HTTP handler for /v1/chat/completions that handles
// both streaming and non-streaming responses, transforming BCAI format to standard OpenAI format.
func (s *Server) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	apiKey := s.getAPIKey(r)
	if apiKey == "" {
		http.Error(w, `{"error": {"message": "No API key provided", "type": "auth_error"}}`, http.StatusUnauthorized)
		return
	}

	// Read and transform request
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error": {"message": "Failed to read body"}}`, http.StatusBadRequest)
		return
	}

	var openAIReq map[string]any
	if err := json.Unmarshal(bodyBytes, &openAIReq); err != nil {
		http.Error(w, `{"error": {"message": "Invalid JSON"}}`, http.StatusBadRequest)
		return
	}

	isStreaming, _ := openAIReq["stream"].(bool)
	bcaiReq := transformToBCAI(openAIReq)

	reqBody, _ := json.Marshal(bcaiReq)

	// Make request to BCAI
	targetURL := s.baseURL + "/bcai-public-api/bcai-public-api/conversation"
	proxyReq, err := http.NewRequestWithContext(r.Context(), "POST", targetURL, bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, `{"error": {"message": "Failed to create request"}}`, http.StatusInternalServerError)
		return
	}

	proxyReq.Header.Set("Authorization", "basic "+apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")
	if isStreaming {
		proxyReq.Header.Set("Accept", "application/json")
	} else {
		proxyReq.Header.Set("Accept", "application/json")
	}

	// Use a transport that doesn't impose timeouts on streaming responses
	client := &http.Client{
		Timeout: 0, // no timeout for streaming
	}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": {"message": "Boeing AI unreachable: %v"}}`, err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Forward error as-is
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	if !isStreaming {
		// Non-streaming: forward response directly (already in OpenAI format)
		w.Header().Set("Content-Type", "application/json")
		io.Copy(w, resp.Body)
		return
	}

	// Streaming: read BCAI chunks and convert to standard OpenAI SSE format
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	// BCAI returns newline-delimited JSON chunks (not SSE with "data: " prefix)
	// Read line by line
	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer size for large chunks
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var chunk map[string]any
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			// Skip non-JSON lines
			continue
		}

		// Transform BCAI chunk to OpenAI SSE chunk format
		openAIChunk := transformStreamChunk(chunk)
		chunkJSON, err := json.Marshal(openAIChunk)
		if err != nil {
			continue
		}

		fmt.Fprintf(w, "data: %s\n\n", chunkJSON)
		if canFlush {
			flusher.Flush()
		}
	}

	// Send the final [DONE] marker
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}
}

// transformStreamChunk converts a BCAI streaming chunk to OpenAI SSE chunk format.
// BCAI format: {"choices": [{"messages": [{"role": "assistant", "content": "full", "delta": "new"}], "finish_reason": null}]}
// OpenAI format: {"choices": [{"index": 0, "delta": {"content": "new"}, "finish_reason": null}]}
func transformStreamChunk(bcaiChunk map[string]any) map[string]any {
	result := map[string]any{
		"id":      bcaiChunk["id"],
		"object":  "chat.completion.chunk",
		"created": bcaiChunk["created"],
		"model":   bcaiChunk["model"],
	}

	choices, ok := bcaiChunk["choices"].([]any)
	if !ok || len(choices) == 0 {
		result["choices"] = []any{}
		return result
	}

	var openAIChoices []any
	for i, c := range choices {
		choice, ok := c.(map[string]any)
		if !ok {
			continue
		}

		finishReason := choice["finish_reason"]

		// Extract delta content from BCAI's messages array
		var deltaContent string
		var role string
		if messages, ok := choice["messages"].([]any); ok && len(messages) > 0 {
			if msg, ok := messages[0].(map[string]any); ok {
				if d, ok := msg["delta"].(string); ok {
					deltaContent = d
				}
				if r, ok := msg["role"].(string); ok {
					role = r
				}
			}
		}

		// Also handle standard OpenAI delta format (in case BCAI returns it)
		if delta, ok := choice["delta"].(map[string]any); ok {
			if content, ok := delta["content"].(string); ok {
				deltaContent = content
			}
			if r, ok := delta["role"].(string); ok {
				role = r
			}
		}

		openAIChoice := map[string]any{
			"index":         i,
			"finish_reason": finishReason,
		}

		delta := map[string]any{}
		if deltaContent != "" {
			delta["content"] = deltaContent
		}
		if role != "" && deltaContent == "" {
			// First chunk typically has role but no content
			delta["role"] = role
		}
		openAIChoice["delta"] = delta

		openAIChoices = append(openAIChoices, openAIChoice)
	}

	result["choices"] = openAIChoices

	// Pass through usage if present
	if usage, ok := bcaiChunk["usage"]; ok && usage != nil {
		result["usage"] = usage
	}

	return result
}

// EmbeddingsDirector rewrites /v1/embeddings requests to Boeing's /bcai-public-api/bcai-public-api/embedding
func (s *Server) EmbeddingsDirector(req *http.Request) {
	apiKey := s.getAPIKey(req)
	req.Header.Del(PersonalAPIKeyHeader)

	s.setTargetURL(req, "/bcai-public-api/bcai-public-api/embedding")
	req.Header.Set("Authorization", "basic "+apiKey)
	req.Header.Set("Content-Type", "application/json")
}

// setTargetURL sets the request URL to the Boeing AI endpoint
func (s *Server) setTargetURL(req *http.Request, path string) {
	req.URL.Scheme = "https"
	host := strings.TrimPrefix(s.baseURL, "https://")
	host = strings.TrimPrefix(host, "http://")
	req.URL.Host = host
	req.URL.Path = path
	req.Host = req.URL.Host
}

// transformToBCAI converts an OpenAI chat completions request to BCAI conversation format
func transformToBCAI(openAIReq map[string]any) map[string]any {
	bcaiReq := map[string]any{
		"model":    openAIReq["model"],
		"messages": openAIReq["messages"],
		"stream":   openAIReq["stream"],
		// BCAI-specific required fields
		"conversation_mode":   []string{"non-rag"},
		"conversation_guid":   "boeing-session",
		"conversation_name":   "",
		"conversation_source": "boeing",
		"skip_db_save":        true,
	}

	model, _ := openAIReq["model"].(string)
	isGPT5 := isGPT5Model(model)

	// Map OpenAI parameters to BCAI equivalents
	// GPT-5 models only support temperature=1, so skip temperature for them
	if v, ok := openAIReq["temperature"]; ok && v != nil && !isGPT5 {
		bcaiReq["temperature"] = v
	}
	if v, ok := openAIReq["max_tokens"]; ok && v != nil {
		bcaiReq["response_max_tokens"] = v
	}
	if v, ok := openAIReq["top_p"]; ok && v != nil && !isGPT5 {
		bcaiReq["top_p"] = v
	}
	if v, ok := openAIReq["stop"]; ok && v != nil {
		bcaiReq["stop"] = v
	}
	if v, ok := openAIReq["response_format"]; ok && v != nil {
		bcaiReq["response_format"] = v
	}
	if v, ok := openAIReq["frequency_penalty"]; ok && v != nil && !isGPT5 {
		bcaiReq["frequency_penalty"] = v
	}
	if v, ok := openAIReq["presence_penalty"]; ok && v != nil && !isGPT5 {
		bcaiReq["presence_penalty"] = v
	}
	if v, ok := openAIReq["seed"]; ok && v != nil {
		bcaiReq["seed"] = v
	}
	if v, ok := openAIReq["tools"]; ok && v != nil {
		bcaiReq["tools"] = v
	}
	if v, ok := openAIReq["tool_choice"]; ok && v != nil {
		bcaiReq["tool_choice"] = v
	}
	if v, ok := openAIReq["parallel_tool_calls"]; ok && v != nil {
		bcaiReq["parallel_tool_calls"] = v
	}

	return bcaiReq
}

// isGPT5Model returns true if the model is a reasoning model that doesn't support temperature/top_p/penalties
// This includes GPT-5 family, o3, o4 models
func isGPT5Model(model string) bool {
	if len(model) >= 5 && model[:5] == "gpt-5" {
		return true
	}
	if len(model) >= 2 && (model[:2] == "o3" || model[:2] == "o4") {
		return true
	}
	return false
}
