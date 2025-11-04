# Alfred Client

macOS menubar application providing voice-first interface to Alfred.

## Structure

- **App/**: AppDelegate, TurnMachine, work-mode switch
- **Audio/**: whisper.cpp bridge + streaming player (barge-in)
- **Embeddings/**: Qwen-Embedding-0.6B sidecar + IPC, SQLite-vector
- **Memory/**: local store (CRUD), preproc composer, Supabase sync
- **Heartbeat/**: 5s publisher (only consumer: productivity agent)
- **TTS/**: DeepInfra Kokoro streaming client
- **IPC/**: Redis TLS client + SSE/WS to server

## Requirements

- macOS 14.0+
- Xcode 15+

## Build

```bash
cd client
xcodebuild -project Alfred.xcodeproj -scheme Alfred -configuration Release build
```

## Development

Refer to individual component READMEs for specific setup instructions.