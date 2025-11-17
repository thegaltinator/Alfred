# Gmail OAuth Implementation

This document describes the Gmail OAuth implementation for Alfred, configured with your specific client ID.

## 🚀 Quick Start

### 1. Environment Setup

Create a `.env` file in the `cloud/` directory:

```bash
# Copy the example template
cp .env.example .env

# Edit .env with your credentials
```

**Required Environment Variables:**
```bash
GMAIL_CLIENT_SECRET=your_gmail_client_secret_here
OAUTH_REDIRECT_URL=http://localhost:8080/auth/google/callback
REDIS_URL=redis://localhost:6379
```

**Optional Environment Variables:**
```bash
CALENDAR_CLIENT_ID=your_calendar_client_id_here
CALENDAR_CLIENT_SECRET=your_calendar_client_secret_here
PORT=8080
HOST=localhost
```

### 2. Gmail OAuth Configuration

The system is pre-configured with your Gmail client ID:
```
Client ID: 435511693699-h3g45bt07smpnvr9oul5pap771cbdjnl.apps.googleusercontent.com
```

### 3. OAuth Scopes

The implementation includes the following Gmail scopes:
- `gmail.readonly` - Read emails
- `gmail.compose` - Create drafts
- `gmail.modify` - Modify labels
- `gmail.send` - Send emails

### 4. Build and Run

```bash
# Build the main application
go build -o alfred-cloud .

# Run the server
./alffred-cloud
```

## 📡 API Endpoints

### Authentication Flow

**1. Initiate OAuth**
```bash
POST /auth/google
Content-Type: application/json

{
  "user_id": "user123",
  "service": "gmail"
}
```

**Response:**
```json
{
  "auth_url": "https://accounts.google.com/oauth/authorize?...",
  "state": "random_state_string"
}
```

**2. OAuth Callback** (handled automatically by Google redirect)

**3. Check Status**
```bash
GET /auth/status?user_id=user123
```

**Response:**
```json
{
  "user_id": "user123",
  "services": {
    "gmail": "valid",
    "calendar": "not_authenticated"
  }
}
```

## 🔄 Email Poller

The Gmail poller runs every 30 seconds and:

1. **Fetches new emails** from authenticated Gmail accounts
2. **Classifies emails** into categories:
   - Question (requires response)
   - Action Required (urgent)
   - FYI (informational)
   - Information (general)
3. **Generates summaries** and draft replies
4. **Emits to Redis stream** `user:{id}:in:email`

### Email Message Format

```json
{
  "id": "message_id",
  "thread_id": "thread_id",
  "subject": "Email Subject",
  "from": "sender@example.com",
  "to": ["recipient@example.com"],
  "date": "2024-01-01T12:00:00Z",
  "snippet": "Email preview...",
  "body_text": "Full email body...",
  "requires_response": true,
  "summary": "Brief summary of email content",
  "draft_reply": "Thank you for your message. I'll review and respond shortly.",
  "classification": "Question",
  "priority": "Medium",
  "timestamp": "2024-01-01T12:30:00Z",
  "user_id": "user123"
}
```

## 🔧 Client Integration

### Swift Client Usage

```swift
import Foundation

// Initialize Gmail auth client
let gmailClient = GmailAuthClient(baseURL: URL(string: "http://localhost:8080")!)

// Authenticate Gmail
let (authURL, state) = try await gmailClient.authenticateGmail(userID: "user123")

// Open authURL in browser for user to complete OAuth

// Check status
let status = try await gmailClient.checkStatus(userID: "user123")
print("Gmail status: \(status.services["gmail"] ?? "unknown")")

// Validate service
let (isValid, error) = try await gmailClient.isGmailAuthenticated(userID: "user123")
```

## 🛠️ Testing

### Run Tests

```bash
# Run all security tests
go test ./security/ -v

# Run specific test suites
go test ./security/ -v -run TestTokenStore
go test ./security/ -v -run TestGoogleServiceClient

# Run email poller tests
go test ./subagents/email_triage/ -v
```

### Test Coverage

- ✅ OAuth token storage and retrieval
- ✅ Token refresh logic
- ✅ Revoked token handling
- ✅ Concurrent access patterns
- ✅ Email poller functionality
- ✅ Swift client integration

## 🔒 Security Features

1. **Server-side Token Storage** - All OAuth tokens stored securely in Redis
2. **CSRF Protection** - State parameters prevent CSRF attacks
3. **Token Expiration** - Automatic token refresh with 5-minute buffer
4. **Scope Separation** - Minimal scopes per service (principle of least privilege)
5. **Secure Defaults** - Client ID hardcoded, secrets via environment variables

## 📊 Redis Streams

### Input Streams
- `user:{id}:in:email` - New email messages
- `user:{id}:in:calendar` - Calendar changes
- `user:{id}:in:prod` - Productivity heartbeats

### Output Stream
- `user:{id}:wb` - Whiteboard outputs (read-only for client)

## 🚨 Error Handling

### Common Errors

**1. "GMAIL_CLIENT_SECRET environment variable is required"**
```bash
export GMAIL_CLIENT_SECRET=your_actual_client_secret
```

**2. Redis Connection Failed**
```bash
# Make sure Redis is running
redis-cli ping

# Check Redis configuration
export REDIS_URL=redis://localhost:6379
```

**3. OAuth State Parameter Invalid**
- State expires after 10 minutes
- Generate new auth URL for each authentication attempt

**4. Token Refresh Failed**
- Check if OAuth app is still authorized
- User may need to re-authenticate

## 📝 Development Notes

### Architecture

1. **Security Module** (`security/`)
   - Token storage and management
   - OAuth configuration
   - Google service clients

2. **API Layer** (`routes_google_auth.go`)
   - HTTP endpoints for OAuth flow
   - Status checking and validation

3. **Email Triage** (`subagents/email_triage/`)
   - Gmail poller with 30-second intervals
   - Email classification and summarization
   - Redis stream emission

4. **Client Bridge** (`client/Bridge/GmailAuthClient.swift`)
   - Swift client for OAuth initiation
   - Status checking and validation
   - Mock support for testing

### Token Lifecycle

1. **Initiate OAuth** → Store state → Redirect to Google
2. **Handle Callback** → Exchange code → Store tokens
3. **Regular Use** → Check expiry → Refresh if needed
4. **Error Handling** → Re-authenticate on revocation

## 🔄 Next Steps

1. **Configure OAuth App** in Google Cloud Console
2. **Set Environment Variables** with client secret
3. **Test Authentication Flow** with your Gmail account
4. **Integrate Email Poller** into main application
5. **Configure Calendar OAuth** (optional) for calendar functionality

## 📞 Support

For issues with the Gmail OAuth implementation:

1. Check environment variable configuration
2. Verify Google Cloud Console OAuth settings
3. Check Redis connection and permissions
4. Review application logs for detailed error messages