package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/boeing-ai-gateway/providers/openai-model-provider/api"
)

// HandleModels handles GET /v1/models by fetching from Boeing Security API
// and transforming the response to OpenAI /v1/models format.
func (s *Server) HandleModels(w http.ResponseWriter, r *http.Request) {
	apiKey := s.getAPIKey(r)
	if apiKey == "" {
		http.Error(w, `{"error": {"message": "No API key provided. Please configure your UDAL Personal Access Token.", "type": "authentication_error"}}`, http.StatusUnauthorized)
		return
	}

	var models []api.Model

	// Fetch conversation models from Boeing Security API
	convModels, err := s.fetchBoeingModels(r, apiKey, "/bcai-public-security-api/models")
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": {"message": "Failed to fetch models: %v", "type": "api_error"}}`, err), http.StatusBadGateway)
		return
	}
	models = append(models, convModels...)

	// Fetch embedding models from Boeing Security API
	embModels, err := s.fetchBoeingEmbeddingModels(r, apiKey)
	if err != nil {
		// Non-fatal: log but continue with conversation models only
		fmt.Printf("[Boeing AI] Warning: failed to fetch embedding models: %v\n", err)
	} else {
		models = append(models, embModels...)
	}

	// Return OpenAI-format response
	response := api.ModelsResponse{
		Object: "list",
		Data:   models,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// fetchBoeingModels fetches conversation/batch models from the Boeing Security API
func (s *Server) fetchBoeingModels(r *http.Request, apiKey, path string) ([]api.Model, error) {
	url := s.baseURL + path

	req, err := http.NewRequestWithContext(r.Context(), "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "basic "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	// Parse Boeing models response
	var boeingModels []struct {
		ModelID                  string   `json:"model_id"`
		ModelType                string   `json:"model_type"`
		DisplayName              string   `json:"display_name"`
		ModelFamily              string   `json:"model_family"`
		MaxTokens                string   `json:"max_tokens"`
		ResponseMaxTokens        int      `json:"response_max_tokens"`
		DefaultResponseMaxTokens int      `json:"default_response_max_tokens"`
		ShortDescription         string   `json:"short_description"`
		Description              string   `json:"description"`
		SupportedParameters      []string `json:"supported_parameters"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&boeingModels); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Transform to OpenAI Model format
	models := make([]api.Model, 0, len(boeingModels))
	for _, m := range boeingModels {
		models = append(models, api.Model{
			ID:      m.ModelID,
			Object:  "model",
			Created: 1700000000,
			OwnedBy: "boeing-ai",
			Metadata: map[string]string{
				"usage":        "llm",
				"display_name": m.DisplayName,
				"model_family": m.ModelFamily,
				"model_type":   m.ModelType,
			},
		})
	}

	return models, nil
}

// fetchBoeingEmbeddingModels fetches embedding models from the Boeing Security API
func (s *Server) fetchBoeingEmbeddingModels(r *http.Request, apiKey string) ([]api.Model, error) {
	url := s.baseURL + "/bcai-public-security-api/embedding-models"

	req, err := http.NewRequestWithContext(r.Context(), "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "basic "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	// Parse Boeing embedding models response
	var embModels []struct {
		ModelID  string `json:"model_id"`
		Provider string `json:"provider"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&embModels); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	models := make([]api.Model, 0, len(embModels))
	for _, m := range embModels {
		models = append(models, api.Model{
			ID:      m.ModelID,
			Object:  "model",
			Created: 1700000000,
			OwnedBy: "boeing-ai",
			Metadata: map[string]string{
				"usage":    "text-embedding",
				"provider": m.Provider,
			},
		})
	}

	return models, nil
}
