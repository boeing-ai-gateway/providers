package main

import (
	"fmt"
	"os"

	"github.com/boeing-ai-gateway/providers/openai-model-provider/proxy"
)

func main() {
	apiKey := os.Getenv("BOEING_GROQ_MODEL_PROVIDER_API_KEY")
	if apiKey == "" {
		fmt.Println("BOEING_GROQ_MODEL_PROVIDER_API_KEY is not set, credential must be provided on a per-request basis")
	}

	cfg := &proxy.Config{
		APIKey:               apiKey,
		PersonalAPIKeyHeader: "X-Boeing-BOEING_GROQ_MODEL_PROVIDER_API_KEY",
		ListenPort:           os.Getenv("PORT"),
		BaseURL:              "https://api.groq.com/openai/v1",
		RewriteModelsFn:      proxy.RewriteAllModelsWithUsage("llm"),
		Name:                 "Groq",
	}

	if err := cfg.Validate(); err != nil {
		os.Exit(1)
	}

	if len(os.Args) > 1 && os.Args[1] == "validate" {
		return
	}

	if err := proxy.Run(cfg); err != nil {
		panic(err)
	}
}
