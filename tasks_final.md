# Alfred MVP Tasks — Final Build Plan (no stubs)

Order of operations (strict):
**Client→Talker**, **Kokoro**, **Inputs/Tools**, **Subagents (hook to tools first)**, **Whiteboard+Manager (wire outputs after subagents exist)**, **Voice stack (STT moved up)**, **Security**, **Billing**.  
No stubs or fakes anywhere.

---

## Phase A — Minimal client + Talker path (real model)

### A-01 Repo + builds
- **Start:** empty.
- **End:** `client/` macOS menubar app + `cloud/` Go server; `make client-dev`, `make cloud-dev`.
- **Test:** `make client-dev` launches menubar; `GET /healthz` → 200.

### A-02 Menubar UI (textbox only)
- **Start:** blank.
- **End:** status-bar icon → popover with multiline textbox + “Send”.
- **Test:** click icon; type in textbox.

### A-03 TalkerBridge → Cerberas (direct)
- **Start:** no send action.
- **End:** “Send” posts to **Cerberas OSS-120B**; show reply text in UI.
- **Test:** type “Hello” → coherent reply (no proxies).

### A-04 Secrets in Keychain
- **Start:** hardcoded.
- **End:** Cerberas URL/token stored/read from Keychain; clear error on 4xx/5xx.
- **Test:** delete token → request fails with UI error; restore token → works.

---

## Phase B — TTS (DeepInfra Kokoro)

### B-01 Kokoro playback (fixed text)
- **Start:** no audio.
- **End:** “Speak Test” hits **DeepInfra Kokoro** with fixed text; plays audio.
- **Test:** hear audio; offline → visible error.

### B-02 Speak Talker replies
- **Start:** test-only.
- **End:** “Speak Reply” plays last Talker text via Kokoro stream.
- **Test:** get reply → click “Speak Reply” → hear it.

### B-03 Pause-on-input hook
- **Start:** none.
- **End:** user typing toggles pause/cancel on current Kokoro playback.
- **Test:** while playing, type → playback pauses.

---

## Phase C — Inputs & Tools (real integrations)

### C-01 Redis online
- **Start:** none.
- **End:** Redis reachable from cloud; `XADD/XREADGROUP` verified.
- **Test:** `XADD user:dev:test * msg test` → read it back.

### C-02 Heartbeat sender (client→server)
- **Start:** none.
- **End:** every 5s POST `{bundle_id, window_title, url?, activity_id?}` to `/prod/heartbeat`.
- **Test:** logs show a POST every 5s; quit app → stops.

### C-03 Heartbeat ingest → input stream
- **Start:** log-only.
- **End:** write each heartbeat to `user:{id}:in:prod`.
- **Test:** `XLEN user:{id}:in:prod` increments.

### C-04 Memory (SQLite WAL)
- **Start:** none.
- **End:** `memory.db` created under `~/Library/Application Support/Alfred/`; “Add Note” saves a row.
- **Test:** add note → `COUNT(*)` increases.

### C-05 Qwen3-Embedding-0.6B local
- **Start:** no vectors.
- **End:** run Qwen-0.6B on device; store 1024-dim vectors for notes.
- **Test:** add two related notes; nearest neighbor returns the related one.

### C-06 Memory cloud mirror (write-through)
- **Start:** device-only.
- **End:** on local change, POST `/memory/upsert`; server persists text+metadata; periodic reconcile.
- **Test:** go offline, add note; go online → syncs; server row exists.

### C-07 Gmail OAuth + token store
- **Start:** none.
- **End:** full OAuth; store refresh token; scope to read inbox.
- **Test:** token refresh succeeds; revoked token handled.

### C-08 Gmail poller (30s) → input stream
- **Start:** none.
- **End:** every 30s list new mail (incremental), push to `user:{id}:in:email`.
- **Test:** send mail to test acct → appears once in stream.

### C-09 Google Calendar webhook registration
- **Start:** none.
- **End:** push channel/watch registered; validation handshake good.
- **Test:** edit test event → webhook fires.

### C-10 Calendar deltas → input stream
- **Start:** webhook only.
- **End:** normalize deltas to `user:{id}:in:calendar`.
- **Test:** move an event → stream item appears.

###C-11 Planner tool (Mini) — DayPlan generator & re-planner

Start: none.

End: /planner/run (GPT-5 Mini) returns a DayPlan from now using prefs/memory and current calendar (shadow + read-only real):

plan_id, version

timeline[] blocks {start, end, label, priority, notes}

conflicts[] (overlaps, travel gaps)

rationale (short text)

No expected apps; no external writes

Test: POST with existing events + "coding 10–12" → DayPlan includes those blocks; conflicts listed if overlaps; no expected_apps anywhere.

---

## Phase D — Subagents first (connect to tools/inputs), THEN wire to WB

### D-01 Calendar-Planner subagent: shadow calendar (Mini)
- **Start:** only deltas exist.
- **End:** maintain **shadow calendar**; apply webhook deltas; compute candidate changes using Planner tool.
- **Test:** create conflict → shadow shows proposal set (no real calendar writes).

### D-02 Calendar-Planner confirm path (internal)
- **Start:** proposals only.
- **End:** local endpoint applies a proposal to real calendar **only when told** (no Talker yet).
- **Test:** call confirm → real event updated.

###D-03 Productivity subagent (GPT-5 Nano) — Hybrid Heuristic (authoritative expected apps)

Start: heartbeats only.

End: Implement the hybrid heuristic that produces the authoritative expected_apps for the current time block. Inputs:

Current DayPlan block (label/priority/notes)

Local memory (user app/tool prefs)

Historical allowlist + recent foreground usage (from heartbeats)

Time-of-day + recency bias
Output: ranked expected_apps[] (bundle IDs) with confidence + TTL (expires at next block boundary).

Keep state internally; never invent DayPlan—only interpret it.

Test: With a DayPlan that has a "coding" block, Productivity emits prod.expected_apps.updated containing a ranked list like [com.microsoft.VSCode, com.google.Chrome] with TTL up to block end.

### D-04 Productivity classifier (2-minute rule)
- **Start:** no decisions.
- **End:** if foreground not in expected set for 120s (timer resets on match): decide **underrun/overrun/allowlist/nudge** (internal state for now).
- **Test:** keep non-expected app in foreground >120s → single decision recorded.

### D-05 Email-Triage subagent (Nano): classify + draft
- **Start:** input-only.
- **End:** for new messages: classify **requires-response** vs FYI; generate one-line summary + 1–2 sentence draft.
- **Test:** email “Can you confirm 3pm?” → decision + summary + draft produced (internal endpoint returns payload).

### D-06 Manager skeleton (Mini)
- **Start:** none.
- **End:** small service that can be called with any subagent output and returns: “ask user?”, “route?”, or “noop”. No bus yet.
- **Test:** feed it a prod.nudge → returns “ask user to refocus”.

---

## Phase E — Whiteboard + wiring (after subagents exist)

### E-01 Whiteboard stream (outputs-only)
- **Start:** none.
- **End:** Redis stream `user:{id}:wb`; append helper; **Server-Sent Events / WebSocket** tail endpoint (client read-only).
- **Test:** `XADD …wb` from admin endpoint → client sees it.

### E-02 Client WB reader UI
- **Start:** no feed.
- **End:** “Whiteboard” list with newest first; read-only.
- **Test:** write message → shows in UI.

### E-03 Wire subagents → WB (no Talker writing)
- **Start:** subagents only internal.
- **End:**  
  - Calendar-Planner emits **planned updates** to WB.  
  - Productivity emits **underrun/overrun/allowlist/nudge** to WB.  
  - Email-Triage emits **sender, summary, draft** to WB.
- **Test:** trigger each → corresponding WB items appear.

### E-04 Manager on WB append
- **Start:** manager skeleton.
- **End:** manager consumes WB; emits **prompts** (e.g., “confirm calendar change?”, “read reply draft?”, “refocus?”) back to WB.
- **Test:** WB items appear after subagent outputs with correct prompts.

### E-05 TalkerBridge tools
- **Start:** Talker only chats.
- **End:** Talker can **read WB**, **call Planner**, **write Memory** (local). Talker **never writes WB**.
- **Test:** say “What’s new?” → Talker references latest WB; “Plan coding” → Planner called; “Remember X” → note saved.

---

## Phase F — Voice stack (STT moved up)

### F-01 VoiceProcessing I/O
- **Start:** no mic path.
- **End:** AVAudioEngine with echo cancellation/noise suppression/gain control (playback as echo ref).
- **Test:** loud TTS while recording → captured audio has no TTS bleed.

### F-02 whisper.cpp streaming STT
- **Start:** none.
- **End:** stream mic frames to whisper.cpp; partials to UI; final on end-of-speech.
- **Test:** speak a sentence → accurate transcript in textbox.

### F-03 VAD gate
- **Start:** none.
- **End:** lightweight VAD toggles “speaking” state; drives STT segmenting.
- **Test:** talk vs silence → low-latency state changes; stable segmentation.

### F-04 Wake word
- **Start:** none.
- **End:** on-device wake word model; adjustable sensitivity.
- **Test:** say keyword → capture starts; low false positive rate.

### F-05 Full barge-in
- **Start:** partial pause on typing.
- **End:** when VAD active, pause/duck TTS within ~100 ms; resume/cancel per user setting.
- **Test:** speak during TTS → immediate pause; resumes correctly.

---

## Phase G — Security & reliability (before billing)

### G-01 TLS only
- **Start:** dev HTTP.
- **End:** all endpoints HTTPS; HSTS; strict TLS config.
- **Test:** HTTP blocked; HTTPS OK.

### G-02 Keychain tokens
- **Start:** env vars.
- **End:** Cerberas/Kokoro/Cloud tokens stored in Keychain; rotate via UI.
- **Test:** rotate → next call uses new token.

### G-03 Rate limits & backpressure
- **Start:** none.
- **End:** per-user limits (Planner, WB append); **Server-Sent Events / WebSocket** keep-alive, idle timeouts, replay-last-N on reconnect.
- **Test:** exceed limits → 429; reconnect → last items replayed.

### G-04 WB validation + idempotency
- **Start:** free-form.
- **End:** validate required fields/types; idempotency keys; reject oversize payloads.
- **Test:** duplicate append ignored; invalid payload 4xx.

### G-05 Observability
- **Start:** scattered logs.
- **End:** structured logs + correlation IDs; metrics for Cerberas, Kokoro, Planner, subagents; traces across calls.
- **Test:** fail Kokoro → visible in logs/metrics with same correlation ID.

### G-06 License-Guard
- **Start:** none.
- **End:** gate Cerberas/Kokoro with license checks (client + server); offline grace.
- **Test:** revoke → calls blocked; restore → unblocked.

### G-07 Clerk auth (late)
- **Start:** open.
- **End:** Clerk JWT on client→server; service mTLS; route scopes.
- **Test:** missing token → 401; valid → 200.

---

## Phase H — Billing last

### H-01 Stripe webhook → entitlements
- **Start:** none.
- **End:** webhook drives entitlements table; caps (TTS minutes, Planner runs).
- **Test:** create/cancel sub in test → entitlements flip.

### H-02 Graceful downgrade
- **Start:** none.
- **End:** over-quota UX: text-only Talker, slower polling, prompts to manage plan.
- **Test:** exceed cap → downgraded behavior without crashes.
