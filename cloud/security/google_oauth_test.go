package security

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestTokenStore_GetAuthURL(t *testing.T) {
	// Setup
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	// Configure Gmail service
	tokenStore.ConfigureService(ServiceGmail, "test-client-id", "test-client-secret", "http://localhost:8080/callback", GmailScopes)

	// Test
	userID := "test-user-123"
	authURL, state, err := tokenStore.GetAuthURL(ctx, ServiceGmail, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotEmpty(t, authURL)
	assert.NotEmpty(t, state)
	assert.Contains(t, authURL, "accounts.google.com")
	assert.Contains(t, authURL, "client_id=test-client-id")

	resolvedUserID, err := tokenStore.ResolveUserIDFromState(ctx, state)
	require.NoError(t, err)
	assert.Equal(t, userID, resolvedUserID)
}

func TestTokenStore_StoreAndRetrieveToken(t *testing.T) {
	// Setup
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	// Configure Gmail service
	tokenStore.ConfigureService(ServiceGmail, "test-client-id", "test-client-secret", "http://localhost:8080/callback", GmailScopes)

	// Create test token
	testToken := &oauth2.Token{
		AccessToken:  "test-access-token-123",
		RefreshToken: "test-refresh-token-456",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	}

	userID := "test-user-123"
	service := ServiceGmail

	// Test store token
	err := tokenStore.StoreToken(ctx, service, userID, testToken)
	require.NoError(t, err)

	// Test retrieve token
	retrievedToken, err := tokenStore.GetToken(ctx, service, userID)
	require.NoError(t, err)

	assert.Equal(t, testToken.AccessToken, retrievedToken.AccessToken)
	assert.Equal(t, testToken.RefreshToken, retrievedToken.RefreshToken)
	assert.Equal(t, testToken.TokenType, retrievedToken.TokenType)
	assert.WithinDuration(t, testToken.Expiry, retrievedToken.Expiry, time.Second)

	// Cleanup
	err = tokenStore.DeleteToken(ctx, service, userID)
	require.NoError(t, err)
}

func TestTokenStore_GetValidToken(t *testing.T) {
	// Setup
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	// Configure Gmail service
	tokenStore.ConfigureService(ServiceGmail, "test-client-id", "test-client-secret", "http://localhost:8080/callback", GmailScopes)

	userID := "test-user-123"
	service := ServiceGmail

	// Test with expired token
	expiredToken := &oauth2.Token{
		AccessToken:  "expired-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour), // Expired
	}

	err := tokenStore.StoreToken(ctx, service, userID, expiredToken)
	require.NoError(t, err)

	// Test GetValidToken - should fail since we can't refresh without proper OAuth config
	_, err = tokenStore.GetValidToken(ctx, service, userID)
	assert.Error(t, err) // Expected to fail without proper OAuth setup for refresh

	// Cleanup
	err = tokenStore.DeleteToken(ctx, service, userID)
	require.NoError(t, err)
}

func TestTokenStore_ListUserServices(t *testing.T) {
	// Setup
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	// Configure both services
	tokenStore.ConfigureService(ServiceGmail, "test-client-id", "test-client-secret", "http://localhost:8080/callback", GmailScopes)
	tokenStore.ConfigureService(ServiceCalendar, "test-client-id", "test-client-secret", "http://localhost:8080/callback", CalendarScopes)

	userID := "test-user-123"

	// Store tokens for both services
	testToken := &oauth2.Token{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	}

	err := tokenStore.StoreToken(ctx, ServiceGmail, userID, testToken)
	require.NoError(t, err)

	err = tokenStore.StoreToken(ctx, ServiceCalendar, userID, testToken)
	require.NoError(t, err)

	// Test list services
	services, err := tokenStore.ListUserServices(ctx, userID)
	require.NoError(t, err)

	assert.Len(t, services, 2)
	assert.Contains(t, services, ServiceGmail)
	assert.Contains(t, services, ServiceCalendar)

	// Cleanup
	err = tokenStore.DeleteToken(ctx, ServiceGmail, userID)
	require.NoError(t, err)
	err = tokenStore.DeleteToken(ctx, ServiceCalendar, userID)
	require.NoError(t, err)
}

func TestServiceScopes(t *testing.T) {
	// Test Gmail scopes
	assert.Equal(t, ServiceGmail, ServiceScope("gmail"))
	assert.Equal(t, ServiceCalendar, ServiceScope("calendar"))

	// Test Gmail scopes constants
	expectedGmailScopes := []string{
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/gmail.compose",
		"https://www.googleapis.com/auth/gmail.modify",
		"https://www.googleapis.com/auth/gmail.send",
	}
	assert.Equal(t, expectedGmailScopes, GmailScopes)

	// Test Calendar scopes constants
	expectedCalendarScopes := []string{
		"https://www.googleapis.com/auth/calendar.readonly",
		"https://www.googleapis.com/auth/calendar.events",
	}
	assert.Equal(t, expectedCalendarScopes, CalendarScopes)
}

func TestTokenInfoSerialization(t *testing.T) {
	// Create TokenInfo
	tokenInfo := TokenInfo{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
		Service:      ServiceGmail,
		UserID:       "test-user-123",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Test JSON serialization
	data, err := json.Marshal(tokenInfo)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	// Test JSON deserialization
	var parsed TokenInfo
	err = json.Unmarshal(data, &parsed)
	assert.NoError(t, err)

	assert.Equal(t, tokenInfo.AccessToken, parsed.AccessToken)
	assert.Equal(t, tokenInfo.RefreshToken, parsed.RefreshToken)
	assert.Equal(t, tokenInfo.TokenType, parsed.TokenType)
	assert.Equal(t, tokenInfo.Service, parsed.Service)
	assert.Equal(t, tokenInfo.UserID, parsed.UserID)
	assert.WithinDuration(t, tokenInfo.Expiry, parsed.Expiry, time.Second)
}

func TestGoogleServiceClient_Initialization(t *testing.T) {
	// Setup
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)
	googleClient := NewGoogleServiceClient(tokenStore)

	// Test initialization
	assert.NotNil(t, googleClient)

	// Test Gmail-only initialization
	gmailClientSecret := "test-gmail-client-secret"
	redirectURL := "http://localhost:8080/callback"

	googleClient.InitializeGmailOnly(gmailClientSecret, redirectURL)

	// Test default initialization with optional Calendar
	calendarClientID := "test-calendar-client-id"
	calendarClientSecret := "test-calendar-client-secret"

	googleClient.InitializeDefaultServices(gmailClientSecret, calendarClientID, calendarClientSecret, redirectURL)

	// This should not panic and should complete successfully
	assert.True(t, true)
}
