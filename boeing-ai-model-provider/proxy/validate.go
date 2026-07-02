package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	oproxy "github.com/boeing-ai-gateway/providers/openai-model-provider/proxy"
)

// ValidateBoeingAI validates the Boeing AI provider configuration
// by calling the /bcai-public-security-api/authorized endpoint.
func ValidateBoeingAI(cfg *oproxy.Config, baseURL string) error {
	apiKey := cfg.APIKey
	if apiKey == "" {
		return handleError(nil, "UDAL Personal Access Token is required")
	}

	// Call the Boeing authorized endpoint to verify the PAT is valid
	url := baseURL + "/bcai-public-security-api/authorized"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return handleError(err, "")
	}

	req.Header.Set("Authorization", "basic "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Network error — service might be temporarily down, allow startup
		fmt.Printf("[Boeing AI] Warning: could not reach authorized endpoint: %v (proceeding anyway)\n", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return handleError(nil, "Invalid or expired UDAL Personal Access Token")
	}

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode >= 500 {
		// Service temporarily unavailable — don't block provider startup
		fmt.Printf("[Boeing AI] Warning: security API returned %d (service may be temporarily unavailable, proceeding anyway)\n", resp.StatusCode)
		return nil
	}

	// Also try to verify we can list models (best-effort)
	modelsURL := baseURL + "/bcai-public-security-api/models"
	modelsReq, err := http.NewRequest("GET", modelsURL, nil)
	if err != nil {
		return nil // Don't fail validation on this
	}

	modelsReq.Header.Set("Authorization", "basic "+apiKey)
	modelsReq.Header.Set("Accept", "application/json")

	modelsResp, err := http.DefaultClient.Do(modelsReq)
	if err != nil {
		fmt.Printf("[Boeing AI] Warning: could not fetch models: %v\n", err)
		return nil
	}
	defer modelsResp.Body.Close()

	if modelsResp.StatusCode == http.StatusUnauthorized || modelsResp.StatusCode == http.StatusForbidden {
		return handleError(nil, "PAT is not authorized to access models")
	}

	return nil
}

func handleError(err error, msg string) error {
	if err != nil {
		log.Printf("ERROR Boeing AI validation: %v", err)
	}
	if msg == "" && err != nil {
		msg = err.Error()
	}
	errorJSON := map[string]string{"error": msg}
	_ = json.NewEncoder(os.Stdout).Encode(errorJSON)
	return fmt.Errorf("%s", msg)
}
