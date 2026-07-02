package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	oauth2proxy "github.com/oauth2-proxy/oauth2-proxy/v7"
	"github.com/oauth2-proxy/oauth2-proxy/v7/pkg/apis/options"
	"github.com/oauth2-proxy/oauth2-proxy/v7/pkg/validation"
	"github.com/boeing-ai-gateway/providers/auth-providers-common/pkg/env"
	"github.com/boeing-ai-gateway/providers/auth-providers-common/pkg/state"
	"github.com/boeing-ai-gateway/providers/gitlab-auth-provider/pkg/profile"
)

type Options struct {
	ClientID                 string  `env:"BOEING_GITLAB_AUTH_PROVIDER_CLIENT_ID"`
	ClientSecret             string  `env:"BOEING_GITLAB_AUTH_PROVIDER_CLIENT_SECRET"`
	BoeingServerURL            string  `env:"BOEING_SERVER_PUBLIC_URL,BOEING_SERVER_URL"`
	PostgresConnectionDSN    string  `env:"BOEING_AUTH_PROVIDER_POSTGRES_CONNECTION_DSN" optional:"true"`
	AuthCookieSecret         string  `usage:"Secret used to encrypt cookie" env:"BOEING_AUTH_PROVIDER_COOKIE_SECRET"`
	AuthEmailDomains         string  `usage:"Email domains allowed for authentication" default:"*" env:"BOEING_AUTH_PROVIDER_EMAIL_DOMAINS"`
	AuthTokenRefreshDuration string  `usage:"Duration to refresh auth token after" optional:"true" default:"1h" env:"BOEING_AUTH_PROVIDER_TOKEN_REFRESH_DURATION"`
	LoggingEnabled           string  `usage:"Enable oauth2-proxy logging" optional:"true" env:"BOEING_AUTH_PROVIDER_ENABLE_LOGGING"`
	GitLabURL                *string `usage:"URL of the GitLab instance (leave empty for gitlab.com)" optional:"true" env:"BOEING_GITLAB_AUTH_PROVIDER_URL"`
	GitLabGroup              *string `usage:"restrict logins to members of this GitLab group" optional:"true" env:"BOEING_GITLAB_AUTH_PROVIDER_GROUP"`
}

func main() {
	var opts Options
	if err := env.LoadEnvForStruct(&opts); err != nil {
		fmt.Printf("ERROR: gitlab-auth-provider: failed to load options: %v\n", err)
		os.Exit(1)
	}

	refreshDuration, err := time.ParseDuration(opts.AuthTokenRefreshDuration)
	if err != nil {
		fmt.Printf("ERROR: gitlab-auth-provider: failed to parse token refresh duration: %v\n", err)
		os.Exit(1)
	}

	if refreshDuration < 0 {
		fmt.Printf("ERROR: gitlab-auth-provider: token refresh duration must be greater than 0\n")
		os.Exit(1)
	}

	cookieSecret, err := base64.StdEncoding.DecodeString(opts.AuthCookieSecret)
	if err != nil {
		fmt.Printf("ERROR: gitlab-auth-provider: failed to decode cookie secret: %v\n", err)
		os.Exit(1)
	}

	legacyOpts := options.NewLegacyOptions()
	legacyOpts.LegacyProvider.ProviderType = "gitlab"
	legacyOpts.LegacyProvider.ProviderName = "gitlab"
	legacyOpts.LegacyProvider.Scope = "read_user openid email profile"
	legacyOpts.LegacyProvider.ClientID = opts.ClientID
	legacyOpts.LegacyProvider.ClientSecret = opts.ClientSecret

	// GitLab-specific options
	gitlabURL := "https://gitlab.com"
	if opts.GitLabURL != nil && *opts.GitLabURL != "" {
		gitlabURL = strings.TrimRight(*opts.GitLabURL, "/")
		legacyOpts.LegacyProvider.LoginURL = gitlabURL + "/oauth/authorize"
		legacyOpts.LegacyProvider.RedeemURL = gitlabURL + "/oauth/token"
		legacyOpts.LegacyProvider.ValidateURL = gitlabURL + "/api/v4/user"
		legacyOpts.LegacyProvider.OIDCIssuerURL = gitlabURL
	} else {
		legacyOpts.LegacyProvider.OIDCIssuerURL = "https://gitlab.com"
	}

	// Allow unverified emails (common with self-hosted GitLab)
	legacyOpts.LegacyProvider.InsecureOIDCAllowUnverifiedEmail = true
	legacyOpts.LegacyProvider.InsecureOIDCSkipIssuerVerification = true

	if opts.GitLabGroup != nil && *opts.GitLabGroup != "" {
		legacyOpts.LegacyProvider.AllowedGroups = []string{*opts.GitLabGroup}
	}

	oauthProxyOpts, err := legacyOpts.ToOptions()
	if err != nil {
		fmt.Printf("ERROR: gitlab-auth-provider: failed to convert legacy options to new options: %v\n", err)
		os.Exit(1)
	}

	oauthProxyOpts.Server.BindAddress = ""
	oauthProxyOpts.MetricsServer.BindAddress = ""
	if opts.PostgresConnectionDSN != "" {
		oauthProxyOpts.Session.Type = options.PostgresSessionStoreType
		oauthProxyOpts.Session.Postgres.ConnectionDSN = opts.PostgresConnectionDSN
		oauthProxyOpts.Session.Postgres.TableNamePrefix = "gitlab_"
	}
	oauthProxyOpts.Cookie.Refresh = refreshDuration
	oauthProxyOpts.Cookie.Name = "boeing_access_token"
	oauthProxyOpts.Cookie.Secret = string(cookieSecret)
	oauthProxyOpts.Cookie.Secure = strings.HasPrefix(opts.BoeingServerURL, "https://")
	oauthProxyOpts.Cookie.CSRFExpire = 30 * time.Minute
	oauthProxyOpts.Cookie.CSRFPerRequest = true
	oauthProxyOpts.RawRedirectURL = opts.BoeingServerURL + "/oauth2/callback"
	if opts.AuthEmailDomains != "" {
		emailDomains := strings.Split(opts.AuthEmailDomains, ",")
		for i := range emailDomains {
			emailDomains[i] = strings.TrimSpace(emailDomains[i])
		}
		oauthProxyOpts.EmailDomains = emailDomains
	}

	loggingEnabled := strings.EqualFold(opts.LoggingEnabled, "true")
	oauthProxyOpts.Logging.RequestEnabled = loggingEnabled
	oauthProxyOpts.Logging.AuthEnabled = loggingEnabled
	oauthProxyOpts.Logging.StandardEnabled = loggingEnabled

	if err = validation.Validate(oauthProxyOpts); err != nil {
		fmt.Printf("ERROR: gitlab-auth-provider: failed to validate options: %v\n", err)
		os.Exit(1)
	}

	oauthProxy, err := oauth2proxy.NewOAuthProxy(oauthProxyOpts, oauth2proxy.NewValidator(oauthProxyOpts.EmailDomains, oauthProxyOpts.AuthenticatedEmailsFile))
	if err != nil {
		fmt.Printf("ERROR: gitlab-auth-provider: failed to create oauth2 proxy: %v\n", err)
		os.Exit(1)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "9999"
	}

	listenHost := os.Getenv("BOEING_PROVIDER_LISTEN_HOST")
	if listenHost == "" {
		listenHost = "127.0.0.1"
	}
	addr := net.JoinHostPort(listenHost, port)

	mux := http.NewServeMux()
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Write(fmt.Appendf(nil, "http://%s", addr))
	})
	mux.HandleFunc("/boeing-get-state", getState(oauthProxy, gitlabURL))
	mux.HandleFunc("/boeing-get-user-info", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		// Strip "Bearer " or "token " prefix if present
		token = strings.TrimPrefix(token, "Bearer ")
		token = strings.TrimPrefix(token, "token ")

		userInfo, err := profile.FetchUserProfile(r.Context(), token, gitlabURL)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to fetch user info: %v", err), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(userInfo)
	})
	mux.HandleFunc("/boeing-list-user-auth-groups", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/", oauthProxy.ServeHTTP)

	fmt.Printf("listening on %s\n", addr)
	if err := http.ListenAndServe(addr, mux); !errors.Is(err, http.ErrServerClosed) {
		fmt.Printf("ERROR: gitlab-auth-provider: failed to listen and serve: %v\n", err)
		os.Exit(1)
	}
}

func getState(p *oauth2proxy.OAuthProxy, gitlabURL string) http.HandlerFunc {
	type profileKey struct {
		username string
		email    string
	}
	profileCache := expirable.NewLRU[profileKey, string](5000, nil, time.Hour)

	return func(w http.ResponseWriter, r *http.Request) {
		var sr state.SerializableRequest
		if err := json.NewDecoder(r.Body).Decode(&sr); err != nil {
			http.Error(w, fmt.Sprintf("failed to decode request body: %v", err), http.StatusBadRequest)
			return
		}

		reqObj, err := http.NewRequest(sr.Method, sr.URL, nil)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to create request object: %v", err), http.StatusBadRequest)
			return
		}

		reqObj.Header = sr.Header

		ss, err := state.GetSerializableState(p, reqObj)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to get state: %v", err), http.StatusInternalServerError)
			fmt.Printf("ERROR: gitlab-auth-provider: failed to get state: %v\n", err)
			return
		}

		// For GitLab, the User field from oauth2-proxy is the username.
		// We want the numeric user ID instead.
		ss.PreferredUsername = ss.User

		key := profileKey{
			username: ss.PreferredUsername,
			email:    ss.Email,
		}
		if userID, ok := profileCache.Get(key); ok {
			ss.User = userID
		} else {
			userProfile, err := profile.FetchUserProfile(r.Context(), ss.AccessToken, gitlabURL)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to get user info: %v", err), http.StatusInternalServerError)
				fmt.Printf("ERROR: gitlab-auth-provider: failed to get user info: %v\n", err)
				return
			}
			ss.User = fmt.Sprintf("%d", userProfile.ID)
			profileCache.Add(key, ss.User)
		}

		if err := json.NewEncoder(w).Encode(ss); err != nil {
			http.Error(w, fmt.Sprintf("failed to encode state: %v", err), http.StatusInternalServerError)
			fmt.Printf("ERROR: gitlab-auth-provider: failed to encode state: %v\n", err)
			return
		}
	}
}
