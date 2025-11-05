# Alfred (macOS menubar) — Full Architecture (whiteboard = internal outputs only)

Menubar app, small footprint. **Talker = Cerberas OSS-120B** (only user-facing agent).  
**Manager = GPT-5 Mini. Calendar-Planner = GPT-5 Mini.** Other subagents = **GPT-5 Nano**.  
**Whiteboard = Redis Streams, internal, outputs-only.** Talker is **read-only** on the whiteboard.  
**Memory = on-device SQLite (+ Qwen3-Embedding-0.6B index) with cloud mirror + two-way sync.**  
**Speech-to-text = whisper.cpp (local). Text-to-speech = Kokoro via DeepInfra (remote).**  
**Gmail = 30-second poller. Google Calendar = webhook.**  
**Hybrid productivity heuristic lives on the server** (client sends heartbeats every 5s; classification stays inside the subagent).  
No Slack. No “memory search tool.”  
System-prompt files are present (placeholders only, contents not included here).

---

## File + folder structure

```
alfred/
├─ client/                                           # macOS menubar (Swift/ObjC++)
│  ├─ AppKitUI/
│  │  ├─ StatusBar.swift                             # menu icon + popover
│  │  └─ ContentView.swift                           # minimal textbox + transcript
│  ├─ AudioIO/
│  │  ├─ AudioEngine.swift                           # AVAudioEngine + VoiceProcessing I/O (echo cancellation, noise suppression, gain control)
│  │  ├─ VoiceActivityDetection.swift                # gate for barge-in
│  │  └─ WakeWord.swift                              # wake word detector
│  ├─ STT/
│  │  ├─ WhisperBridge.mm                            # whisper.cpp streaming (16 kHz mono PCM16)
│  │  └─ Resampler.swift
│  ├─ TTS/
│  │  └─ DeepInfraKokoro.swift                       # streaming request/response to Kokoro (remote)
│  ├─ Memory/
│  │  ├─ SQLiteStore.swift                           # ONLY memory (WAL) + migrations
│  │  ├─ VecIndex.swift                              # sqlite-vec / sqlite-vss glue (1024-dim cosine)
│  │  ├─ Chunker.swift
│  │  ├─ EmbedRunner.swift                           # Qwen3-Embedding-0.6B (GGUF via llama.cpp/CoreML)
│  │  └─ MemorySync.swift                            # local→cloud push on change; periodic reconcile
│  ├─ Heartbeat/
│  │  ├─ ForegroundWatcher.swift                     # foremost app/window/URL (Accessibility API / NSWorkspace)
│  │  └─ HeartbeatClient.swift                       # POST /prod/heartbeat every 5 s
│  ├─ Bridge/
│  │  └─ TalkerBridge.swift                          # single pipe:
│  │                                                 #  - Cerberas 120B (prompt/stream)
│  │                                                 #  - read whiteboard (Server-Sent Events or WebSocket)
│  │                                                 #  - call Planner tool (endpoint)
│  │                                                 #  - write Memory locally (triggers MemorySync)
│  ├─ Security/
│  │  ├─ LicenseGuardClient.swift                    # gate costly cloud calls
│  │  └─ Keychain.swift                              # token storage
│  └─ Info.plist
│
├─ cloud/                                            # thin server (Go; static binaries)
│  ├─ api/
│  │  ├─ main.go                                     # HTTP + Server-Sent Events + WebSocket; HSTS/CORS; rate limits
│  │  ├─ routes_whiteboard.go                        # whiteboard tail to client (read-only stream)
│  │  ├─ routes_planner_tool.go                      # Planner tool endpoint (callable by Talker)
│  │  ├─ routes_prod_heuristic.go                    # /prod/heartbeat ingress
│  │  ├─ routes_cerberas_proxy.go                    # optional Cerberas proxy (if you don’t hit it direct)
│  │  └─ routes_memory.go                            # cloud memory API (mirror write/read; reconcile)
│  ├─ wb/                                            # whiteboard internal bus
│  │  ├─ bus.go                                      # Redis Streams helpers (append, consumer groups, trim)
│  │  └─ topics.md                                   # user:{id}:wb (outputs); user:{id}:in:<agent> (inputs)
│  ├─ manager/                                       # GPT-5 Mini
│  │  ├─ langgraph.go                                # orchestrator; consumes WB; fans out; writes back to WB
│  │  ├─ checkpoint.go                               # minimal checkpoint store
│  │  └─ system_prompts/manager.system.md            # placeholder file
│  ├─ subagents/
│  │  ├─ calendar_planner/                           # GPT-5 Mini
│  │  │  ├─ google_calendar_webhook.go               # ingest → user:{id}:in:calendar (inputs only)
│  │  │  ├─ shadow_calendar.go                       # internal calendar for staging/test of changes
│  │  │  ├─ planned_updates.go                       # emits planned updates → whiteboard (outputs)
│  │  │  ├─ tool.go                                  # planner tool endpoint (called by Talker)
│  │  │  └─ system_prompts/calendar_planner.system.md
│  │  ├─ productivity/                               # GPT-5 Nano
│  │  │  ├─ heuristic.go                             # heartbeats classification; timers; expected-apps
│  │  │  ├─ consumer.go                              # consumes heartbeats (inputs) → emits underrun/overrun (outputs)
│  │  │  └─ system_prompts/productivity.system.md
│  │  ├─ email_triage/                               # GPT-5 Nano
│  │  │  ├─ poller_30s.go                            # list/get every 30 s → user:{id}:in:email (inputs only)
│  │  │  ├─ classifier.go                            # classify; generate summary and draft
│  │  │  ├─ emitter.go                               # emits sender + summary + draft → whiteboard (outputs)
│  │  │  └─ system_prompts/email_triage.system.md
│  │  └─ talking_agent/                              # Cerberas 120B (cloud text-out if Manager delegates)
│  │     └─ system_prompts/talking.system.md
│  ├─ streams/
│  │  ├─ redis_bus.go                                # typed helpers; consumer groups; dead-letter queue
│  │  └─ contracts.md                                # human-readable contracts (no code schemas)
│  ├─ memory/                                        # cloud memory mirror
│  │  ├─ memory_store.go                             # database tables (text + metadata; no vectors)
│  │  ├─ memory_diff.go                              # two-way reconciliation
│  │  └─ schema.sql                                  # cloud mirror DDL (file exists; contents not in this doc)
│  ├─ security/
│  │  ├─ auth_mw.go                                  # Clerk JWT (late); mutual TLS; scopes
│  │  ├─ license_guard.go
│  │  ├─ link_guard.go
│  │  └─ invariants.go
│  └─ billing/                                       # LAST (Stripe)
│     ├─ stripe_webhook.go
│     └─ entitlements.go
│
├─ talker_prompts/
│  └─ talker.system.md                               # placeholder file
└─ docs/
   ├─ events_overview.md                             # narrative examples only (no JSON/SQL schemas)
   ├─ planner_tool_api.md                            # human-readable contract
   └─ threat_model.md
```

---

## What each part does

### Client (menubar, single process)
- **Audio pipeline** — one AVAudioEngine using VoiceProcessing I/O so playback is the echo reference (true echo cancellation). Voice activity detection and wake word drive barge-in. Text-to-speech duck/stop within ~100 ms; keep a short pre-roll for speech-to-text.
- **Speech-to-text** — whisper.cpp streaming locally at 16 kHz mono PCM.
- **Text-to-speech** — Kokoro via DeepInfra (remote).
- **Memory** — on-device SQLite (write-optimized).  
  - Embeddings with Qwen3-Embedding-0.6B (1024-dim) stored in a vector table for semantic recall.  
  - MemorySync pushes local changes to cloud memory, pulls remote changes on interval, and resolves conflicts.
- **Heartbeat** — every 5 seconds send the foremost app/window/URL and activity context to the productivity service.
- **TalkerBridge** — the only integration path the Talker needs:  
  - read the whiteboard tail as a live stream (Server-Sent Events or WebSocket),  
  - call the Planner tool endpoint,  
  - write Memory locally (which triggers MemorySync).  
  Talker **never** writes to the whiteboard.

### Cloud (whiteboard + subagents + memory mirror)
- **Whiteboard (internal, outputs-only)**  
  - Single canonical stream per user. Only subagents and Manager append. The client reads it; cannot write.  
  - Every append is consumed by Manager (consumer group) and may produce follow-up outputs.
- **Input streams (separate)**  
  - Calendar deltas, productivity heartbeats, and incoming email go to distinct input streams. Subagents consume these inputs; they do not pass through the whiteboard.
- **Manager (GPT-5 Mini)**  
  - Reacts to every whiteboard output. Decides whether to: request confirmation from the user (via Talker), call Planner, route to another subagent, or do nothing. Keeps a small checkpoint for in-flight threads.
- **Calendar-Planner (GPT-5 Mini)**  
  - Ingest calendar webhook updates into its input stream.  
  - Maintains a **shadow calendar** to stage and test changes safely.  
  - Emits **planned updates** to the whiteboard (for example: “move X, add Y, prep bundle Z”).  
  - Exposes a single Planner tool endpoint callable by the Talker.
- **Productivity (GPT-5 Nano)**  
  - Receives heartbeats. All timing rules and classification stay **inside** this service.  
  - Emits **only** its decisions to the whiteboard: **underrun** (you are under target), **overrun** (you are over target), **allowlist expanded**, or **nudge** to return to the expected set. No raw heartbeats are written to the whiteboard.
- **Email Triage (GPT-5 Nano)**  
  - Polls Gmail every 30 seconds (list/get).  
  - Classifies new mail and emits to the whiteboard **only** what the user needs: **sender**, a concise **summary**, and a proposed **draft** reply. No low-value noise.
- **Cloud memory mirror**  
  - Stores memory text and metadata (not vectors). Used for backup/rehydration and multi-device in the future. MemorySync on the client keeps it aligned.

---

## Where state lives

- **Whiteboard**: Redis Streams, per-user. Internal, outputs-only. The authoritative bus for what Talker should consider next.  
- **Inputs**: Redis Streams dedicated to each subagent (calendar, productivity, email). Not visible to the client.  
- **Device memory**: SQLite (WAL) + local vector index (Qwen 0.6B). Source of truth for preferences, notes, and useful information.  
- **Cloud memory**: Mirror of memory text/metadata for resilience and restore.  
- **Manager checkpoints**: small server store to keep orchestration context.  
- **Optional**: compact whiteboard snapshots for cold-start, if you want faster resume.

---

## How services connect

**Client → Talker (Cerberas)**  
- Prompt in, stream out. Client then voices with Kokoro.

**Client → Whiteboard (read-only)**  
- Live tail via Server-Sent Events or WebSocket.

**Client → Planner**  
- Single tool endpoint called from TalkerBridge when the user needs planning or expected-apps.

**Client → Productivity**  
- Foremost app/window heartbeat every 5 seconds.

**Cloud ingest**  
- Calendar webhook feeds the Calendar-Planner input stream.  
- Gmail poller feeds the Email Triage input stream.

**Subagents → Whiteboard**  
- Productivity writes **underrun/overrun/allowlist/nudge** decisions only.  
- Email Triage writes **sender + summary + draft** only.  
- Calendar-Planner writes **planned updates** staged from its shadow calendar.

**Manager**  
- Consumes every whiteboard output, decides whether to involve the Talker for confirmation, route to another subagent, or finalize.

---

## Build order (ship fast, keep it clean)

1) Menubar client → Cerberas talk loop (textbox) → Kokoro audio out.  
2) Whiteboard tail to client (read-only stream).  
3) Productivity path: heartbeats in; subagent emits underrun/overrun/nudges to whiteboard.  
4) Calendar path: webhook in; shadow calendar; planned updates out to whiteboard; Planner tool callable by Talker.  
5) Gmail poller: every 30 seconds; subagent emits sender/summary/draft to whiteboard.  
6) Manager: consume whiteboard, decide when to ask the user (via Talker) or route.  
7) Memory: Qwen 0.6B embeddings on device; MemorySync to cloud mirror.  
8) Voice polish: wake word, voice activity detection, barge-in, echo cancellation.  
9) Security hardening; License-Guard; Clerk/Stripe last.
