# Alfred - Personal AI Butler

A voice-first AI assistant project designed as a personal butler with calendar, email, and productivity management capabilities.

## Architecture

This project follows a modular, microservices architecture:

- **Client**: macOS menubar application (Swift)
- **Server**: Go backend with Redis streams and AI subagents
- **Services**: Separate TTS, embedding, and connector services
- **Infrastructure**: Redis (streams/pub-sub), Supabase (pgvector), Docker compose

## Development Setup

### Prerequisites

- **Go** 1.21+ for cloud server
- **Xcode** 15+ for macOS client
- **Redis** server for message streaming
- **llama.cpp** build with embedding support (`main` binary >= 0.2.0)
- **Qwen3-Embedding-0.6B** GGUF model file (`qwen3-embedding-0.6b.gguf`)

### Embedding Setup (Qwen3-Embedding-0.6B)

1. Download the GGUF model from the official release: `https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF` (recommended file: `Qwen3-Embedding-0.6B-f16.gguf`; quantized `Q8_0` also works).
2. Place the file at `~/Library/Application Support/Alfred/Models/Qwen3-Embedding-0.6B-f16.gguf` **or** export `ALFRED_EMBED_MODEL_PATH=/absolute/path/to/Qwen3-Embedding-0.6B-f16.gguf`.
3. Build `llama.cpp` with `cmake --build . --config Release` and copy an embedding-capable binary to `~/Library/Application Support/Alfred/bin/llama-embedding` **or** export `ALFRED_EMBED_BIN_PATH=/absolute/path/to/llama/binary`.
4. You can automate the download by running `scripts/install_qwen_embeddings.sh` (optional env `MODEL_FILE=Qwen3-Embedding-0.6B-Q8_0.gguf`).
5. Verify the setup locally:
   ```bash
   cd client
   swift test --target MemoryTests --filter SQLiteStoreTests/testVectorSearchWithStoredEmbeddings
   swift run TestMemory
   ```

### Quick Start

1. **Start Redis server:**
   ```bash
   redis-server
   ```

2. **Start cloud server:**
   ```bash
   make cloud-dev
   ```

3. **Run client (optional):**
   ```bash
   make client-dev
   ```

### Testing

#### Cloud Server Tests

```bash
# Run all cloud tests
make test-cloud

# Run heartbeat API tests specifically
cd cloud && go test ./api -v -run TestHeartbeat

# Run tests with coverage
cd cloud && go test ./api -v -cover
```

#### Heartbeat API Testing

Test the heartbeat endpoint directly:

```bash
# Test successful heartbeat
curl -X POST http://localhost:8000/prod/heartbeat \
  -H "Content-Type: application/json" \
  -d '{"bundle_id": "com.test.App", "window_title": "Test Window"}'

# Verify stream contents
redis-cli XLEN user:dev:test:in:productivity
redis-cli XRANGE user:dev:test:in:productivity - + COUNT 1
```

#### Client Tests

```bash
# Run client tests
make test-client

# Run Memory unit tests (includes vector search)
cd client && swift test --target MemoryTests

# Run end-to-end memory + embedding smoke test
cd client && swift run TestMemory
```

### Development Commands

```bash
# Development targets
make help          # Show all available targets
make cloud-dev     # Start cloud server in development mode
make client-dev    # Build and run macOS menubar app
make test          # Run all tests
make clean         # Clean build artifacts
make dev-check     # Verify development environment
```

## API Documentation

- **Heartbeat API**: [docs/heartbeat_api.md](docs/heartbeat_api.md)
- **Architecture Overview**: [arectiure_final.md](arectiure_final.md)
- **Task List**: [tasks_final.md](tasks_final.md)

## Component Status

### ✅ Completed
- **A-01**: Repository structure and builds
- **A-02**: Basic menubar UI
- **A-03**: TalkerBridge → Cerberas integration
- **C-01**: Redis connectivity
- **C-02**: Heartbeat sender (client)
- **C-03**: Heartbeat ingest → input stream (with tests)
- **C-04**: Memory (SQLite WAL) local store
- **C-05**: Qwen3-Embedding-0.6B on-device vectors

### 🚧 In Progress
- Productivity subagent implementation
- Calendar integration

### 📋 Planned
- Voice processing stack
- Security hardening
- Billing integration

## License

Proprietary - All rights reserved.
