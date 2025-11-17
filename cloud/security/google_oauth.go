package security

import (
	"context"
	"fmt"
	"log"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// GoogleServiceClient provides authenticated access to Google services
type GoogleServiceClient struct {
	tokenStore *TokenStore
}

// NewGoogleServiceClient creates a new Google service client
func NewGoogleServiceClient(tokenStore *TokenStore) *GoogleServiceClient {
	return &GoogleServiceClient{
		tokenStore: tokenStore,
	}
}

// GetGmailService returns an authenticated Gmail service for a user
func (g *GoogleServiceClient) GetGmailService(ctx context.Context, userID string) (*gmail.Service, error) {
	token, err := g.tokenStore.GetValidToken(ctx, ServiceGmail, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get valid Gmail token for user %s: %w", userID, err)
	}

	config, exists := g.tokenStore.oauthConfigs[ServiceGmail]
	if !exists {
		return nil, fmt.Errorf("Gmail OAuth config not found")
	}

	// Create authenticated HTTP client
	client := config.Client(ctx, token)

	// Create Gmail service
	service, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gmail service: %w", err)
	}

	return service, nil
}

// GetCalendarService returns an authenticated Calendar service for a user
func (g *GoogleServiceClient) GetCalendarService(ctx context.Context, userID string) (*calendar.Service, error) {
	token, err := g.tokenStore.GetValidToken(ctx, ServiceCalendar, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get valid Calendar token for user %s: %w", userID, err)
	}

	config, exists := g.tokenStore.oauthConfigs[ServiceCalendar]
	if !exists {
		return nil, fmt.Errorf("Calendar OAuth config not found")
	}

	// Create authenticated HTTP client
	client := config.Client(ctx, token)

	// Create Calendar service
	service, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("failed to create Calendar service: %w", err)
	}

	return service, nil
}

// ValidateGmailAccess checks if Gmail access is working for a user
func (g *GoogleServiceClient) ValidateGmailAccess(ctx context.Context, userID string) error {
	service, err := g.GetGmailService(ctx, userID)
	if err != nil {
		return err
	}

	// Test access by getting user profile
	_, err = service.Users.GetProfile("me").Do()
	if err != nil {
		return fmt.Errorf("Gmail access validation failed: %w", err)
	}

	log.Printf("Gmail access validated for user %s", userID)
	return nil
}

// ValidateCalendarAccess checks if Calendar access is working for a user
func (g *GoogleServiceClient) ValidateCalendarAccess(ctx context.Context, userID string) error {
	service, err := g.GetCalendarService(ctx, userID)
	if err != nil {
		return err
	}

	// Test access by getting calendar list
	_, err = service.CalendarList.List().MaxResults(1).Do()
	if err != nil {
		return fmt.Errorf("Calendar access validation failed: %w", err)
	}

	log.Printf("Calendar access validated for user %s", userID)
	return nil
}

// RevokeServiceAccess revokes access for a specific service
func (g *GoogleServiceClient) RevokeServiceAccess(ctx context.Context, service ServiceScope, userID string) error {
	// Delete stored token
	if err := g.tokenStore.DeleteToken(ctx, service, userID); err != nil {
		return fmt.Errorf("failed to delete token for service %s: %w", service, err)
	}

	log.Printf("Revoked access for user %s, service %s", userID, service)
	return nil
}

// Helper function to allow time mocking in tests
var Now = time.Now

// InitializeDefaultServices configures default Gmail and Calendar services
func (g *GoogleServiceClient) InitializeDefaultServices(gmailClientSecret, calendarClientID, calendarClientSecret, redirectURL string) {
	// Configure Gmail service with default client ID
	g.tokenStore.ConfigureService(ServiceGmail, DefaultGmailClientID, gmailClientSecret, redirectURL, GmailScopes)

	// Configure Calendar service (if provided)
	if calendarClientID != "" && calendarClientSecret != "" {
		g.tokenStore.ConfigureService(ServiceCalendar, calendarClientID, calendarClientSecret, redirectURL, CalendarScopes)
	}

	log.Printf("Initialized default Gmail OAuth with client ID: %s", DefaultGmailClientID)
	if calendarClientID != "" {
		log.Printf("Initialized Calendar OAuth with client ID: %s", calendarClientID)
	}
}

// InitializeGmailOnly configures only Gmail service with default credentials
func (g *GoogleServiceClient) InitializeGmailOnly(gmailClientSecret, redirectURL string) {
	g.tokenStore.ConfigureService(ServiceGmail, DefaultGmailClientID, gmailClientSecret, redirectURL, GmailScopes)
	log.Printf("Initialized Gmail OAuth with client ID: %s", DefaultGmailClientID)
}

// InitializeCalendarOnly configures only Calendar service with provided credentials
func (g *GoogleServiceClient) InitializeCalendarOnly(calendarClientID, calendarClientSecret, redirectURL string) {
	if calendarClientID == "" || calendarClientSecret == "" {
		log.Printf("Calendar OAuth credentials missing; skipping initialization")
		return
	}

	g.tokenStore.ConfigureService(ServiceCalendar, calendarClientID, calendarClientSecret, redirectURL, CalendarScopes)
	log.Printf("Initialized Calendar OAuth with client ID: %s", calendarClientID)
}

// GetAuthURL provides access to the token store's GetAuthURL method
func (g *GoogleServiceClient) GetAuthURL(ctx context.Context, service ServiceScope, userID string) (string, string, error) {
	return g.tokenStore.GetAuthURL(ctx, service, userID)
}

// ExchangeCodeForToken provides access to the token store's ExchangeCodeForToken method
func (g *GoogleServiceClient) ExchangeCodeForToken(ctx context.Context, service ServiceScope, userID, code, state string) (*oauth2.Token, error) {
	return g.tokenStore.ExchangeCodeForToken(ctx, service, userID, code, state)
}

// ResolveUserIDFromState exposes the token store lookup for OAuth callbacks
func (g *GoogleServiceClient) ResolveUserIDFromState(ctx context.Context, state string) (string, error) {
	return g.tokenStore.ResolveUserIDFromState(ctx, state)
}

// ResolveServiceFromState exposes the token store lookup for determining OAuth service
func (g *GoogleServiceClient) ResolveServiceFromState(ctx context.Context, userID, state string) (ServiceScope, error) {
	return g.tokenStore.ResolveServiceFromState(ctx, userID, state)
}

// GetServiceStatus returns the authentication status for all services
func (g *GoogleServiceClient) GetServiceStatus(ctx context.Context, userID string) map[ServiceScope]string {
	services, err := g.tokenStore.ListUserServices(ctx, userID)
	if err != nil {
		log.Printf("Failed to list user services: %v", err)
		return make(map[ServiceScope]string)
	}

	status := make(map[ServiceScope]string)
	for _, service := range services {
		token, err := g.tokenStore.GetValidToken(ctx, service, userID)
		if err != nil {
			status[service] = "error: " + err.Error()
			continue
		}

		if token.Expiry.Before(Now().Add(5 * time.Minute)) {
			status[service] = "expired"
		} else {
			status[service] = "valid"
		}
	}

	return status
}
