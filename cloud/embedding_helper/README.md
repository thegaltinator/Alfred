# Alfred Embedding Helper

Fast Python service for Qwen3-Embedding-0.6B inference, replacing the slow llama.cpp process approach.

## Why This Exists

The original llama.cpp approach created a new process for every embedding, which was:
- Extremely slow (process startup overhead)
- CPU intensive
- Serialization/deserialization bottleneck
- Difficult to cache and optimize

This Python service:
- Loads the model once at startup
- Provides fast HTTP API endpoints
- Supports batch processing for efficiency
- Uses sentence-transformers for optimized embedding generation
- Much lower CPU usage and latency

## Installation

```bash
cd cloud/embedding_helper
pip install -r requirements.txt
```

## Usage

### Start the service:
```bash
python embedding_helper.py --port 8901
```

### Health check:
```bash
curl http://localhost:8901/health
```

### Single embedding:
```bash
curl -X POST http://localhost:8901/embed \
  -H "Content-Type: application/json" \
  -d '{"text": "Swift concurrency overview"}'
```

### Batch embedding:
```bash
curl -X POST http://localhost:8901/embed_batch \
  -H "Content-Type: application/json" \
  -d '{"texts": ["Text 1", "Text 2", "Text 3"]}'
```

## API Endpoints

- `GET /health` - Health check and model info
- `GET /model_info` - Get model details
- `POST /embed` - Generate single embedding
- `POST /embed_batch` - Generate multiple embeddings efficiently

## Integration with Swift Client

The Swift EmbedRunner will be updated to call this service instead of spawning llama.cpp processes.

## Performance

- **Latency**: ~50-100ms per embedding (vs 2-5 seconds with llama.cpp)
- **CPU Usage**: Much lower (shared model instance)
- **Memory**: Model loaded once and cached
- **Batch**: Processes multiple texts efficiently