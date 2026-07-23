# System Design Document: WAPulse Engine (v4)

> **v4 changelog:** Revises §2.3–2.4 based on two things learned during
> client onboarding planning:
> 1. **Coexistence over new-number.** Meta's Coexistence feature lets the
>    Cloud API run on a client's *existing* WhatsApp Business app number,
>    in parallel with the app, with no history loss and no forced
>    migration. This is now the default onboarding path; registering a
>    brand-new number is a fallback, not the primary recommendation.
> 2. **Verification without a website or GST.** Most of these clients are
>    small, informal, village-level businesses with no website and often
>    no GST registration. A website is not required for Meta business
>    verification, and UDYAM registration (free, same-day, minimal
>    paperwork) is a realistic fallback when GST isn't available.
> Everything else carries over unchanged from v3 — plugin architecture,
> ingestion idempotency, template approval flow, SQLite schema, and
> operational readiness are not affected by this revision.

---

## 1. System Overview

WAPulse is a single-binary Windows desktop engine (macOS later, same
codebase) that watches a client-configured folder for file changes, runs a
client-specific plugin to extract structured data, and dispatches
WhatsApp messages (via the official Cloud API) and OS desktop
notifications based on approved, user-editable templates.

### Core Architectural Guarantees

- **No Server of Yours, Anywhere** — the engine runs entirely on the
  client's machine; the only network calls it makes are the client's own
  desktop directly to Meta's Graph API (`graph.facebook.com`) using that
  client's own credentials. SQLite (WAL mode) remains the only datastore.
- **Compile-Time Driver Isolation, Runtime Message Customization** — which
  client type runs (doctor vs. wholesaler) is chosen at build time via Go
  build tags; what messages say is stored in SQLite, editable from the UI,
  and submitted to Meta for template approval.
- **Idempotent Ingestion** — every file event deduplicated by SHA-256
  content hash; read → enqueue → watermark-update is one SQLite
  transaction.
- **Compliant, Rate-Respecting Delivery** — outbox pattern with backoff;
  send volume is governed by Meta's own per-account messaging tier.

---

## 2. WhatsApp Delivery: Official Cloud API, Per-Client WABA

### 2.1 Why this replaces whatsmeow (unchanged from v3)

Unofficial libraries (`whatsmeow`, Baileys, WAHA) get detected and banned
based on protocol signature and behavioral pattern, independent of how
carefully volume is paced. At 25–500 proactive messages/day per client,
that's exactly the pattern that gets flagged. The official Cloud API is
the only channel where ban risk approaches zero by design, because Meta
pre-approves what gets sent.

### 2.2 Where the API is called from (unchanged from v3)

The Go binary on the client's desktop calls Meta's Graph API directly,
using that client's own long-lived access token and `phone_number_id`.
Nothing routes through infrastructure you operate.

### 2.3 Per-Client Setup: Coexistence-First, New Number as Fallback

**Revised from v3.** The default path is now **WhatsApp Coexistence**, a
Meta feature that runs the Cloud API and the WhatsApp Business app on the
*same, already-active* phone number simultaneously:

- The client keeps using their existing WhatsApp Business app exactly as
  before — same contacts, same chat history, same number their customers
  already know and trust.
- WAPulse sends automated bills/reminders through the Cloud API on that
  same number, in parallel. New messages sent or received on either side
  sync both ways in real time.
- Setup goes through Meta's official **Embedded Signup** flow, selecting
  "connect an existing number" — the client authorizes it from their
  phone, no account deletion, no history loss.

**Trade-offs to disclose to each client during onboarding, since they
affect daily use:**

- The client must open the WhatsApp Business app at least once every 14
  days, or the API connection is cut off and needs reactivating.
- A few app features are disabled once Coexistence is active: broadcast
  lists, view-once messages, and edit/delete of sent messages.
- By default, WAPulse's business display name may not show in the chat
  header for *new* customers — they may see the phone number instead —
  unless the client also has a paid Meta Verified subscription. Not
  usually a concern for this client base, but worth setting expectations.

**Fallback: register a new, dedicated number instead of using Coexistence**
when a client would rather keep their personal WhatsApp app usage
completely separate from the automated line, or when a specific number
has onboarding issues. This was the v3 default and remains fully
supported — see §2.3.1.

#### 2.3.1 New-number path (fallback, unchanged mechanics from v3)

A new WABA registered under the client's own business name, on a number
that has never been active on WhatsApp — a cheap secondary SIM or eSIM is
sufficient since it doesn't need to be a daily-use phone.

### 2.4 Business Verification: Documents & Fallback Order (new in v4)

Meta business verification is required before the account can send
business-initiated messages, regardless of which path (§2.3 or §2.3.1) is
used. It does **not** require a website — that's a common misconception.
Given most clients here are small, informal, village-level businesses,
plan verification in this fallback order:

1. **GST certificate** (fastest review, most widely accepted) — use if
   the client already has one.
2. **UDYAM (Udyog Aadhaar) registration** — free, fully online, same-day
   in most cases, and only needs the owner's Aadhaar + PAN. This is the
   realistic default for the majority of clients in this segment who
   don't already have GST.
3. **Utility bill showing the business address** — last resort, weakest
   signal, more likely to need extra manual review time.

Practical rule for whichever document is used: the business name on the
document must exactly match the legal entity name entered in Meta
Business Manager, and any address on the document must match what's
entered there too — mismatches are the most common cause of rejection.
Build this document-collection step into the onboarding checklist (see
the companion Client Onboarding Checklist doc) rather than discovering a
client lacks the right paperwork mid-setup.

### 2.5 Onboarding path: direct-to-Meta vs. a BSP (unchanged from v3, renumbered)

| | Direct (Meta Developer App per client) | Via a BSP (Interakt, Gupshup, Twilio, etc.) |
|---|---|---|
| Extra party in the loop | None | The BSP (sees traffic, charges markup) |
| Cost | Meta's per-conversation rate only | Meta's rate + BSP subscription/markup |
| Setup complexity | Higher — client needs a Meta Business Portfolio, Developer App, business verification | Lower — BSP usually offers guided/embedded signup |
| Template management | Direct Graph API calls (WAPulse can automate this) | Usually via BSP's dashboard/API |
| Fits "no middleman" goal | Best fit | Adds a dependency you were trying to avoid |

Coexistence works under either path — it's a property of the phone
number/WABA setup, not of who runs the Embedded Signup. Default to
direct-to-Meta per §2.5 of v3's reasoning; use a BSP only as a per-client
fallback if a specific client's verification gets stuck.

### 2.6 Credential storage (unchanged from v3, renumbered)

Per client, WAPulse stores `waba_id`, `phone_number_id`, and a long-lived
system-user access token in `app_config`, encrypted at rest (see §8).

### 2.7 Message templates are not free-text (unchanged from v3, renumbered)

Every message this app sends is business-initiated and requires a
Meta-approved template. Templates have a lifecycle
(`DRAFT → PENDING_REVIEW → APPROVED | REJECTED`); only `APPROVED`
templates can be used by the outbox worker.

### 2.8 Rate limits are Meta's, not ours (unchanged from v3, renumbered)

New numbers start around 250 business-initiated conversations per rolling
24 hours and scale up automatically based on quality rating and sending
history — comfortably covering the stated 25–500/day range.

---

## 3. Component Architecture & Data Flow (unchanged from v3)

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
    this client's WABA —
    on their existing number
    via Coexistence, or a
    new dedicated number)
```

---

## 4. Workflow Pipeline (unchanged from v3)

### 4.1 Ingestion & Scheduling

fsnotify watcher with debounce, startup catch-up scan against a stored
watermark, SHA-256 dedupe via `processed_files` before any plugin runs.

### 4.2 Client Plugin Engine

Plugins return structured `ExtractedEvent` data only — no message text.
See §5.

### 4.3 Template Engine

`message_templates` tracks Meta's template name, language, category,
approval status, and variable mapping. If no `APPROVED` template exists
for an event type, the event is held (`AWAITING_TEMPLATE`), not dropped.

### 4.4 Universal Action Dispatcher

Outbox worker polls `message_outbox`, sends via the Cloud API `Sender`,
distinguishes transient (rate-limited → retry) from permanent
(template rejected/disabled → surface to user) failures. Desktop
notifications are a separate local, no-network handler.

---

## 5. Plugin Contract (unchanged from v3)

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

## 6. Database Schema (unchanged from v3)

```sql
-- 1. Persistent system settings & watermarks
CREATE TABLE IF NOT EXISTS app_config (
    config_key   TEXT PRIMARY KEY,
    config_value TEXT NOT NULL,   -- includes per-client waba_id, phone_number_id, encrypted access token
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 2. Transactional outbox queue
CREATE TABLE IF NOT EXISTS message_outbox (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    idempotency_key   TEXT UNIQUE NOT NULL,
    action_type       TEXT NOT NULL,            -- 'WHATSAPP_TEMPLATE' | 'DESKTOP_NOTIFICATION'
    customer_phone    TEXT,
    template_name     TEXT,
    template_vars_json TEXT,
    message_body      TEXT,
    payload_path      TEXT,
    status            TEXT DEFAULT 'PENDING',    -- 'PENDING' | 'SENT' | 'FAILED_PERMANENT' | 'AWAITING_TEMPLATE'
    retry_count       INTEGER DEFAULT 0,
    next_retry_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    error_log         TEXT,
    created_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
    sent_at           DATETIME
);

-- 3. Scheduled notification rules
CREATE TABLE IF NOT EXISTS scheduled_notifications (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_key              TEXT NOT NULL,
    metadata_json         TEXT,
    scheduled_for         DATETIME NOT NULL,
    repeat_interval_days  INTEGER DEFAULT 0,
    status                TEXT DEFAULT 'SCHEDULED',
    created_at            DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 4. Idempotent file ingestion ledger
CREATE TABLE IF NOT EXISTS processed_files (
    file_hash    TEXT PRIMARY KEY,
    file_path    TEXT NOT NULL,
    event_type   TEXT,
    processed_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 5. Message templates
CREATE TABLE IF NOT EXISTS message_templates (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type      TEXT NOT NULL UNIQUE,
    meta_template_name TEXT,
    language_code   TEXT DEFAULT 'en',
    category        TEXT,                   -- 'UTILITY' | 'MARKETING'
    body_text       TEXT NOT NULL,
    variable_order  TEXT,
    approval_status TEXT DEFAULT 'DRAFT',    -- 'DRAFT' | 'PENDING_REVIEW' | 'APPROVED' | 'REJECTED'
    rejection_reason TEXT,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**Concurrency settings (unchanged):** `PRAGMA journal_mode=WAL`,
`PRAGMA busy_timeout=5000`, single writer connection
(`SetMaxOpenConns(1)`).

---

## 7. Operational Readiness (unchanged from v3)

- Autostart + system tray, structured logging, update mechanism.
- Credential & Delivery Health Monitor: token expiry/revocation, template
  rejection/disablement, quality-rating drops, **and now also** loss of
  Coexistence sync (e.g. the 14-day app-inactivity cutoff in §2.3) —
  surfaced distinctly in the UI/tray rather than a silently growing queue.

## 8. Data Sensitivity Note (unchanged from v3)

Recommend evaluating SQLCipher for both patient data (doctor build) and
stored WABA access tokens (all builds).

## 9. Platform Strategy (unchanged from v3)

Windows first via Wails v2 (WebView2-based); same Go codebase
cross-compiles for macOS later.
