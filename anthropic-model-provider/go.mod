module github.com/boeing-ai-gateway/providers/anthropic-model-provider

go 1.26.4

replace (
	github.com/boeing-ai-gateway/providers/openai-model-provider => ../openai-model-provider
	github.com/boeing-ai-gateway/chat-completion-client => ../../chat-completion-client
)

require (
	github.com/boeing-ai-gateway/chat-completion-client v0.0.0-20260529163740-88dd50945c18
	github.com/boeing-ai-gateway/providers/openai-model-provider v0.0.0-20250327233502-e281d9bc8d01
)
