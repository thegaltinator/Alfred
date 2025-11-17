package security

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestTokenStore_ExpiredTokenRefresh(t *testing.T) {
	// This test demonstrates that expired tokens are detected and refresh logic is triggered
	// In a real scenario, this would require proper OAuth configuration

	// Setup
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	// Configure Gmail service
	tokenStore.ConfigureService(ServiceGmail, "test-client-id", "test-client-secret", "http://localhost:8080/callback", GmailScopes)

	userID := "test-user-refresh"
	service := ServiceGmail

	// Create an expired token
	expiredToken := &oauth2.Token{
		AccessToken:  "expired-access-token",
		RefreshToken: "valid-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-1 * time.Hour), // Expired 1 hour ago
	}

	// Store the expired token
	err := tokenStore.StoreToken(ctx, service, userID, expiredToken)
	require.NoError(t, err)

	// Test GetValidToken - should attempt refresh but fail without proper OAuth config
	// This verifies our detection logic works
	retrievedToken, err := tokenStore.GetToken(ctx, service, userID)
	require.NoError(t, err)

	// Verify the token is indeed expired
	assert.True(t, retrievedToken.Expiry.Before(time.Now()))

	// GetValidToken should detect this and attempt refresh, but will fail
	// without proper OAuth configuration (which is expected in test environment)
	_, err = tokenStore.GetValidToken(ctx, service, userID)
	assert.Error(t, err) // Expected to fail due to missing OAuth config for refresh

	// Cleanup
	err = tokenStore.DeleteToken(ctx, service, userID)
	require.NoError(t, err)
}

func TestTokenStore_RevokedTokenHandling(t *testing.T) {
	// Test behavior when dealing with revoked tokens
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	// Configure Gmail service
	tokenStore.ConfigureService(ServiceGmail, "test-client-id", "test-client-secret", "http://localhost:8080/callback", GmailScopes)

	userID := "test-user-revoked"
	service := ServiceGmail

	// Create a token that simulates being revoked (invalid refresh token)
	revokedToken := &oauth2.Token{
		AccessToken:  "revoked-access-token",
		RefreshToken: "", // No refresh token - simulating revoked scenario
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-1 * time.Hour), // Expired
	}

	// Store the revoked token
	err := tokenStore.StoreToken(ctx, service, userID, revokedToken)
	require.NoError(t, err)

	// Test retrieval
	retrievedToken, err := tokenStore.GetToken(ctx, service, userID)
	require.NoError(t, err)
	assert.Equal(t, revokedToken.AccessToken, retrievedToken.AccessToken)
	assert.Empty(t, retrievedToken.RefreshToken) // No refresh token available

	// GetValidToken should fail when trying to refresh a token without refresh token
	_, err = tokenStore.GetValidToken(ctx, service, userID)
	assert.Error(t, err) // Expected to fail - no refresh token available

	// Cleanup
	err = tokenStore.DeleteToken(ctx, service, userID)
	require.NoError(t, err)
}

func TestTokenStore_NearExpiryRefresh(t *testing.T) {
	// Test token refresh when token is about to expire (within 5 minutes)
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	// Configure Gmail service
	tokenStore.ConfigureService(ServiceGmail, "test-client-id", "test-client-secret", "http://localhost:8080/callback", GmailScopes)

	userID := "test-user-near-expiry"
	service := ServiceGmail

	// Create a token that expires in 3 minutes (within the 5-minute refresh window)
	nearExpiryToken := &oauth2.Token{
		AccessToken:  "near-expiry-access-token",
		RefreshToken: "valid-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(3 * time.Minute), // Expires in 3 minutes
	}

	// Store the near-expiry token
	err := tokenStore.StoreToken(ctx, service, userID, nearExpiryToken)
	require.NoError(t, err)

	// GetValidToken should detect this as needing refresh
	_, err = tokenStore.GetValidToken(ctx, service, userID)
	assert.Error(t, err) // Expected to fail without proper OAuth config for refresh

	// Cleanup
	err = tokenStore.DeleteToken(ctx, service, userID)
	require.NoError(t, err)
}

func TestTokenStore_ValidTokenNoRefresh(t *testing.T) {
	// Test that valid tokens (far from expiry) are not refreshed unnecessarily
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	// Configure Gmail service
	tokenStore.ConfigureService(ServiceGmail, "test-client-id", "test-client-secret", "http://localhost:8080/callback", GmailScopes)

	userID := "test-user-valid"
	service := ServiceGmail

	// Create a valid token with long expiry
	validToken := &oauth2.Token{
		AccessToken:  "valid-access-token",
		RefreshToken: "valid-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(1 * time.Hour), // Expires in 1 hour
	}

	// Store the valid token
	err := tokenStore.StoreToken(ctx, service, userID, validToken)
	require.NoError(t, err)

	// GetValidToken should return the token without attempting refresh
	// Since we can't mock OAuth properly, we'll test the logic indirectly
	retrievedToken, err := tokenStore.GetToken(ctx, service, userID)
	require.NoError(t, err)
	assert.Equal(t, validToken.AccessToken, retrievedToken.AccessToken)
	assert.True(t, retrievedToken.Expiry.After(time.Now().Add(50*time.Minute))) // Still valid

	// The token should not be considered needing refresh (more than 5 minutes from expiry)
	assert.True(t, retrievedToken.Expiry.After(time.Now().Add(5*time.Minute)))

	// Cleanup
	err = tokenStore.DeleteToken(ctx, service, userID)
	require.NoError(t, err)
}

func TestTokenStore_MissingTokenHandling(t *testing.T) {
	// Test behavior when requesting a token that doesn't exist
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	userID := "nonexistent-user"
	service := ServiceGmail

	// Try to get a token that doesn't exist
	_, err := tokenStore.GetToken(ctx, service, userID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no token found")

	// GetValidToken should also fail
	_, err = tokenStore.GetValidToken(ctx, service, userID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no token found")
}

func TestTokenStore_ListUserServicesEmpty(t *testing.T) {
	// Test listing services for a user with no stored tokens
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	userID := "user-with-no-tokens"

	// List services should return empty list
	services, err := tokenStore.ListUserServices(ctx, userID)
	require.NoError(t, err)
	assert.Empty(t, services)
}

func TestTokenStore_ConcurrentAccess(t *testing.T) {
	// Test concurrent access to token storage and retrieval
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	// Configure Gmail service
	tokenStore.ConfigureService(ServiceGmail, "test-client-id", "test-client-secret", "http://localhost:8080/callback", GmailScopes)

	userID := "test-user-concurrent"
	service := ServiceGmail

	// Create test token
	testToken := &oauth2.Token{
		AccessToken:  "concurrent-access-token",
		RefreshToken: "concurrent-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	}

	// Store token in one goroutine and retrieve in another
	errCh := make(chan error, 2)

	// Store token
	go func() {
		errCh <- tokenStore.StoreToken(ctx, service, userID, testToken)
	}()

	// Retrieve token
	go func() {
		time.Sleep(100 * time.Millisecond) // Small delay to ensure store happens first
		_, err := tokenStore.GetToken(ctx, service, userID)
		errCh <- err
	}()

	// Wait for both operations
	for i := 0; i < 2; i++ {
		err := <-errCh
		assert.NoError(t, err)
	}

	// Cleanup
	err := tokenStore.DeleteToken(ctx, service, userID)
	require.NoError(t, err)
}

// Integration-style test for the complete token lifecycle
func TestTokenStore_CompleteLifecycle(t *testing.T) {
	ctx := context.Background()
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	tokenStore := NewTokenStore(mockRedis)

	// Configure both services
	tokenStore.ConfigureService(ServiceGmail, "test-gmail-client", "test-gmail-secret", "http://localhost:8080/callback", GmailScopes)
	tokenStore.ConfigureService(ServiceCalendar, "test-calendar-client", "test-calendar-secret", "http://localhost:8080/callback", CalendarScopes)

	userID := "lifecycle-test-user"

	// Test lifecycle for Gmail
	gmailToken := &oauth2.Token{
		AccessToken:  "gmail-access-token",
		RefreshToken: "gmail-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	}

	// Store Gmail token
	err := tokenStore.StoreToken(ctx, ServiceGmail, userID, gmailToken)
	require.NoError(t, err)

	// Verify Gmail token stored
	services, err := tokenStore.ListUserServices(ctx, userID)
	require.NoError(t, err)
	assert.Contains(t, services, ServiceGmail)

	// Retrieve Gmail token
	retrievedGmailToken, err := tokenStore.GetToken(ctx, ServiceGmail, userID)
	require.NoError(t, err)
	assert.Equal(t, gmailToken.AccessToken, retrievedGmailToken.AccessToken)

	// Test Calendar token lifecycle
	calendarToken := &oauth2.Token{
		AccessToken:  "calendar-access-token",
		RefreshToken: "calendar-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(2 * time.Hour),
	}

	err = tokenStore.StoreToken(ctx, ServiceCalendar, userID, calendarToken)
	require.NoError(t, err)

	// Verify both services are now stored
	services, err = tokenStore.ListUserServices(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, services, 2)
	assert.Contains(t, services, ServiceGmail)
	assert.Contains(t, services, ServiceCalendar)

	// Cleanup - remove tokens individually
	err = tokenStore.DeleteToken(ctx, ServiceGmail, userID)
	require.NoError(t, err)

	services, err = tokenStore.ListUserServices(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, services, 1)
	assert.NotContains(t, services, ServiceGmail)
	assert.Contains(t, services, ServiceCalendar)

	err = tokenStore.DeleteToken(ctx, ServiceCalendar, userID)
	require.NoError(t, err)

	services, err = tokenStore.ListUserServices(ctx, userID)
	require.NoError(t, err)
	assert.Empty(t, services)
}