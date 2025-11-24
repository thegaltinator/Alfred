## Alfred Python Helper

This directory contains the on-device service that now owns embeddings, vector search, prompt building, and the Cerberas call. Swift spawns `python_helper/app.py` once at launch and sends it exactly one JSON line per turn over stdin; the helper replies with a single JSON line over stdout.

### Layout

```
python_helper/
├─ README.md                 # this doc
├─ requirements.txt          # pip deps (httpx, pydantic, numpy, faiss, llama-cpp, tiktoken, dotenv)
├─ app.py                    # stdin/stdout loop (loads resources, handles /chat payloads)
├─ manage.py                 # CLI: migrate, backfill, build-index, nn
├─ schema.py                 # Pydantic models for request/response/options
├─ core/
│  ├─ embeddings.py          # Qwen3-Embedding-0.6B runner (llama_cpp)
│  ├─ store.py               # SQLite accessors + embeddings table/migrations
│  ├─ index.py               # FAISS IndexFlatIP wrapper
│  ├─ prompt_builder.py      # memory ranking + 900-token cap templating
│  └─ cerberas.py            # streaming/non-streaming Cerberas client (httpx)
└─ tests/                    # unit tests for prompt builder, FAISS wrapper, embeddings shim
```

### Data + Models

| Component | Location |
| --- | --- |
| Memory DB | `~/Library/Application Support/Alfred/memory.db` |
| Embedding table | `note_embeddings` inside the same DB |
| FAISS index | `~/Library/Application Support/Alfred/index_flatip.faiss` |
| Qwen GGUF | `~/Library/Application Support/Alfred/Models/Qwen3-Embedding-0.6B-f16.gguf` |

Override paths with env vars: `QWEN_EMBED_MODEL`, `ALFRED_DB_PATH`, `ALFRED_FAISS_PATH` if needed.

### Setup / Prereqs

```bash
cd python_helper
pip3 install --upgrade pip
pip3 install -r requirements.txt

python3 manage.py migrate            # ensure notes + note_embeddings tables exist
python3 manage.py backfill --batch 16
python3 manage.py build-index
```

Environment variables are pulled (in order) from:
1. `.env` at repo root (if present)
2. `client/.env` (ships with Cerberas + DeepInfra tokens)
3. Process environment

### Running the helper manually

```bash
python3 app.py <<'EOF'
{"session_id":"demo","user_text":"How do I prep Najdorf?","opts":{"top_k":3,"min_score":0.2,"temperature":0.4,"stream":true}}
EOF
```

Output is a single JSON object:

```json
{
  "assistant_text": "…",
  "used_memory": [{"id": 42, "score": 0.73}],
  "latency_ms": 1210,
  "token_usage": {"prompt": 160, "completion": 90}
}
```

Swift launches the same script via `/usr/bin/env python3 python_helper/app.py`. Override the path with `PY_HELPER_PATH=/absolute/path/to/app.py` if you relocate it.

### Handy CLI commands

All via `python3 manage.py …`:

| Command | Purpose |
| --- | --- |
| `migrate` | ensure schema (notes + note_embeddings) + WAL |
| `backfill --batch 16` | embed any notes missing vectors |
| `build-index` | rebuild the FAISS IndexFlatIP file |
| `nn NOTE_ID --top-k 5` | inspect nearest neighbors for a specific note |

Run `pytest` inside `python_helper/` to execute the small prompt/index/embedding tests.
