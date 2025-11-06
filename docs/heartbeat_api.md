# Heartbeat API Documentation

## Overview

The Heartbeat API receives real-time productivity data from Alfred client applications and writes it to Redis streams for processing by the productivity subagent. This is a core component of Alfred's hybrid productivity heuristic system.

## Endpoint

**POST** `/prod/heartbeat`

## Request Format

### Headers

- `Content-Type: application/json` (required)
- `User-Agent: Alfred-Client/<version>` (recommended)

### Body

```json
{
  "bundle_id": "com.apple.Safari",
  "window_title": "Alfred Documentation",
  "url": "https://docs.alfred.ai",
  "activity_id": "com.apple.Safari#Alfred Documentation#docs.alfred.ai",
  "ts": "2025-01-06T12:00:00Z"
}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bundle_id` | string | **Yes** | Application bundle identifier (e.g., `com.apple.Safari`) |
| `window_title` | string | No | Current window/tab title |
| `url` | string | No | Current URL (if applicable) |
| `activity_id` | No | string | Client-generated activity identifier for grouping related heartbeats |
| `ts` | string | No | ISO 8601 timestamp. If omitted, server generates timestamp |

## Response Format

### Success (200 OK)

```json
{
  "ok": true,
  "stream": "user:dev:test:in:prod",
  "entry_id": "1762400317317-0",
  "correlation": "a1b2c3d4",
  "processed_at": "2025-01-06T12:00:01Z",
  "metrics": {
    "processed": 1234,
    "errors": 2,
    "last_process": "2025-01-06T11:59:58Z",
    "last_error": "2025-01-06T11:45:12Z"
  }
}
```

### Error Responses

#### 400 Bad Request - Invalid JSON
```json
"invalid JSON body"
```

#### 400 Bad Request - Missing Required Field
```json
"bundle_id required"
```

#### 500 Internal Server Error - Redis Failure
```json
"failed to enqueue heartbeat"
```

## Response Headers

- `Content-Type: application/json`
- `X-Correlation-ID: <correlation-id>` - Tracing identifier for the request

## Stream Format

Each heartbeat is written to a Redis stream with the following structure:

**Stream Key**: `user:{user_id}:in:productivity`

**Stream Entry Fields**:
- `bundle_id`: Application bundle identifier
- `window_title`: Window/tab title (may be empty)
- `url`: Current URL (may be empty)
- `activity_id`: Activity identifier (may be empty)
- `ts`: ISO 8601 timestamp
- `correlation`: Server correlation ID for tracing
- `client_ip`: Client IP address
- `user_agent`: Client User-Agent header

## Usage Examples

### Basic Heartbeat

```bash
curl -X POST http://localhost:8000/prod/heartbeat \
  -H "Content-Type: application/json" \
  -d '{
    "bundle_id": "com.apple.Safari",
    "window_title": "GitHub Repository",
    "url": "https://github.com/alfred/alfred"
  }'
```

### Heartbeat with Custom Timestamp

```bash
curl -X POST http://localhost:8000/prod/heartbeat \
  -H "Content-Type: application/json" \
  -d '{
    "bundle_id": "com.apple.Xcode",
    "window_title": "AppDelegate.swift",
    "activity_id": "com.apple.Xcode#AppDelegate.swift",
    "ts": "2025-01-06T12:00:00Z"
  }'
```

### Minimal Heartbeat (only required field)

```bash
curl -X POST http://localhost:8000/prod/heartbeat \
  -H "Content-Type: application/json" \
  -d '{"bundle_id": "com.example.App"}'
```

## Testing

### Local Development Setup

1. Start Redis server:
```bash
redis-server
```

2. Start the Alfred cloud server:
```bash
make cloud-dev
```

3. Send test heartbeats:
```bash
# Test successful heartbeat
curl -X POST http://localhost:8000/prod/heartbeat \
  -H "Content-Type: application/json" \
  -d '{"bundle_id": "com.test.App", "window_title": "Test Window"}'

# Test validation error
curl -X POST http://localhost:8000/prod/heartbeat \
  -H "Content-Type: application/json" \
  -d '{"window_title": "Missing bundle_id"}'

# Test invalid JSON
curl -X POST http://localhost:8000/prod/heartbeat \
  -H "Content-Type: application/json" \
  -d 'invalid json'
```

### Verify Stream Contents

```bash
# Check stream length
redis-cli XLEN user:dev:test:in:productivity

# View latest entries
redis-cli XRANGE user:dev:test:in:productivity - + COUNT 5

# Monitor live stream entries
redis-cli XREAD GROUP $ group STREAMS user:dev:test:in:productivity >
```

### Run Test Suite

```bash
# Run heartbeat tests
cd cloud
go test ./api -v -run TestHeartbeat

# Run all API tests
go test ./api -v

# Run with coverage
go test ./api -v -cover
```

## Performance Characteristics

- **Timeout**: 5 seconds for Redis operations
- **Rate Limit**: None currently (client controls heartbeat frequency)
- **Processing Time**: Typically <10ms for Redis XADD operations
- **Concurrent Support**: High (Redis streams handle concurrent writes efficiently)

## Monitoring

### Logs

Each request is logged with a correlation ID for tracing:

```
[heartbeat:a1b2c3d4] Incoming request from 127.0.0.1:54321
[heartbeat:a1b2c3d4] Success: bundle=com.apple.Safari stream=user:dev:test:in:prod entry_id=1762400317317-0 duration=8ms
```

### Metrics

The response includes real-time metrics:
- `processed`: Total successful heartbeats processed
- `errors`: Total errors encountered
- `last_process`: Timestamp of last successful processing
- `last_error`: Timestamp of last error

## Error Handling

### Client-Side Errors (4xx)
- Invalid JSON body → Log error, return 400
- Missing required fields → Log validation error, return 400

### Server-Side Errors (5xx)
- Redis connection failure → Log error, return 500
- Redis XADD timeout → Log error, return 500
- Stream write failure → Log error, return 500

### Recovery

- Redis operations include 5-second timeouts
- Failed requests are logged with correlation IDs
- Metrics track error rates for monitoring
- Client should retry on 5xx errors with exponential backoff

## Security Considerations

- No authentication required (development mode)
- Production should add API key or JWT authentication
- Rate limiting recommended for production
- Input validation prevents malformed data
- No PII is stored in heartbeat data (only app metadata)

## Related Components

- **Client**: `HeartbeatClient.swift` - Sends heartbeats every 5 seconds
- **Productivity Subagent**: Consumes from `user:{id}:in:prod` stream
- **Redis Streams**: Provides durable, ordered message storage
- **Whiteboard**: Receives productivity decisions from subagent