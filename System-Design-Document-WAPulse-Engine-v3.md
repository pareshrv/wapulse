# System Design Document: WAPulse Engine (v3)

> **v3 changelog:** Replaces `whatsmeow` (unofficial protocol) with the
> **official WhatsApp Business Platform (Cloud API)** as the only delivery
> mechanism. Key decisions locked in this revision:
> 1. No QR-pairing / personal-session automation anywhere in the app.
> 2. The Cloud API is called **directly from each client's desktop machine**
>    using that client's own WABA credentials — there is no server of
>    yours in the loop, so "zero cloud dependency on your side" still
>    holds; the only outbound call is client-desktop → Meta's Graph API.
> 3. Each client registers a **brand-new phone number** under their own
>    business name for the WABA, rather than migrating their existing
>    personal/Business-app number. This is Meta's own recommended path
>    (migrating an existing number means deleting that account first and
>    losing its chat history) and it means their current WhatsApp Business
>    app keeps working untouched, on its own number, in parallel.
> 4. Messages sent proactively (bills, visit reminders) require Meta-
>    approved message templates — this changes the Template Editor from
>    "freely edit text" to "edit + submit for approval, track status."

---

## 1. System Overview

WAPulse is a single-binary Windows desktop engine (macOS later, same
codebase) that watches a client-configured folder for file changes, runs a
client-specific plugin to extract structured data, and dispatches
WhatsApp messages (via the official Cloud API) and OS desktop
notifications based on approved, user-editable templates.

### Core Architectural Guarantees

- **No Server of Yours, Anywhere** — the engine runs entirely on the
  client's machine; the *only* network calls it makes are the client's own
  desktop directly to Meta's Graph API (`graph.facebook.com`) using that
  client's own credentials, and nothing passes through infrastructure you
  operate. SQLite (WAL mode) remains the only datastore.
- **Compile-Time Driver Isolation, Runtime Message Customization** — which
  client type runs (doctor vs. wholesaler) is chosen at build time via Go
  build tags; what messages say is stored in SQLite, editable from the UI,
  and submitted to Meta for template approval — no rebuild needed to
  change wording (only to change which fields a plugin extracts).
- **Idempotent Ingestion** — every file event deduplicated by SHA-256
  content hash before it can produce an outbound action; the
  read → enqueue → watermark-update sequence is one SQLite transaction.
- **Compliant, Rate-Respecting Delivery** — outbox pattern with backoff;
  send volume is governed by Meta's own per-account messaging tier, not a
  self-imposed guess.

---

## 2. WhatsApp Delivery: Official Cloud API, Per-Client WABA

### 2.1 Why this replaces whatsmeow

`whatsmeow`, Baileys, WAHA, and similar libraries reverse-engineer
WhatsApp's personal multi-device protocol. WhatsApp's detection systems
flag that protocol signature itself, independent of how carefully volume
is paced — and at 25–500 proactive messages/day per client, that pattern
(business-initiated, templated, to recipients who didn't message first)
is squarely what gets accounts banned within weeks. Given how central a
client's WhatsApp number is to their business, that risk is not
acceptable. The official WhatsApp Business Platform (Cloud API) is the
only channel where ban risk approaches zero *by design* — Meta pre-approves
what gets sent, rather than guessing after the fact whether you're a spammer.

### 2.2 Where the API is called from

Nothing changes about "no cloud on your side": the Go binary running on
the client's desktop makes the HTTPS call to Meta's Graph API directly,
using that client's own long-lived access token and `phone_number_id`.
You never see the message content, the customer's phone number, or the
client's credentials — they never leave the client's machine except in
the direct call to Meta.

### 2.3 Per-client WABA setup — new number, client's own business name

Each client gets their own WhatsApp Business Account (WABA), registered
under **their own business name, on a new phone number** — not their
existing personal or WhatsApp Business app number. This is deliberate,
and matches Meta's own guidance:

- Migrating an existing number to the Cloud API normally requires
  **deleting that WhatsApp account first**, which loses its chat history
  and makes the number unusable in the regular app afterward (unless
  onboarding through a BSP that specifically supports "coexistence" —
  extra cost/complexity for no real benefit here).
- A new number leaves the client's current WhatsApp Business app fully
  intact and usable, on its own number, for whatever they already use it
  for day-to-day. WAPulse's number becomes a dedicated, automation-only
  line — which is also good practice on its own, since it cleanly
  separates "automated bill/reminder" traffic from any human-operated chat.
- The new number needs to be SMS/voice-reachable once for verification and
  otherwise doesn't need a SIM in an active phone — a cheap secondary
  SIM, VoIP number, or eSIM is sufficient.

### 2.4 Onboarding path: direct-to-Meta vs. a BSP

Two ways to get each client their WABA + API access. Worth deciding
deliberately rather than defaulting:

| | Direct (Meta Developer App per client) | Via a BSP (Interakt, Gupshup, Twilio, etc.) |
|---|---|---|
| Extra party in the loop | None | The BSP (sees traffic, charges markup) |
| Cost | Meta's per-conversation rate only | Meta's rate + BSP subscription/markup |
| Setup complexity | Higher — client needs a Meta Business Portfolio, Developer App, business verification | Lower — BSP usually offers guided/embedded signup |
| Template management | Direct Graph API calls (WAPulse can automate this) | Usually via BSP's dashboard/API |
| Fits "no middleman" goal | Best fit | Adds a dependency you were trying to avoid |

**Recommendation:** default to **direct-to-Meta** per client, since it
keeps the architecture consistent with "no cloud/third-party of yours in
the loop" and avoids recurring BSP fees. WAPulse's setup wizard should
walk the client (or you, during onboarding) through creating the Meta
Business Portfolio and WABA once per client. Revisit a BSP only if a
specific client's Meta business verification gets stuck (this does happen
for less-established businesses) — this becomes a per-client fallback
decision, not an architecture-wide one.

### 2.5 Credential storage

Per client, WAPulse stores: `waba_id`, `phone_number_id`, and a long-lived
system-user access token, in `app_config` (see §6). Given this token can
send messages on the client's behalf, it should be encrypted at rest — see
§8, this reinforces the existing SQLCipher recommendation rather than
adding a new one.

### 2.6 Message templates are no longer free-text

Every message this app sends (bill notice, medicine summary, visit
reminder) is business-initiated, not a reply within a 24-hour customer
service window. The Cloud API requires these to use a **pre-approved
message template** — Meta reviews template wording (not the filled-in
variables) before it can be sent. This reshapes the Template Editor:

- A template has a lifecycle: `DRAFT → PENDING_REVIEW → APPROVED |
  REJECTED`. Only `APPROVED` templates can be used by the outbox worker.
- Submission is a Graph API call WAPulse makes on the client's behalf;
  approval typically takes minutes to a couple of days and happens
  asynchronously — the UI needs to reflect "waiting on Meta," not treat a
  save as instantly live.
- Variable fields (`{{1}}`, `{{2}}`, …) map to `ExtractedEvent.Data` the
  same way the old free-text `text/template` did — this is a stricter
  syntax, not a different underlying design.

### 2.7 Rate limits are Meta's, not ours

The Cloud API enforces **messaging tiers** — new numbers start around 250
business-initiated conversations per rolling 24 hours and scale up (1K →
10K → unlimited) automatically based on quality rating and sending
history. At the stated 25–500 PDFs/day, most clients start comfortably
within tier 1 and grow into higher tiers naturally; the outbox worker
should track Meta's returned rate-limit/quality-rating signals (from
response headers/error codes) rather than enforcing an arbitrary
self-imposed cap as v2 specified.

---

## 3. Component Architecture & Data Flow

```
┌─────────────────────────── WAPulse Engine (client desktop) ───────────┐
│                                                                        │
│  [File Watcher]         [Catch-Up Scanner]                            │
│  (fsnotify, debounced)   (mtime vs watermark, on startup)              │
│         │                        │                                    │
│         └───────────┬────────────┘                                    │
│                      v                                                │
│         [Ingestion Gate: SHA-256 dedupe + stability check]            │
│                      │                                                │
│                      v                                                │
│           [Client Plugin: Extraction only]                            │
│         (returns structured ExtractedEvent, no message text)          │
│                      │                                                │
│                      v                                                │
│    [Template Engine: renders ExtractedEvent + APPROVED template]      │
│                      │                                                │
│                      v                                                │
│              [message_outbox] <──── [30s Scheduler/Ticker]            │
│                      │                     ^                          │
│                      v                     │                          │
│         [Outbox Worker: respects Meta rate-limit/tier signals]        │
│              │                    │                                   │
│              v                    v                                   │
└──────────────┼────────────────────┼───────────────────────────────────┘
               v                    v
   [WhatsApp Cloud API]     [OS Desktop Notifier]
   (Meta Graph API, direct   (local, no network)
    HTTPS from this device,
    this client's WABA)
```

---

## 4. Workflow Pipeline

### 4.1 Ingestion & Scheduling (unchanged from v2)

fsnotify watcher with debounce (wait for file size/mtime to settle before
reading), startup catch-up scan against a stored watermark, SHA-256 dedupe
via `processed_files` before any plugin runs.

### 4.2 Client Plugin Engine (unchanged from v2)

Plugins return structured `ExtractedEvent` data only — no message text.
See §5 for the interface, carried over unchanged from v2.

### 4.3 Template Engine (revised for Cloud API)

- `message_templates` now tracks Meta's template name, language, category,
  approval status, and variable mapping — not just a `text/template` string.
- Rendering means: look up the `APPROVED` template for the event type,
  map `ExtractedEvent.Data` fields to the template's numbered variables,
  and hand the Cloud API sender a template name + variable list (Meta
  renders the final text server-side from the approved template).
- If no `APPROVED` template exists for an event type yet, the event is
  held (not silently dropped) and surfaced in the UI as "needs a template."

### 4.4 Universal Action Dispatcher (revised)

- Outbox worker polls `message_outbox` for `PENDING` rows past
  `next_retry_at`, sends via the `Sender` interface (now a Cloud API HTTP
  client, not whatsmeow), backs off on transient errors, and specifically
  handles Meta's rate-limit and quality-rating error codes as a distinct
  retry class (wait-and-resume) versus a permanent template/policy
  rejection (surface to the user, don't blindly retry).
- Desktop notifications are unchanged — same local, no-network handler.

---

## 5. Plugin Contract (unchanged from v2)

```go
type ExtractedEvent struct {
    IdempotencyKey string
    EventType      string
    CustomerPhone  string
    AttachmentPath string
    Data           map[string]string // maps to template variables
}

type DomainPlugin interface {
    Init(db *sql.DB) error
    OnFileDiscovered(filePath string) ([]ExtractedEvent, error)
    OnScheduledTrigger(rule ScheduledRule) ([]ExtractedEvent, error)
}
```

---

## 6. Database Schema (SQLite, WAL Mode)

```sql
-- 1. Persistent system settings & watermarks (unchanged)
CREATE TABLE IF NOT EXISTS app_config (
    config_key   TEXT PRIMARY KEY,
    config_value TEXT NOT NULL,   -- includes per-client waba_id, phone_number_id, encrypted access token
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 2. Transactional outbox queue (revised: template-based, not free text)
CREATE TABLE IF NOT EXISTS message_outbox (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    idempotency_key   TEXT UNIQUE NOT NULL,
    action_type       TEXT NOT NULL,            -- 'WHATSAPP_TEMPLATE' | 'DESKTOP_NOTIFICATION'
    customer_phone    TEXT,
    template_name     TEXT,                     -- Meta-approved template name (WHATSAPP_TEMPLATE only)
    template_vars_json TEXT,                     -- ordered variable values for the template
    message_body      TEXT,                      -- rendered text, kept for DESKTOP_NOTIFICATION / local log
    payload_path      TEXT,
    status            TEXT DEFAULT 'PENDING',    -- 'PENDING' | 'SENT' | 'FAILED_PERMANENT' | 'AWAITING_TEMPLATE'
    retry_count       INTEGER DEFAULT 0,
    next_retry_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    error_log         TEXT,
    created_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
    sent_at           DATETIME
);

-- 3. Scheduled notification rules (unchanged)
CREATE TABLE IF NOT EXISTS scheduled_notifications (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_key              TEXT NOT NULL,
    metadata_json         TEXT,
    scheduled_for         DATETIME NOT NULL,
    repeat_interval_days  INTEGER DEFAULT 0,
    status                TEXT DEFAULT 'SCHEDULED',
    created_at            DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 4. Idempotent file ingestion ledger (unchanged from v2)
CREATE TABLE IF NOT EXISTS processed_files (
    file_hash    TEXT PRIMARY KEY,
    file_path    TEXT NOT NULL,
    event_type   TEXT,
    processed_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 5. Message templates (revised: Meta approval lifecycle, not free text)
CREATE TABLE IF NOT EXISTS message_templates (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type      TEXT NOT NULL UNIQUE,   -- matches ExtractedEvent.EventType
    meta_template_name TEXT,                -- name registered with Meta
    language_code   TEXT DEFAULT 'en',
    category        TEXT,                   -- 'UTILITY' | 'MARKETING' (WhatsApp template category)
    body_text       TEXT NOT NULL,           -- template body with {{1}}, {{2}} placeholders, as submitted
    variable_order  TEXT,                    -- JSON array mapping {{n}} -> ExtractedEvent.Data key
    approval_status TEXT DEFAULT 'DRAFT',    -- 'DRAFT' | 'PENDING_REVIEW' | 'APPROVED' | 'REJECTED'
    rejection_reason TEXT,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**Concurrency settings (unchanged from v2):** `PRAGMA journal_mode=WAL`,
`PRAGMA busy_timeout=5000`, single writer connection (`SetMaxOpenConns(1)`).

---

## 7. Operational Readiness (mostly unchanged from v2, one addition)

- **Autostart + system tray, structured logging, update mechanism:**
  unchanged from v2 — still all needed for an unattended background app.
- **Replaces "WhatsApp session health monitor" with "Credential & Delivery
  Health Monitor":** since there's no session to log out of, the failure
  modes are instead (a) access token expiring/being revoked, (b) template
  getting rejected or a previously-approved template getting disabled, and
  (c) the account's quality rating dropping (which throttles the tier).
  The engine should surface all three in the UI/tray rather than let the
  outbox queue grow silently.

## 8. Data Sensitivity Note (unchanged from v2, now also covers WABA credentials)

The doctor plugin's local SQLite file holds patient name/phone/medicine
data, and now also each client's WABA access token. Recommend evaluating
`SQLCipher` (encrypted SQLite) for both reasons — the credential is
arguably the more urgent one, since a leaked token lets someone send
messages as the client's business.

## 9. Platform Strategy (unchanged from v2)

Windows first via Wails v2 (WebView2-based); same Go codebase
cross-compiles for macOS later. The Cloud API dependency doesn't affect
this — it's a plain HTTPS client on both platforms.
