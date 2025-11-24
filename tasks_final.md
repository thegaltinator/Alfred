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

Phase E — Whiteboard, LangGraph, and Autonomous Subagents
E-01 Whiteboard stream (outputs-only)

Start: No shared event bus.

End: Redis stream user:{id}:wb created per user with:

simple append helper for server components,

Server-Sent Events endpoint /wb/stream (client read-only),

WebSocket endpoint /wb/ws (client read-only).

Test: XADD user:{id}:wb * type test msg "hello" → client UI shows a “hello” whiteboard item.

E-02 Client whiteboard reader UI

Start: Client does not show WB content.

End: Menubar client has a “Whiteboard” list:

newest items at top,

read-only,

each item shows source (calendar/prod/email/manager), type, and short msg.

Test: Append a test WB item with type=test and msg="ping" → appears in the list with correct source and message.

E-03 Thread IDs and event tagging

Start: No clear separation of conversations.

End:

Each Talker session gets a thread_id (UUID).

Client attaches thread_id to:

Talker calls,

all WB writes,

Manager requests,

any related memory entries.

Redis keys and LangGraph checkpoints use (user_id, thread_id) as identity.

Test: Run two parallel conversations; verify WB items and checkpoints for each have different thread_id values and never mix.

E-04 Event normalization (WB → Manager input)

Start: WB holds arbitrary JSON blobs.

End: Normalization layer that maps WB items into typed events for Manager:

calendar.plan.proposed {delta_id, summary, impact}

calendar.plan.new_version {plan_id, version}

prod.underrun {block_id, activity_label}

prod.overrun {block_id, activity_label}

(optional) prod.nudge {block_id, activity_label}

email.reply_needed {message_id, sender, summary, draft}

manager.user_action {action_id, choice, thread_id, metadata}

Test: Write one WB item of each type → Manager logs show correct normalized event type and parsed payload.

E-05 Manager service: LangGraph runtime bootstrap

Start: No orchestration layer.

End: Manager runs as a long-lived service with:

LangGraph engine initialized,

Redis connection (WB + control streams),

config for Planner URL and Productivity control endpoint,

/healthz endpoint,

loop that reads WB items, normalizes them, and feeds them to LangGraph.

Test: Start Manager service:

/healthz returns 200,

logs show successful connection to Redis and idle LangGraph worker.

E-06 ManagerGraph nodes (no expected-app leakage)

Start: Graph not defined.

End: LangGraph graph with nodes:

ingest_wb: entry node taking {wb_id, user_id, thread_id, event}.

router: routes by event.type to:

calendar_branch,

prod_branch,

email_branch,

user_action_branch.

planner_call: calls /planner/run (Mini) to compute or update DayPlan.

prod_recalc_signal: sends internal recompute trigger to Productivity (no WB write).

maybe_prompt_user: decides if a user-facing prompt is needed.

emit_prompt: appends exactly one Manager prompt event to WB.

Test: Inject a synthetic prod.overrun WB item → ManagerGraph path in logs shows ingest_wb -> router -> prod_branch -> emit_prompt; exactly one prompt appears on WB.

E-07 Manager policies (graph edges and behavior)

Start: No clear behavior on events.

End: Policies:

calendar_branch:

On calendar.plan.proposed or calendar.plan.new_version or raw calendar delta:

call planner_call → updated DayPlan,

if today is impacted, send event to maybe_prompt_user then emit_prompt (e.g. “Apply these changes now?”),

always call prod_recalc_signal after a DayPlan update (internal only; no WB write).

prod_branch:

On prod.underrun or prod.overrun:

emit_prompt with options like ["refocus", "update_plan", "dismiss"].

email_branch:

On email.reply_needed:

emit_prompt proposing actions like “Read draft aloud?” / “Send?” / “Dismiss”.

user_action_branch:

On manager.user_action:

if choice == "update_plan": call planner_call, then prod_recalc_signal, then optional follow-up prompt summarizing the new plan.

if choice == "refocus" or choice == "dismiss": mark prompt resolved in checkpoint, no planner call, no Productivity signal.

Test: Trigger prod.overrun → prompt appears with the right options; send manager.user_action with choice="update_plan" → Manager calls Planner exactly once and sends one internal Productivity recompute trigger.

E-08 Checkpointing (contents and usage)

Start: Manager state not persisted.

End: LangGraph checkpoint store per (user_id, thread_id) containing:

last_wb_id_processed,

last_plan_id,

last_plan_version,

pending_prompt_id (if there is an unanswered prompt),

side_effects_log listing completed external calls with idempotency keys.

Test: Cause Manager to handle a prod.overrun and a calendar.plan.proposed:

check checkpoint store; it tracks the last wb_ids, last plan, and any pending prompt,

restart Manager and replay WB from the same point → it does not re-call Planner or re-emit the same prompt.

E-09 Idempotency and replay safety

Start: Replays may double-call Planner or double-prompt.

End:

Manager ignores WB items with wb_id <= last_wb_id_processed.

Each external call uses an idempotency key (user_id, thread_id, wb_id, node_name).

Before making a call, Manager checks side_effects_log; skips if call already recorded.

Test: Replay the same WB event (same wb_id) multiple times:

only one Planner call appears in logs,

only one corresponding prompt shows on WB.

E-10 User choice → WB → ManagerGraph loop

Start: User actions are not first-class events.

End:

Manager prompts include a unique action_id and a fixed set of options.

Talker presents these options in the UI (buttons or command phrases).

When user selects an option, client POSTs /wb/user_action which writes:

manager.user_action {action_id, choice, thread_id, metadata} to WB.

user_action_branch in ManagerGraph consumes it and applies the policy defined in E-07.

Test: Trigger prod.overrun → WB prompt; click “update plan” → manager.user_action event appears; Manager calls Planner exactly once and sends one internal Productivity recompute signal.

E-11 Whiteboard trimming and archival

Start: WB can grow unbounded.

End:

Use MAXLEN ~ on user:{id}:wb to limit stream length (for example ~1000 items).

Optionally mirror a minimal summary of each item to a log table for analytics (not needed for runtime).

Test: Push >1000 events into WB → Redis stream length stays around configured max; older entries are trimmed; newest events remain visible.

E-12 Checkpoint GC and decay

Start: Checkpoints accumulate forever.

End:

For each (user_id, thread_id), keep at most N checkpoints or M days of history.

Older checkpoints are compacted into a small summary record (last plan ID/version, last decision type, last prompt) and detailed steps are deleted.

Test: Generate more than N WB events for a thread → confirm older checkpoints are summarized and removed; /debug/checkpoint returns only recent ones plus summary.

E-13 Manager observability

Start: Only ad-hoc logs.

End: Manager exports /metrics with:

WB lag per user/thread,

Planner call count + error rate,

Productivity recompute signal count,

prompts emitted per type,

LangGraph node error counts.

Test: Trigger several calendar, productivity, and email events → /metrics shows non-zero counts; lag remains low under normal conditions.

E-14 LangGraph local test harness

Start: Only integration testing through full stack.

End: Small test runner that:

runs ManagerGraph in-process (no Redis),

feeds a scripted list of synthetic events (sequence of prod.overrun, calendar.plan.proposed, email.reply_needed, manager.user_action),

asserts:

no duplicate Planner calls for identical wb_id,

no duplicate prompts for the same decision,

correct routing by type.

Test: Run the test harness; all assertions pass; failing them produces clear messages.

E-15 Calendar-Planner subagent: stream consumer loop

Start: Calendar subagent only reacts when manually called.

End: Calendar-Planner (Mini) runs as a worker that:

consumes user:{id}:in:calendar via a Redis consumer group,

applies deltas to the shadow calendar,

uses Planner tool as needed,

emits only decisions to WB:

calendar.plan.proposed,

calendar.plan.new_version.

Test: Edit or add a test event → a single calendar.plan.proposed or calendar.plan.new_version appears on WB; no direct writes to real calendar occur from this worker.

E-16 Calendar-Planner replay, dedupe, and idempotency

Start: Potential to process the same delta twice.

End:

Track last_stream_id per user and drop items with stream_id <= last_stream_id.

Use a stable delta_id to ensure each calendar change yields at most one WB decision.

Test: Restart the Calendar-Planner worker with unacknowledged items still in stream → each delta produces exactly one WB decision; no duplicates.

E-17 Calendar “shadow vs real” safety checks

Start: No final safety check before real calendar writes.

End: Before any confirmed plan is applied to real calendar (via a separate write path):

fetch current real calendar state for affected events,

compare with shadow state,

if drift is detected (user changed real calendar separately), do not write:

instead emit a new calendar.plan.proposed WB event explaining the conflict.

Test: Generate a plan, then manually change the real event before confirming → system refuses to apply stale write and emits a new proposal with the conflict.

E-18 Productivity subagent: heartbeat consumer and hybrid heuristic (Nano)

Start: Heartbeats exist but not used autonomously.

End: Productivity (Nano) worker that:

consumes user:{id}:in:prod heartbeats,

maintains hybrid heuristic and internal expected-apps for the current DayPlan block,

uses inputs:

active DayPlan block (label, priority, notes),

local memory of preferred tools/apps,

recent foreground app history and time-of-day,

never writes expected-apps to WB, never writes raw heartbeats to WB.

Test: With heartbeats flowing and a DayPlan in place, logs show heuristic updates per heartbeat; WB remains empty until a decision rule triggers.

E-19 Productivity timers and decision emission (Nano)

Start: No stable mismatch logic.

End:

For each active block:

track mismatch time when foreground app not in internal expected-apps,

reset timer on match,

when mismatch ≥120 seconds (plus small jitter), emit exactly one decision to WB:

prod.underrun or prod.overrun (optionally prod.nudge),

enforce cooldown so repeated mismatch does not spam WB.

Test: Keep a non-expected app in foreground for >120s during a “coding” block → exactly one prod.underrun or prod.overrun event appears on WB; returning to expected apps resets timer.

E-20 Productivity block boundaries and recompute triggers

Start: Heuristic may drift when plan/time changes.

End: Productivity recomputes its internal heuristic and expected-apps when:

time crosses into a new DayPlan block,

Manager sends a prod.recompute internal signal (after re-plan),

a calendar delta directly affects the current block.

Test: Trigger a re-plan via Manager → logs show a prod.recompute control message and an internal recompute; WB shows no extra events unless mismatch rules later fire.

E-21 Productivity control channel and Manager integration

Start: prod_recalc_signal concept not wired.

End:

Define a control channel, for example Redis user:{id}:control:prod or an HTTP endpoint.

Manager’s prod_recalc_signal node writes prod.recompute {plan_id, version, block_id} messages to this channel.

Productivity loop listens and recomputes its heuristic for the specified block on receipt.

Test: Send a synthetic prod.recompute message without new heartbeats → Productivity recomputes (logged) and does not write anything to WB unless conditions later warrant a decision.

E-22 Email-Triage subagent: stream consumer loop (Nano)

Start: Poller writes to user:{id}:in:email, no automatic triage.

End: Email-Triage worker that:

consumes user:{id}:in:email,

de-dupes by {messageId, internalDate},

classifies needs-reply vs FYI,

generates short summary and draft,

emits email.reply_needed {message_id, sender, summary, draft} once per message to WB when a reply is appropriate.

Test: Send a clear “Can you join at 3pm?” email → within 30–60s a single email.reply_needed appears on WB.

E-23 Email send path (decisions → Gmail send)

Start: Drafts exist but are never sent autonomously.

End:

When user confirms “Send” on a prompt, Manager writes an internal email.send.confirmed {message_id, draft_hash} event to a mailer stream (not to WB).

A mailer worker consumes that stream and calls Gmail send API with idempotency key (messageId, draft_hash).

Replaying the same event does not send a second email.

Test: Accept “Send” on an email.reply_needed prompt → exactly one email is sent; re-injecting the same email.send.confirmed event yields no extra send.

E-24 Backpressure, backoff, and rate limits for subagents

Start: Subagents may hammer downstream APIs.

End: For Calendar-Planner, Productivity, Email-Triage, and mailer:

process input streams in small batches (for example up to 10 items),

use exponential backoff for Planner/LLM/Gmail errors (429/5xx),

enforce per-minute and per-hour call caps.

Test: Simulate Planner or Gmail failure → logs show backoff and capped call rates; streams still drain eventually.

E-25 Subagent health, lag, and liveness

Start: No per-subagent visibility.

End: Each subagent exposes metrics:

lag (oldest pending message age),

throughput (items per second),

WB decision rate,

error rate,

liveness probe.

Test: Pause a subagent loop for 60 seconds → lag metric grows; resume → lag returns to normal; liveness check stays true.

E-26 Crash-safe resume and auto-claim for subagents

Start: Manual recovery needed on crashes.

End: For each stream:

use Redis consumer groups and XAUTOCLAIM to reassign stuck messages,

on startup, subagents resume from last known stream ID.

Test: Kill a subagent worker mid-batch → another worker auto-claims the items and processes them; no duplicate WB events.

E-27 Midnight and DST rollovers

Start: Timers and blocks may misbehave on clock jumps.

End:

At local midnight or DST change:

reload or recompute DayPlan for the new day,

reset Productivity timers,

trigger an internal prod.recompute for the first block of the day.

Test: Simulate a local time jump → first block after jump is correct; timers reset; no spurious WB decisions.

E-28 Subagent local state GC and decay

Start: Heuristic/history state can grow without bound.

End: Each subagent:

retains only last 24 hours (or N items) of heuristic history,

prunes older history on a schedule,

ensures GC runs even when input is low.

Test: Run system for >24 hours with synthetic load → memory usage plateaus; debug endpoints show no per-subagent state older than retention window.

E-29 Error buckets and degraded mode per subagent

Start: Only backpressure exists; no self-protection.

End:

Each subagent tracks error rate over a short window.

If error rate stays above threshold (for example >20% for 60 seconds), subagent:

enters “degraded mode,” logs a clear warning,

reduces or pauses optional LLM/API calls,

continues to drain streams but may skip non-critical decisions until errors drop.

Test: Force persistent upstream errors → logs show degraded mode; WB decisions drop; once errors resolve, normal behavior resumes.

E-30 Policy toggles (safe autonomy)

Start: Subagent behavior is hard-wired.

End: Config flags:

Productivity: enable/disable prod.nudge, configure mismatch threshold and cooldown.

Calendar-Planner: “propose-only” (default) vs “auto-apply trivial changes” (still opt-in and off by default).

Email-Triage: cap emails triaged and emails sent per hour.

Test: Change config values; restart subagent processes if needed; verify behavior updates and defaults remain conservative (no auto-changes without explicit opt-in).

E-31 Soak test for full E stack

Start: No long-run validation of Manager + subagents together.

End: A 60-minute soak script that:

generates synthetic heartbeats with match → mismatch → match patterns,

applies a few calendar changes,

sends multiple emails,

drives user actions (manager.user_action) for each decision path.
Collects:

decision counts per type,

planner and mailer call counts,

lag histograms,

CPU and memory usage.

Test: Run the soak; verify:

exactly one WB decision per rule trigger,

no duplicate sends,

Manager and subagents stay healthy and within resource limits.

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
