package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DefaultGitLabBaseURL is the default GitLab API base URL.
var DefaultGitLabBaseURL = "https://gitlab.com"

type GitLabUserProfile struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
	WebURL    string `json:"web_url"`
	State     string `json:"state"`
	IsAdmin   bool   `json:"is_admin"`
}

func FetchUserProfile(ctx context.Context, accessToken, gitlabURL string) (*GitLabUserProfile, error) {
	if gitlabURL == "" {
		gitlabURL = DefaultGitLabBaseURL
	}

	apiURL := fmt.Sprintf("%s/api/v4/user", gitlabURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d: %s", resp.StatusCode, body)
	}

	var profile GitLabUserProfile
	if err = json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}

	return &profile, nil
}
