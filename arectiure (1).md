# Alfred vNext — Canonical Architecture (clean, modular, production-ready)

You were right: we were overcomplicating foreground/tab tracking and memory plumbing. This version trims it down, makes **Talker write memories**, keeps **memory reads as a tiny local pre-processing step**, and nails your **Hybrid Productivity Heuristic** exactly as specced. Outputs still land on a **single Whiteboard Redis stream**; inputs are per-agent streams. Supabase is **memory mirror only**.

---

## What’s non-negotiable

- **Talker writes memories.** Scoped to preferences / habits / aliases / small notes; idempotent, versioned, audited.
- **Memory reads are pre-processing only.** On device: embed prompt with **Qwen-Embedding-0.6B**, retrieve **K** snippets from local SQLite-vector, inject into Talker. No tool call.
- **Whiteboard = one Redis stream** for all outputs; agents only **consume** input streams. Use ZSETs for outstanding/deferred; dedupe with a SET + TTL.
- **Supabase = cloud memory mirror only** (pgvector). Not a whiteboard.
- **Manager (GPT-5 Mini)** runs the LangGraph graph: deferrals, HITL, approvals, commits. Subagents are GPT-5 Nano workers.

---

## File & folder layout (two roots, modular)

```text
repo/
├─ client/                             # macOS menubar app (Swift)
│  ├─ App/                             # AppDelegate, TurnMachine, work-mode switch
│  ├─ Audio/                           # whisper.cpp bridge + streaming player (barge-in)
│  ├─ Embeddings/                      # Qwen-Embedding-0.6B sidecar + IPC, SQLite-vector
│  ├─ Memory/                          # local store (CRUD), preproc composer, Supabase sync
│  ├─ Heartbeat/                       # 5s publisher (only consumer: productivity agent)
│  ├─ TTS/                             # DeepInfra Kokoro streaming client
│  └─ IPC/                             # Redis TLS client + SSE/WS to server
│
├─ server/
│  ├─ app/
│  │  ├─ brain/
│  │  │  ├─ langgraph_manager/         # Manager graph (Mini): nodes, edges, checkpointer
│  │  │  └─ langgraph_talker/          # Talker graph wrapper (pause/resume/HITL)
│  │  ├─ agents/
│  │  │  ├─ scheduler/                 # calendar overlay & commit worker
│  │  │  ├─ comms/                     # email summarize/draft worker
│  │  │  └─ productivity/              # hybrid heuristic worker (only heartbeat consumer)
│  │  ├─ tools/                        # Broker: calendar/gmail/spotify/linkguard
│  │  ├─ events/google/                # Calendar watch + webhook ingest
│  │  ├─ events/gmail/                 # Gmail push bridge + poller
│  │  ├─ whiteboard/                   # write to wb:<user>; maintain outstanding/deferred
│  │  ├─ state/                        # Supabase/pgvector models for cloud memories
│  │  └─ metrics/                      # Prometheus counters/histos
│  └─ infra/
│     ├─ redis/                        # managed Redis (TLS/ACL), stream groups
│     └─ supabase/                     # managed Postgres + pgvector migrations
```

This mirrors your refactor and LangGraph docs: Manager/Talker graphs are the delta; everything else is reused plumbing.

---

## Streams, pub/sub, and who writes what

```text
# INPUT STREAMS (XREADGROUP consumers)
events:calendar.update
events:email.new
agt:scheduler.in
agt:comms.in
agt:productivity.in
hitl:tasks:<user>                  # approvals to Talker

# OUTPUT (the only output surface)
wb:<user>                          # Whiteboard stream (append-only artifacts/status)

# PUB/SUB (ephemeral)
hb:<user>                          # heartbeats (only productivity agent subscribes)

# INDICES / CONTROL
outstanding:<user>                 # ZSET wb ids needing voice attention
deferred:<user>                    # ZSET wb ids with deliver_at
dedup:<user>                       # SET of idempotency keys (TTL)
```

- Agents **never** publish to “out” streams—**they append to `wb:<user>`**. Manager also appends decisions/status there.
- **Deferred** and **outstanding** ZSETs let Manager pause/resume and batch post-busy voice windows.

---

## Subagents (purpose, narrow, production-ready)

- **Scheduler (Nano):** turn calendar deltas into overlays, propose reschedules, commit with **ETag + FreeBusy** after approval.
- **Comms (Nano):** summarize new direct mail, prep drafts, queue for approval, then send. Filters skip newsletters/promos.
- **Productivity (Nano):** the **only heartbeat consumer**; runs the **Hybrid AI Productivity Heuristic** below and posts underrun/overrun/learning updates to the **whiteboard**. (It can emit `agt:productivity.in` inputs if Manager needs to wake policies.)

Agents share the same SDK (cap tokens, budgets, stream I/O) and don’t depend on each other. Drop-in modular.

---

## Manager & Talker (LangGraph)

- **Manager (Mini):** consume inputs, dedupe, policy gate, route to agents, raise HITL, resume to commit tools, update whiteboard, maintain **outstanding/deferred**. Redis checkpointer stores `(user, thread)` state.
- **Talker (Cerberas-OSS-120B):** takes HITL tasks, speaks, captures decision; **can write memories directly** to device store (scoped, schema-first, idempotent).

---

## Memory (simple and fast)

- **Read (pre-proc only):** `whisper.cpp` transcript → **Qwen-Embedding-0.6B** (local) → SQLite-vector top-K → inject snippets into Talker prompt. No tool.
- **Write (by Talker):** preferences / habits / aliases / short notes → **device store** (versioned, supersedes, confidence); async mirrored to **Supabase pgvector**. Undo = supersede last version.
- **Decay:** recency-frequency-confidence score; soft cap on device; archive old versions in cloud; **XTRIM**/TTL for whiteboard.

---

## Heartbeat (cleaned up)

**Interval:** 5 s. **Channel:** `hb:<user>` (pub/sub). **Only the productivity agent subscribes.**

```json
{
  "ts": 1730780000,
  "foreground_window": {
    "bundle_id": "com.microsoft.VSCode",
    "title": "api/routes.ts — project"
  },
  "idle_ms": 1800,
  "work_mode": "work|off"
}
```

No “net” field. No tab scraping. Minimal and stable.

---

## Hybrid AI Productivity Heuristic (server-side)

**Objective**  
Keep the user’s **current activity** aligned with **expected activity** from calendar context; detect **deviations**, **underruns**, **overruns**; post events to **whiteboard**; learn expansions over time.

**Event source**  
- Calendar push (preferred) or poll; each event → **GPT-5 Nano** analysis.  
- **Input:** name, start/end, brief metadata (attendees, location).  
- **Output (heuristic JSON):**  
  - `expected_activity`  
  - `expected_apps` (e.g., VSCode, DevDocs, Notion)  
  - `priority`  
  - `contextual_notes` (optional)

**Heuristic store**  
- Lives **server-side**; the latest active heuristic is attached to the current block. Manager also writes a short artifact to **whiteboard** for audit.

**Heartbeats**  
- Client publishes every 5 s to `hb:<user>` with `foreground_window`, `idle_ms`, `work_mode`. Only **productivity agent** consumes.

**Activity matching**  
- If `foreground_window` matches `expected_apps` → **reset deviation_timer**.  
- If mismatch persists **> 2 min** → query **GPT-5 Nano** with window name + context:  
  - If **semantically related** (e.g., ChatGPT during coding) → **add to expected_apps**, write a **heuristic-update** artifact to **whiteboard**.  
  - If **unrelated** (Netflix/YouTube/Twitter) → **emit state change** and write underrun status to **whiteboard**.

**State change detection**  
- **Overrun:** foreground still matches prior block **end + grace** → **overrun_event** to **whiteboard**.  
- **Underrun:** expected apps closed/mismatch before scheduled end → **underrun_event** to **whiteboard**.  
- **Idle/Unknown:** no heartbeat > **N** seconds or persistent unknown → **idle_state** in **whiteboard**.

**Learning loop**  
- Every valid expansion (e.g., adding ChatGPT to coding) is persisted into the heuristic model for that user/block type. Short-term memory improves future `expected_apps`. (Write the learning artifact to **whiteboard** for traceability.)  
- Manager can treat these signals as **busy/available** toggles and drive deferral drain & batching.

---

## Where state lives

| State                        | Store                           | Why |
|---|---|---|
| Local memories (auth.)       | **SQLite + vector** (device)    | fast, private |
| Cloud memory mirror          | **Supabase + pgvector**         | backup, versions |
| Whiteboard (outputs)         | **Redis stream `wb:<user>`**    | single output surface |
| Inputs (work/events)         | **Redis streams**               | per-agent ingestion |
| Heartbeats                   | **Redis pub/sub `hb:<user>`**   | ephemeral; one consumer |
| Outstanding/Deferred/Dedup   | **Redis ZSET/SET**              | delivery & resume control |
| LangGraph checkpoint         | **Redis**                        | resumable threads |

Whiteboard/outstanding/deferred/dedup are the exact control plane LangGraph leans on.

---

## Security & ops (prod switch-on)

- **Clerk auth** + **license guard** at the broker; caps/budgets per call.
- **Tools enforced:** calendar overlay/commit with **ETag + FreeBusy** + hard-boundary locks; email send requires approval.
- **Audit:** every memory write logs `{user_id, schema, confidence, hash}`; redacted JSON logs.
- **Metrics:** `/metrics` histograms for turn latency, tool calls, queue depth; TTFA placeholder.

---

## Concrete flows (tight)

- **Voice turn:** STT → local embed → K snippets → Talker replies → (if a teach-me statement) **Talker writes memory** → Kokoro streams. Undo = supersede.
- **Calendar push:** webhook → `events:calendar.update` → Scheduler builds overlay → Manager HITL → commit on approve → whiteboard artifact.
- **Email push/poll:** push or historyId poll → Comms drafts → HITL → send → whiteboard completion.

---

## Why this is production-ready

- **Minimal client payloads** (no tab scraping, no mystery fields).
- **Single output surface** (whiteboard stream) with **decay** and **indices** to avoid backlogs.
- **HITL as default** with resumable LangGraph threads and Redis checkpointer.
- **Capability tokens, idempotency, versioning, audits** locked.
- **Golden tests** cover pref-write/undo, email triage, overrun replan. CI criteria are explicit.

---

## TL;DR

- **Talker writes memories.**
- **Reads are a tiny on-device pre-proc step.**
- **Whiteboard is a single Redis stream; inputs are streams; Supabase stores cloud memories only.**
- **Only productivity agent consumes heartbeats** and runs your **Hybrid Heuristic** verbatim.
- Manager/Talker graphs are explicit; subagents are narrow and swappable. This lines up with the refactor + LangGraph plans already in your repo.


## Near-final: Accounts & Billing Rails (Clerk / Stripe / License‑Guard)

**Goal**: hard-gate access and feature scope by authenticated user/org and active subscription. No agent call runs if the license is invalid or scope is exceeded.

### Identity (Clerk)
- **Auth**: Clerk sessions (JWT) provide `user_id`, `org_id`, `roles` (`owner|admin|member`).
- **Orgs & seats**: org-scoped projects; seat assignment stored in `entitlements` (DB), source of truth synced from Stripe.
- **Webhooks**: `user.created`, `organization.created`, `session.ended` → seed/cleanup local rows.
- **Invariants**: requests without valid Clerk session or mismatched org are rejected (`401/403`).

### Billing (Stripe)
- **Plans**: per-seat monthly/annual (e.g., `starter`, `team`, `enterprise`). Optional usage add-ons can emit `usage.record`.
- **Sync**: Stripe → webhook → upsert `subscriptions`, `entitlements` (org_id, plan, seats, status, period_end).
- **Grace**: 3‑day grace after `past_due` before features are hard‑off (configurable).

### License‑Guard (middleware)
- **Scope check**: for every manager/subagent/tool call, evaluate `{active_subscription, seat_assigned, feature_scope}`.
- **Feature flags**: `manager_enabled`, `talker_enabled`, `email_ingest_enabled`, `voice_windows_enabled`.
- **Budgets**: optional per‑org caps (e.g., `max_manager_tokens/day`, `max_talker_minutes/day`). Exceed → `429 license_budget_exceeded`.
- **Cache**: entitlements cached 60s; cache bust on Stripe webhook events.
- **Errors**: standardized `403 license_scope_violation` with `detail:{reason,org_id}`.

### Webhooks (backend endpoints)
- `POST /webhooks/stripe` → verify sig, update `subscriptions` & `entitlements`, bust cache.
- `POST /webhooks/clerk` → mirror user/org, deactivate seats on org removal.

### Data Model (DB)
- `entitlements(org_id pk, plan, seats, status, period_end, features jsonb, updated_at)`
- `subscriptions(id pk, org_id fk, stripe_customer_id, status, current_period_end, plan)`

**Invariant**: No write to `wb:<user>` or `outstanding:<user>` occurs unless License‑Guard admits the call.
