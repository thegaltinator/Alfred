package security

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
)

// ServiceScope defines the OAuth scopes for each service
type ServiceScope string

const (
	ServiceGmail    ServiceScope = "gmail"
	ServiceCalendar ServiceScope = "calendar"
)

// Scopes for each service
var (
	// Gmail scopes: read-only, compose drafts, modify labels, send emails
	GmailScopes = []string{
		gmail.GmailReadonlyScope,
		gmail.GmailComposeScope,
		gmail.GmailModifyScope,
		gmail.GmailSendScope,
	}

	// Calendar scopes: read-only, manage events
	CalendarScopes = []string{
		calendar.CalendarReadonlyScope,
		calendar.CalendarEventsScope,
	}

	// Default Gmail client credentials
	DefaultGmailClientID = "435511693699-h3g45bt07smpnvr9oul5pap771cbdjnl.apps.googleusercontent.com"
	// Note: Client secret should be provided via environment variable GMAIL_CLIENT_SECRET
)

// TokenInfo represents stored OAuth token information
type TokenInfo struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	TokenType    string       `json:"token_type"`
	Expiry       time.Time    `json:"expiry"`
	Service      ServiceScope `json:"service"`
	UserID       string       `json:"user_id"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// OAuthConfig represents OAuth configuration for a service
type OAuthConfig struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURL  string   `json:"redirect_url"`
	Scopes       []string `json:"scopes"`
}

// TokenStore manages OAuth tokens using Redis
type TokenStore struct {
	redisClient  *redis.Client
	oauthConfigs map[ServiceScope]*oauth2.Config
}

// NewTokenStore creates a new token store
func NewTokenStore(redisClient *redis.Client) *TokenStore {
	return &TokenStore{
		redisClient:  redisClient,
		oauthConfigs: make(map[ServiceScope]*oauth2.Config),
	}
}

// ConfigureService sets up OAuth configuration for a service
func (ts *TokenStore) ConfigureService(service ServiceScope, clientID, clientSecret, redirectURL string, scopes []string) {
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
		Endpoint:     google.Endpoint,
	}

	ts.oauthConfigs[service] = config
	log.Printf("Configured OAuth for service %s with %d scopes", service, len(scopes))
}

// GetAuthURL generates OAuth authorization URL
func (ts *TokenStore) GetAuthURL(ctx context.Context, service ServiceScope, userID string) (string, string, error) {
	config, exists := ts.oauthConfigs[service]
	if !exists {
		return "", "", fmt.Errorf("OAuth config not found for service: %s", service)
	}

	// Generate state parameter for CSRF protection
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate state: %w", err)
	}
	state := base64.URLEncoding.EncodeToString(stateBytes)

	// Store state in Redis temporarily with 10 minute expiry
	stateKey := fmt.Sprintf("oauth_state:%s:%s", userID, state)
	if err := ts.redisClient.Set(ctx, stateKey, string(service), 10*time.Minute).Err(); err != nil {
		return "", "", fmt.Errorf("failed to store OAuth state: %w", err)
	}

	stateUserKey := fmt.Sprintf("oauth_state_user:%s", state)
	if err := ts.redisClient.Set(ctx, stateUserKey, userID, 10*time.Minute).Err(); err != nil {
		ts.redisClient.Del(ctx, stateKey)
		return "", "", fmt.Errorf("failed to store OAuth state metadata: %w", err)
	}

	// Generate auth URL
	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	return authURL, state, nil
}

// ExchangeCodeForToken exchanges authorization code for tokens
func (ts *TokenStore) ExchangeCodeForToken(ctx context.Context, service ServiceScope, userID, code, state string) (*oauth2.Token, error) {
	stateUserKey := fmt.Sprintf("oauth_state_user:%s", state)
	if userID == "" {
		resolvedUserID, err := ts.ResolveUserIDFromState(ctx, state)
		if err != nil {
			return nil, err
		}
		userID = resolvedUserID
	}

	// Verify state parameter
	stateKey := fmt.Sprintf("oauth_state:%s:%s", userID, state)
	defer ts.redisClient.Del(ctx, stateKey, stateUserKey)

	storedService, err := ts.redisClient.Get(ctx, stateKey).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("invalid or expired state parameter")
	} else if err != nil {
		return nil, fmt.Errorf("failed to verify state: %w", err)
	}

	if ServiceScope(storedService) != service {
		return nil, fmt.Errorf("state parameter service mismatch")
	}

	// Get OAuth config
	config, exists := ts.oauthConfigs[service]
	if !exists {
		return nil, fmt.Errorf("OAuth config not found for service: %s", service)
	}

	// Exchange code for token
	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}

	// Store token
	if err := ts.StoreToken(ctx, service, userID, token); err != nil {
		return nil, fmt.Errorf("failed to store token: %w", err)
	}

	return token, nil
}

// ResolveUserIDFromState returns the user ID associated with an OAuth state token
func (ts *TokenStore) ResolveUserIDFromState(ctx context.Context, state string) (string, error) {
	stateUserKey := fmt.Sprintf("oauth_state_user:%s", state)
	userID, err := ts.redisClient.Get(ctx, stateUserKey).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("invalid or expired state parameter")
	} else if err != nil {
		return "", fmt.Errorf("failed to resolve OAuth state: %w", err)
	}
	return userID, nil
}

// ResolveServiceFromState returns the service scope associated with an OAuth state
func (ts *TokenStore) ResolveServiceFromState(ctx context.Context, userID, state string) (ServiceScope, error) {
	if userID == "" {
		return "", fmt.Errorf("userID is required to resolve service from state")
	}

	stateKey := fmt.Sprintf("oauth_state:%s:%s", userID, state)
	service, err := ts.redisClient.Get(ctx, stateKey).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("invalid or expired state parameter")
	} else if err != nil {
		return "", fmt.Errorf("failed to resolve OAuth state: %w", err)
	}

	return ServiceScope(service), nil
}

// StoreToken stores OAuth token information
func (ts *TokenStore) StoreToken(ctx context.Context, service ServiceScope, userID string, token *oauth2.Token) error {
	if token == nil {
		return fmt.Errorf("token cannot be nil")
	}

	tokenInfo := &TokenInfo{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
		Service:      service,
		UserID:       userID,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	tokenData, err := json.Marshal(tokenInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal token info: %w", err)
	}

	tokenKey := fmt.Sprintf("oauth_token:%s:%s", userID, service)

	// Store with 30 day expiry, will be refreshed on access
	if err := ts.redisClient.Set(ctx, tokenKey, tokenData, 30*24*time.Hour).Err(); err != nil {
		return fmt.Errorf("failed to store token in Redis: %w", err)
	}

	log.Printf("Stored OAuth token for user %s, service %s", userID, service)
	return nil
}

// GetToken retrieves OAuth token for a user and service
func (ts *TokenStore) GetToken(ctx context.Context, service ServiceScope, userID string) (*oauth2.Token, error) {
	tokenKey := fmt.Sprintf("oauth_token:%s:%s", userID, service)

	tokenData, err := ts.redisClient.Get(ctx, tokenKey).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("no token found for user %s, service %s", userID, service)
	} else if err != nil {
		return nil, fmt.Errorf("failed to retrieve token: %w", err)
	}

	var tokenInfo TokenInfo
	if err := json.Unmarshal([]byte(tokenData), &tokenInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token info: %w", err)
	}

	token := &oauth2.Token{
		AccessToken:  tokenInfo.AccessToken,
		RefreshToken: tokenInfo.RefreshToken,
		TokenType:    tokenInfo.TokenType,
		Expiry:       tokenInfo.Expiry,
	}

	return token, nil
}

// RefreshToken refreshes an expired OAuth token
func (ts *TokenStore) RefreshToken(ctx context.Context, service ServiceScope, userID string) (*oauth2.Token, error) {
	config, exists := ts.oauthConfigs[service]
	if !exists {
		return nil, fmt.Errorf("OAuth config not found for service: %s", service)
	}

	// Get current token
	currentToken, err := ts.GetToken(ctx, service, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current token: %w", err)
	}

	if currentToken.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available for user %s, service %s", userID, service)
	}

	// Force the cached token to be considered expired so the TokenSource actually refreshes.
	if currentToken.Expiry.After(time.Now()) {
		currentToken.Expiry = time.Now().Add(-1 * time.Minute)
	}

	// Refresh the token
	newToken, err := config.TokenSource(ctx, currentToken).Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	// Store the refreshed token
	if err := ts.StoreToken(ctx, service, userID, newToken); err != nil {
		return nil, fmt.Errorf("failed to store refreshed token: %w", err)
	}

	log.Printf("Refreshed OAuth token for user %s, service %s", userID, service)
	return newToken, nil
}

// GetValidToken returns a valid token, refreshing if necessary
func (ts *TokenStore) GetValidToken(ctx context.Context, service ServiceScope, userID string) (*oauth2.Token, error) {
	token, err := ts.GetToken(ctx, service, userID)
	if err != nil {
		return nil, err
	}

	// Check if token is expired or will expire within 5 minutes
	if token.Expiry.Before(time.Now().Add(5 * time.Minute)) {
		log.Printf("Token expired for user %s, service %s, refreshing...", userID, service)
		return ts.RefreshToken(ctx, service, userID)
	}

	return token, nil
}

// DeleteToken removes stored token for a user and service
func (ts *TokenStore) DeleteToken(ctx context.Context, service ServiceScope, userID string) error {
	tokenKey := fmt.Sprintf("oauth_token:%s:%s", userID, service)

	if err := ts.redisClient.Del(ctx, tokenKey).Err(); err != nil {
		return fmt.Errorf("failed to delete token: %w", err)
	}

	log.Printf("Deleted OAuth token for user %s, service %s", userID, service)
	return nil
}

// ListUserServices returns all services for which user has tokens
func (ts *TokenStore) ListUserServices(ctx context.Context, userID string) ([]ServiceScope, error) {
	pattern := fmt.Sprintf("oauth_token:%s:*", userID)
	keys, err := ts.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list user tokens: %w", err)
	}

	var services []ServiceScope
	for _, key := range keys {
		// Extract service from key format: oauth_token:{userID}:{service}
		parts := []rune(key)
		if len(parts) > 2 {
			serviceStr := string(parts[len(fmt.Sprintf("oauth_token:%s:", userID)):])
			services = append(services, ServiceScope(serviceStr))
		}
	}

	return services, nil
}
