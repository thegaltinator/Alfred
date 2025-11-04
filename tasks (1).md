# MVP Build Plan — Voice‑First Butler (LLM‑First)
Single‑responsibility tasks with clear start/end and acceptance criteria. Execute **one task at a time**, commit, test, then proceed.

Legend (prefixes):
- **S-** server/conductor (FastAPI)
- **C-** client macOS (Swift, menubar)
- **T-** TTS service (csm‑streaming)
- **P-** proactivity bus/workers
- **G-** Google (Gmail/Calendar)
- **K-** Slack
- **SP-** Spotify
- **B-** Billing (Clerk/Stripe/license)
- **MEM-** Memory/pgvector
- **SEC-** Security/guardrails
- **PR-** Prompts & golden tests
- **QA-** Latency/quality
- **E2E-** End‑to‑end manual checks

All file paths match `architecture.md`.

---

## Phase 0 — Repo, Tooling, CI

### S-00: Initialize monorepo
- **Goal**: Clone folder layout from architecture.
- **Start**: Empty repo.
- **End**: Committed skeleton folders.
- **Acceptance**: `tree` shows `client-mac/`, `server/app/`, `services/tts-csm/`, `infra/`, `docs/`, `scripts/` with READMEs.

### S-01: FastAPI bootstrap + health
- **Goal**: Server boots and returns health JSON.
- **Start**: Empty `server/app/`.
- **End**: `uvicorn` serves `/healthz`.
- **Acceptance**: `curl :8000/healthz` → `{"ok":true,"version":"0.0.1"}`.

### S-02: Dockerized Postgres+Redis
- **Goal**: Local DB/Redis via `docker-compose`.
- **Start**: Empty `infra/docker-compose.yml`.
- **End**: `docker compose up` runs pg+redis+server.
- **Acceptance**: `psql` connects; `redis-cli PING` → PONG.

### S-03: Migrations (Alembic) init
- **Goal**: Migration pipeline created.
- **Start**: No migrations.
- **End**: `alembic upgrade head` succeeds.
- **Acceptance**: Empty DB upgrades cleanly.

### S-04: Base models
- **Goal**: Create core tables.
- **Start**: No tables.
- **End**: Users, subscriptions, licenses, oauth_tokens, proposals, blocks, tasks created.
- **Acceptance**: `\dt` lists tables; Alembic stamped.

---

## Phase 1 — Client Shell (Voice‑First)

### C-01: Menubar app
- **Goal**: App launches with menubar icon.
- **Start**: Empty `client-mac`.
- **End**: Running app with menu stub.
- **Acceptance**: Icon visible; menu opens.

### C-02: Keychain store
- **Goal**: Save/get/delete tokens.
- **Start**: None.
- **End**: Wrapper for Clerk token, license, refresh tokens.
- **Acceptance**: Unit test persists across launch.

### C-03: Wake/PTT
- **Goal**: Push‑to‑talk + wakeword stub.
- **Start**: None.
- **End**: Keypress holds mic; placeholder wakeword toggles.
- **Acceptance**: Console logs show transitions.

### C-04: VAD capture
- **Goal**: Mic capture with Silero endpointing.
- **Start**: None.
- **End**: Automatic stop ~300 ms after silence.
- **Acceptance**: Logs show turn end when speech stops.

### C-05: STT stable prefix
- **Goal**: faster‑whisper small‑int8 streaming.
- **Start**: VAD works.
- **End**: Emit stable prefix chunks.
- **Acceptance**: First partial within ~120 ms after start; accuracy visible in logs.

### C-06: TTS player with barge‑in
- **Goal**: Streaming PCM playback; duck/stop on speech.
- **Start**: None.
- **End**: AVAudioEngine plays chunks; barge‑in stops playback.
- **Acceptance**: Speaking over TTS immediately halts audio.

### C-07: TurnMachine state machine
- **Goal**: Idle→Listen→Plan→Speak→Await→(Commit|Abort).
- **Start**: None.
- **End**: State transitions with callbacks.
- **Acceptance**: Debug overlay shows state changes per turn.

### C-08: Presence modes & rails
- **Goal**: Talkative/Subtle/Silent + caps.
- **Start**: None.
- **End**: Focus/call mute; cap 4 spoken prompts/hour.
- **Acceptance**: Focus on → no voice; counter enforces cap.

### C-09: Voice Inbox (minimal)
- **Goal**: Optional window listing last 10 proposals.
- **Start**: None.
- **End**: Approve/Reject buttons.
- **Acceptance**: Actions call server; UI updates.

---

## Phase 2 — Auth & Billing

### B-01: Clerk middleware (server)
- **Goal**: Verify Clerk JWT on `/v1/*`.
- **Start**: Open routes.
- **End**: Middleware validates; `/healthz` allowed.
- **Acceptance**: Invalid token → 401; valid → pass.

### C-10: Clerk sign‑in flow (client)
- **Goal**: Acquire Clerk session token.
- **Start**: None.
- **End**: Webview login → token saved in Keychain.
- **Acceptance**: Subsequent calls include token header.

### B-02: Stripe checkout (trial 3 days)
- **Goal**: Create Checkout sessions.
- **Start**: None.
- **End**: `POST /billing/checkout` returns URL.
- **Acceptance**: Test session opens Stripe page.

### B-03: Stripe webhooks
- **Goal**: Subscription lifecycle → DB.
- **Start**: None.
- **End**: `checkout.session.completed` and `customer.subscription.*` handled.
- **Acceptance**: Replay test events → status updates.

### B-04: License JWT
- **Goal**: Mint/verify license tokens.
- **Start**: None.
- **End**: `/billing/license/exchange` returns JWT; 48h offline grace.
- **Acceptance**: Protected route requires active/trialing license.

### C-11: Paywall guard
- **Goal**: Gate features when license invalid.
- **Start**: None.
- **End**: Menubar shows paywall; checkout opens.
- **Acceptance**: License expire → features disabled.

---

## Phase 3 — Tools (Read‑Only First)

### S-10: Tool registry scaffold
- **Goal**: Register tool schemas (no impl).
- **Start**: None.
- **End**: `tools/*` signatures exported to LLM.
- **Acceptance**: `/v1/turn` advertises tools list.

### S-11: calendar.read_day
- **Goal**: Return hard/soft blocks, gaps, buffers.
- **Start**: None.
- **End**: Function using Google Calendar; Agent Calendar overlay respected.
- **Acceptance**: Unit returns expected structure on fixtures.

### S-12: calendar.freebusy
- **Goal**: FreeBusy query.
- **Start**: None.
- **End**: start/end ISO input, list of busy intervals.
- **Acceptance**: Known ranges return expected busy blocks.

### S-13: gmail.list_invites
- **Goal**: Parse ICS from emails.
- **Start**: None.
- **End**: Return EventCandidates with iCalUID.
- **Acceptance**: Fixture emails parse correctly.

### S-14: slack.list_mentions
- **Goal**: Fetch DMs/@you since timestamp.
- **Start**: None.
- **End**: Return normalized messages.
- **Acceptance**: Fixture returns expected set.

### S-15: memory.search
- **Goal**: pgvector search.
- **Start**: None.
- **End**: `query,k` → top‑k with scores.
- **Acceptance**: Deterministic order on test data.

---

## Phase 4 — /v1/turn (Brain) + Speech Plan

### PR-01: turn_system.md (persona & etiquette)
- **Goal**: Write system prompt (butler voice, minimal‑diff, confirm‑first).
- **Start**: Empty prompt.
- **End**: Markdown with rules in architecture.
- **Acceptance**: Review includes hooks, continuation, no narration of prep.

### PR-02: scheduler_rules.md
- **Goal**: Calendar‑first rules (20‑min overrun, pull‑forward/reset, invite options).
- **Start**: None.
- **End**: Markdown file with crisp rules.
- **Acceptance**: Peer review; linked by `turn.py`.

### S-20: /v1/turn streaming
- **Goal**: Implement endpoint calling Responses API with tools.
- **Start**: None.
- **End**: Streams `{delivery,speech_plan,proposal,fallback_line}`.
- **Acceptance**: Test response renders opening line first token.

### C-20: Client turn integration
- **Goal**: Wire TurnMachine to /v1/turn.
- **Start**: Standalone client.
- **End**: Listen→Plan→Speak→Await cycle works.
- **Acceptance**: Manual roundtrip prints proposal.

### C-21: Speak opening then continuation
- **Goal**: Two‑stage TTS with barge‑in.
- **Start**: Basic TTS.
- **End**: Opening line <0.8s; continuation up to 20s; barge‑in stops.
- **Acceptance**: Stopwatch <0.8s to first audio.

---

## Phase 5 — Write Tools (Guarded)

### S-30: calendar.overlay_diff
- **Goal**: Write overlay events only.
- **Start**: None.
- **End**: Returns `op_id`; tags with `extendedProperties.agent=true`.
- **Acceptance**: New events appear in Agent Calendar, not Primary.

### S-31: calendar.commit (guarded)
- **Goal**: Mirror overlay to Primary with ETag+FreeBusy, hard locks.
- **Start**: None.
- **End**: Reject touching hard boundaries; rebase on stale ETag.
- **Acceptance**: Unit tests: success, hard‑lock reject, ETag rebase path.

### S-32: gmail.create_drafts
- **Goal**: Create Gmail drafts from items.
- **Start**: None.
- **End**: Returns draft IDs; never send.
- **Acceptance**: Drafts appear in Gmail UI.

### S-33: gmail.send_drafts (confirm‑only)
- **Goal**: Send drafts by ID.
- **Start**: None.
- **End**: Only callable after Proposal approval.
- **Acceptance**: Attempt without approval → 403; approval → sent.

### S-34: slack.post_message (confirm‑only)
- **Goal**: Post messages via Proposal.
- **Start**: None.
- **End**: Enforces approval; logs action.
- **Acceptance**: Messages appear in target channel.

### S-35: spotify.play_playlist
- **Goal**: Play by URI/name; prefer local.
- **Start**: None.
- **End**: AppleScript local; web fallback by device id.
- **Acceptance**: Starts correct playlist locally; if app closed, web plays.

### S-36: memory.upsert
- **Goal**: Store small notes/tags.
- **Start**: None.
- **End**: Insert row with embedding and metadata.
- **Acceptance**: Upsert visible; subsequent search retrieves it.

### S-37: linkguard.check_url
- **Goal**: SPF/DKIM/DMARC + domain alignment.
- **Start**: None.
- **End**: Returns pass/fail with reason.
- **Acceptance**: Fixture headers classify correctly.

---

## Phase 6 — Proactivity Bus & Ambient Prep

### P-01: Redis bus topics
- **Goal**: Define topics & payload schema.
- **Start**: None.
- **End**: `proactivity:*` channels created.
- **Acceptance**: Pub/sub echo test works.

### C-30: Heartbeat publisher
- **Goal**: Send heartbeat every 45s.
- **Start**: None.
- **End**: Payload: front app, tab domain/title, idle_sec, focus/call, presence.
- **Acceptance**: Server logs events at cadence.

### P-02: Overrun detector
- **Goal**: Detect >20 min past block end with context match.
- **Start**: None.
- **End**: Emits `reason='overrun'` with block_id, over_by_min.
- **Acceptance**: Fixture passes produce events.

### P-03: Underrun detector
- **Goal**: Detect early finish (client signal or heuristic).
- **Start**: None.
- **End**: Emits `reason='underrun'` with freed_min.
- **Acceptance**: Manual trigger creates event.

### P-04: Proximity detector
- **Goal**: Soft block squeezing hard boundary.
- **Start**: None.
- **End**: Emits `reason='proximity'`.
- **Acceptance**: Synthetic day triggers event.

### P-05: Ambient worker
- **Goal**: Consume events; build prepared Proposals only.
- **Start**: None.
- **End**: Creates overlay diffs, email drafts; sets TTL 24h; `delivery_hint:"voice_at_next_window"`.
- **Acceptance**: Queue shows prepared proposals; no commits happen.

### P-06: Voice window scheduler
- **Goal**: Open `reason='voice_window'` turns at transitions.
- **Start**: None.
- **End**: Coalesce invites/drafts/replan flags into a single turn.
- **Acceptance**: End of block triggers a spoken prompt.

---

## Phase 7 — Google & Slack Connectors (Push)

### G-01: Google OAuth
- **Goal**: Connect with offline access.
- **Start**: None.
- **End**: Refresh token encrypted in DB.
- **Acceptance**: Test account connects; token refresh succeeds.

### G-02: Calendar `events.watch`
- **Goal**: Push channel created & renewed.
- **Start**: None.
- **End**: Store channel ids/expiry; renew worker.
- **Acceptance**: Webhook receives notifications.

### G-03: Calendar webhook ingest
- **Goal**: Verify headers; fetch changes.
- **Start**: None.
- **End**: Enqueue `calendar_update` event.
- **Acceptance**: Logs show deltas processed.

### G-04: Gmail `users.watch`
- **Goal**: Push history tracking.
- **Start**: None.
- **End**: Save latest historyId; renewals.
- **Acceptance**: Webhook fires; history diffs retrieved.

### G-05: ICS parse & dedupe
- **Goal**: Parse invites; iCalUID dedupe; FreeBusy check.
- **Start**: None.
- **End**: EventCandidates with conflict info added to ambient queue.
- **Acceptance**: Fixtures matched to expected conflicts.

### K-01: Slack app install
- **Goal**: OAuth for workspace.
- **Start**: None.
- **End**: Bot token saved; scopes set.
- **Acceptance**: Can list DMs/@mentions.

### K-02: Slack Events HMAC verify
- **Goal**: Verify signatures & timestamps.
- **Start**: None.
- **End**: Invalid signature → 401.
- **Acceptance**: Unit tests for good/bad signatures.

---

## Phase 8 — Spotify Control

### SP-01: Local control
- **Goal**: AppleScript/JXA play by name/URI.
- **Start**: None.
- **End**: Helper executes script reliably.
- **Acceptance**: Specified playlist starts locally.

### SP-02: Web API fallback
- **Goal**: Device discovery + playback.
- **Start**: None.
- **End**: If local app absent, use Web API device.
- **Acceptance**: Playback starts on target device.

### SP-03: Preference memory
- **Goal**: Bind playlist↔context in memory.
- **Start**: None.
- **End**: Accept/Reject updates memory tags.
- **Acceptance**: Next suggestion prefers accepted playlist.

---

## Phase 9 — Memory (pgvector)

### MEM-01: Enable pgvector
- **Goal**: Install extension & table with vector column.
- **Start**: Base DB.
- **End**: `vector_chunks` table with embedding column.
- **Acceptance**: `SELECT * FROM pg_extension` includes `vector`.

### MEM-02: VectorStore interface
- **Goal**: search/upsert/delete impl.
- **Start**: None.
- **End**: Deterministic cosine search.
- **Acceptance**: Unit returns expected ID order.

### MEM-03: Estimates learning
- **Goal**: Track planned vs actual deltas per task type.
- **Start**: None.
- **End**: Bias applied to next estimates.
- **Acceptance**: Simulated history affects next plan length.

---

## Phase 10 — Security & Guardrails

### SEC-01: TLS/HSTS
- **Goal**: TLS 1.3 + HSTS on ingress.
- **Start**: Default nginx.
- **End**: Valid certs; strict headers.
- **Acceptance**: SSL Labs A locally (self‑signed ok for dev).

### SEC-02: Link guard
- **Goal**: SPF/DKIM/DMARC + eTLD+1 alignment.
- **Start**: None.
- **End**: `check_url` returns pass/fail.
- **Acceptance**: Fixtures behave as expected.

### SEC-03: Calendar commit guard
- **Goal**: Enforce hard‑boundary lock + ETag/FreeBusy.
- **Start**: None.
- **End**: Reject risky diffs; rebase to new Proposal.
- **Acceptance**: Unit tests cover edge cases.

### SEC-04: Log hygiene
- **Goal**: No tokens/PII in logs.
- **Start**: Verbose logs.
- **End**: Redaction middleware.
- **Acceptance**: Test logs redact secrets.

---

## Phase 11 — Prompts & Golden Tests

### PR-10: Golden transcript tests (day start)
- **Goal**: Replay model → expected tool calls & Proposal.
- **Start**: None.
- **End**: Fixture produces identical sequence.
- **Acceptance**: CI diff fails on drift.

### PR-11: Golden (overrun)
- **Goal**: Overrun turn matches expected minimal‑diff Proposal.
- **Start**: None.
- **End**: Deterministic output with set seed.
- **Acceptance**: CI green.

### PR-12: Golden (invite conflict)
- **Goal**: Invite with soft conflict → correct options.
- **Start**: None.
- **End**: Deterministic Proposal.
- **Acceptance**: CI green.

### PR-13: No‑voice scenarios
- **Goal**: `digest_due`/`calendar_update` never return `delivery:"voice"`.
- **Start**: None.
- **End**: Tests enforce toast fallback.
- **Acceptance**: CI green.

---

## Phase 12 — Latency & Quality Polish

### QA-01: First audio timing
- **Goal**: Opening line <0.8s TTFA.
- **Start**: Baseline.
- **End**: Measure across 20 trials.
- **Acceptance**: p95 < 0.8 s.

### QA-02: Barge‑in responsiveness
- **Goal**: Stop TTS within 100 ms on user speech.
- **Start**: Baseline.
- **End**: Audio duck/stop meets target.
- **Acceptance**: p95 < 120 ms stop time.

### QA-03: Tool budgets
- **Goal**: ≤3 tool calls/turn; user 2.5s; proactive 1.5s.
- **Start**: None.
- **End**: Enforced by handler.
- **Acceptance**: Logs show limits; overflow → short clarify line.

### QA-04: Hot TTS worker
- **Goal**: Keep 1 worker warm when active.
- **Start**: Cold starts present.
- **End**: Autoscaler maintains hot instance; idle TTL.
- **Acceptance**: TTS first chunk under 200 ms when warm.

### QA-05: Read‑tool cache per turn
- **Goal**: Cache `read_day`/`freebusy` within a turn.
- **Start**: Multiple identical calls.
- **End**: Single fetch used.
- **Acceptance**: Log proves de‑duplication.

---

## Phase 13 — E2E Scenarios

### E2E-1: Day start → plan → voice approve → commit
- **Goal**: Full flow.
- **Acceptance**: Agent Calendar overlay then Primary mirror; voice only.

### E2E-2: Overrun → extend & shift
- **Goal**: Heartbeat shows VS Code; 25 min over.
- **Acceptance**: Spoken prompt; commit after “Yes”; hard boundary untouched.

### E2E-3: Invite add with soft conflict
- **Goal**: ICS overlaps soft block.
- **Acceptance**: “Add & move?” voiced; approval applies.

### E2E-4: Early finish → reset or pull‑forward
- **Goal**: Finish early; suggest music.
- **Acceptance**: Spoken choice; local Spotify plays if approved.

### E2E-5: Drafts batch send
- **Goal**: Prepare 3 drafts silently.
- **Acceptance**: “Send them?” voiced; upon “Yes”, emails sent.

### E2E-6: Focus mute
- **Goal**: Enable Focus mode.
- **Acceptance**: Agent downgrades to toast; no voice.

### E2E-7: Billing gate
- **Goal**: Expire trial.
- **Acceptance**: License guard blocks features; after payment, features return.

---

## Done Checklist (ship gate)
- [ ] Auth/billing: Clerk + Stripe (trial→$20/mo), license JWT.
- [ ] `/v1/turn` with speech_plan streaming; tool registry wired.
- [ ] Read tools: calendar/gmail/slack/memory; write tools guarded.
- [ ] Client TurnMachine with barge‑in; TTFA p95 <0.8 s.
- [ ] Proactivity bus + ambient worker; voice window scheduler.
- [ ] Gmail/Calendar/Slack push; ICS parse & dedupe.
- [ ] Spotify local control + web fallback.
- [ ] pgvector store + estimate learning.
- [ ] Security: TLS/HSTS, link guard, commit guard, log redaction.
- [ ] Golden tests pass; E2E scenarios pass.


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

## Tie‑Together & Endgame

### Decay & GC (Streams, Memory, and Snapshots)

**Purpose**: bound state growth and keep prompts lean without losing auditability.

- **Redis stream trimming**: 
  - `wb:<user>` and `outstanding:<user>` → `XADD ... MAXLEN ~ 1000`; background trimmer enforces hard cap 2000.
  - Secondary streams (heartbeats, diagnostics) → `MAXLEN ~ 500`.
- **TTL / archival**:
  - Resolved items copied to `supabase.public.items_archive` then removed from Redis after **72h**.
  - Memory items decay: `confidence = confidence * 0.96` daily; items `<0.15` are archived.
- **Orphan cleanup**: entries with missing `whiteboard_id` or stale (>7d) `deferral_until` are pruned.
- **Schedules**:
  - Hourly: trim streams, decay memory, flush Prometheus gauges.
  - Daily @03:00 local: archive aged items, VACUUM tables.
- **Constants** (defaults):
  - `IDLE_MISSING_S=30`, `OVERRUN_GRACE_M=5`, `MAX_WB_LEN=2000`, `MAX_OUTSTANDING_LEN=2000`.

### LangGraph Checkpoint Contents (Minimal, Deterministic)

Persisted per conversation graph to allow **exact resume** after crashes, deploys, or device sleep.

```
checkpoint := {
  "version": 2,
  "whiteboard_id": "<uuid>",
  "last_activity_at": "<iso8601>",
  "deferral": {
    "reason": "meeting|sleep|quota|manual",
    "until": "<iso8601>|null"
  },
  "context_digest": "sha256(<top_prefs + top_mem + outstanding_digest>)",
  "manager_counters": { "pauses": int, "resumes": int, "wake_emits": int },
  "policy_version": "<semver>",            // aligns with policy.yaml
  "snapshot_ref": "supabase://snapshots/<uuid>",
  "license_scope": { "plan": "starter|team|enterprise", "features": [ ... ] }
}
```
**Storage**: LangGraph checkpointer → Redis hash; snapshots ≤5 KB also mirrored to Supabase (`snapshots` table, immutable).  
**Retention**: keep 30 days; GC removes older unless `hold=true`.

### Ambient Prep & Voice‑Window Scheduler (Delivery Hints)

**Goal**: deliver concise voice summaries at **user‑friendly windows** without spam.

- **Intent shape** (`ambient_prep`):
  - `ttl_s: 3600`, `delivery_hint: "voice_at_next_window"`, `priority: "low|normal|high"`.
- **Windows**: derived from user prefs (`voice_windows`: e.g., 08:00–10:00, 12:00–13:00, 18:00–20:00, respecting quiet hours).
- **Coalescing**:
  - Aggregator groups intents within the same window; max one voice delivery per window.
  - If multiple summaries exist, compose ≤90s script (≤160 words) with links pushed to `wb:<user>`.
- **Backoff**:
  - If user rejects/interrupts twice in a window → suppress next window; resume afterwards.
- **Scheduler loop**:
  1. On new `ambient_prep`, tag with nearest window (local tz).
  2. 5 min before window: fetch top `outstanding:<user>` and prefs; build script with Context Composer.
  3. At window open: check License‑Guard & presence; emit to Talker; record delivery in metrics.
- **Metrics**: `voice_windows_emitted_total`, `voice_windows_skipped_quiet_hours_total`, `voice_windows_coalesced_total`.
