package main

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"

	boeingproxy "github.com/boeing-ai-gateway/providers/boeing-ai-model-provider/proxy"
	"github.com/boeing-ai-gateway/providers/openai-model-provider/proxy"
)

func main() {
	apiKey := os.Getenv("BOEING_AI_MODEL_PROVIDER_API_KEY")
	if apiKey == "" {
		fmt.Println("BOEING_AI_MODEL_PROVIDER_API_KEY not set, credential must be provided on a per-request basis")
	}

	baseURL := os.Getenv("BOEING_AI_MODEL_PROVIDER_BASE_URL")
	if baseURL == "" {
		baseURL = "https://udal-test.web.boeing.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	// The proxy framework base URL points to the BCAI conversation API path.
	// The proxy will append the request path after /v1 to this base URL.
	cfg := &proxy.Config{
		APIKey:               apiKey,
		PersonalAPIKeyHeader: "X-Boeing-BOEING_AI_MODEL_PROVIDER_API_KEY",
		ListenPort:           port,
		BaseURL:              baseURL + "/bcai-public-api/bcai-public-api",
		Name:                 "Boeing AI",
		CustomPathHandleFuncs: map[string]http.HandlerFunc{},
		// Custom validation: call the Boeing authorized endpoint
		ValidateFn: func(cfg *proxy.Config) error {
			return boeingproxy.ValidateBoeingAI(cfg, baseURL)
		},
		// Rewrite auth header from Bearer to basic (Boeing AI uses basic auth with PAT)
		RewriteHeaderFn: func(header http.Header) {
			// Extract the key from Bearer header (set by proxy framework) and switch to basic
			auth := header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				key := strings.TrimPrefix(auth, "Bearer ")
				header.Set("Authorization", "basic "+key)
			}
		},
	}

	if err := cfg.EnsureURL(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid base URL: %v\n", err)
		os.Exit(1)
	}

	// Create Boeing AI proxy server for custom endpoint handling
	boeingServer := boeingproxy.NewServer(cfg, baseURL)

	// Custom handler for /v1/models - fetches from Boeing Security API
	cfg.CustomPathHandleFuncs["/v1/models"] = boeingServer.HandleModels

	// Custom handler for /v1/chat/completions - transforms and proxies to BCAI conversation
	// Handles both streaming (with SSE format conversion) and non-streaming
	cfg.CustomPathHandleFuncs["/v1/chat/completions"] = boeingServer.HandleChatCompletions

	// Custom handler for /v1/embeddings - proxies to BCAI embedding endpoint
	cfg.CustomPathHandleFuncs["/v1/embeddings"] = (&httputil.ReverseProxy{
		Director: boeingServer.EmbeddingsDirector,
	}).ServeHTTP

	// Validate
	if err := cfg.Validate(); err != nil {
		os.Exit(1)
	}

	if len(os.Args) > 1 && os.Args[1] == "validate" {
		return
	}

	if err := proxy.Run(cfg); err != nil {
		fmt.Printf("failed to run boeing-ai-model-provider proxy: %v\n", err)
		os.Exit(1)
	}
}
