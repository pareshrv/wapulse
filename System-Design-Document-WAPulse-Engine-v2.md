# System Design Document: WAPulse Engine (v2)

> **v2 changelog (read this first):** This revision fixes four structural
> problems found in v1 during design review:
> 1. Split the plugin contract into **extraction** (compile-time, per client
>    type) vs. **message templating** (runtime, user-editable) — v1's
>    build-tag-only plugins couldn't support client-customizable messages
>    without a recompile, which contradicts a stated future requirement.
> 2. Added an explicit **ingestion idempotency & debounce** design — v1's
>    doc mentioned SHA-256 hashing but never implemented it anywhere in the
>    task list.
> 3. Added **SQLite concurrency rules** (single writer, busy_timeout) — v1
>    had three concurrent writers (watcher, scheduler, worker) and no
>    locking strategy.
> 4. Added an **operational readiness section** (autostart, tray, logging,
>    session-health monitoring, updates) — an unattended desktop watcher
>    silently failing is worse than it crashing.
> Also flags the **WhatsApp automation risk** up front, since it's a
> business risk, not just a technical one.

---

## 1. System Overview

WAPulse is a single-binary Windows desktop engine (macOS later, same
codebase) that watches a client-configured folder for file changes, runs a
client-specific plugin to extract structured data from those files, and
dispatches WhatsApp messages and OS desktop notifications based on
user-editable templates.

### Core Architectural Guarantees

- **Zero Cloud Dependency** — runs entirely on the client's machine, SQLite
  (WAL mode) as the only datastore.
- **Compile-Time Driver Isolation, Runtime Message Customization** — which
  client *type* runs (doctor vs. wholesaler) is chosen at build time via Go
  build tags; what the *messages say* is stored in SQLite and editable from
  the UI without a rebuild. (This is the key fix from v1 — see §5.)
- **Idempotent Ingestion** — every file event is deduplicated by SHA-256
  content hash before it can produce an outbound action, and the
  read → enqueue → watermark-update sequence is one SQLite transaction.
- **At-Least-Once, Rate-Limited Delivery** — outbox pattern with jittered
  backoff; delivery volume is capped to reduce ban risk (see §2).

### Explicit Non-Goals (for now)

- Multi-tenant / multi-client-per-binary. Each install serves one client.
- Group messaging or WhatsApp broadcast lists.
- Any server component. If a client wants a web dashboard later, that's a
  separate project reading the same SQLite file, not part of this engine.

---

## 2. Critical Risk: WhatsApp Delivery via `whatsmeow`

`whatsmeow` implements WhatsApp's personal multi-device protocol — it is
**not** the official WhatsApp Business Platform (Cloud) API. This is the
only way to get zero-cloud, self-hosted, no-per-message-cost delivery, but
it carries real operational risk:

- WhatsApp actively detects automated/bulk sending patterns and can ban the
  linked number, sometimes without warning.
- There is no SLA or support channel — a protocol change on WhatsApp's side
  can break the library until it's patched upstream.

**Mitigations baked into this design:**

- The dispatcher (`internal/whatsapp`) is defined behind a `Sender`
  interface. `whatsmeow` is one implementation; swapping in the official
  Cloud API for a specific client later is a driver swap, not a rewrite.
- The outbox worker enforces a **daily send cap per client** (configurable,
  default conservative) and **randomized inter-message delay** (5–15s,
  already in v1) rather than bursting.
- Desktop notification (task 5.1.3) is a same-cost fallback channel — if
  WhatsApp delivery is degraded, the client at minimum sees the alert
  locally.
- This risk is disclosed to the client at setup, not silently assumed.

---

## 3. Component Architecture & Data Flow

```
┌─────────────────────────── WAPulse Engine ───────────────────────────┐
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
│         [Template Engine: renders ExtractedEvent + message_templates] │
│                      │                                                │
│                      v                                                │
│              [message_outbox] <──── [30s Scheduler/Ticker]            │
│                      │                     ^                          │
│                      v                     │                          │
│         [Outbox Worker: rate-limited, backoff]                        │
│              │                    │                                   │
│              v                    v                                   │
│   [WhatsApp Sender interface]  [Desktop Notifier]                     │
│        (whatsmeow impl)                                               │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 4. Workflow Pipeline

### 4.1 Ingestion & Scheduling

- `fsnotify` watches configured directories; events are **debounced** —
  wait until the target file's size and mtime are unchanged for a
  configurable quiet period (default 2s) before treating it as final. This
  avoids reading partially-written files and collapses the duplicate
  events fsnotify fires per save on Windows.
- Startup catch-up scan walks the tree and compares file mtime against a
  stored high-watermark, so files changed while the app was closed aren't
  missed.
- Every candidate file is hashed (SHA-256). The hash is checked against
  `processed_files` before the plugin runs — if seen before, skip. This is
  the actual mechanism behind the "idempotent delivery" guarantee v1 named
  but didn't implement.

### 4.2 Client Plugin Engine (Extraction Only)

- A plugin implements `DomainPlugin.OnFileDiscovered(file) → ExtractedEvent`
  and `OnScheduledTrigger(rule) → ExtractedEvent`.
- `ExtractedEvent` is a structured payload (customer phone, event type,
  key-value data like `{medicine: "...", next_visit_days: 7}`, optional
  attachment path). **It must never contain final message text.**
- Plugins may run their own SQLite migrations for client-specific tables
  (e.g., doctor's patient/visit history) but must not write to
  `message_outbox` directly — they hand off to the template engine.

### 4.3 Template Engine (New in v2)

- Message text lives in a `message_templates` table (see §6), one row per
  `(client_type, event_type)`, using Go's `text/template` syntax against
  the `ExtractedEvent` data map.
- The UI (Phase 6) lets the client edit these templates and desktop
  notification text without any code change or rebuild — this is what
  makes future Requirement 3 (custom reminder/notification text) actually
  achievable.
- Default templates ship per plugin as seed data on first run.

### 4.4 Universal Action Dispatcher

- Outbox worker polls `message_outbox` for `PENDING` rows whose
  `next_retry_at` has passed, sends via the `Sender` interface, applies
  5–15s jitter between sends and exponential backoff with a max retry
  count before `FAILED_PERMANENT`.
- Desktop notifications go through a separate lightweight handler — no
  network dependency, so they should basically never fail.

---

## 5. Plugin Contract (Revised)

```go
type ExtractedEvent struct {
    IdempotencyKey string            // derived: hash(filePath) + eventType
    EventType      string            // e.g. "BILL_ISSUED", "VISIT_REMINDER"
    CustomerPhone  string
    AttachmentPath string            // optional, e.g. original PDF
    Data           map[string]string // template variables
}

type DomainPlugin interface {
    Init(db *sql.DB) error
    OnFileDiscovered(filePath string) ([]ExtractedEvent, error)
    OnScheduledTrigger(rule ScheduledRule) ([]ExtractedEvent, error)
}
```

The registry (`internal/plugin/registry.go`) wires the active driver based
on build tags exactly as in v1 — that part of the design was correct and
is unchanged. What changed is the return type: plugins used to be
implicitly responsible for message text; now they only return structured
data, and a shared `internal/template` package renders it.

---

## 6. Database Schema (SQLite, WAL Mode)

```sql
-- 1. Persistent system settings & watermarks (unchanged from v1)
CREATE TABLE IF NOT EXISTS app_config (
    config_key   TEXT PRIMARY KEY,
    config_value TEXT NOT NULL,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 2. Transactional outbox queue (unchanged from v1)
CREATE TABLE IF NOT EXISTS message_outbox (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    idempotency_key TEXT UNIQUE NOT NULL,
    action_type     TEXT NOT NULL,           -- 'WHATSAPP' | 'DESKTOP_NOTIFICATION'
    customer_phone  TEXT,
    message_body    TEXT NOT NULL,
    payload_path    TEXT,
    status          TEXT DEFAULT 'PENDING',  -- 'PENDING' | 'SENT' | 'FAILED_PERMANENT'
    retry_count     INTEGER DEFAULT 0,
    next_retry_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    error_log       TEXT,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    sent_at         DATETIME
);

-- 3. Scheduled notification rules (unchanged from v1)
CREATE TABLE IF NOT EXISTS scheduled_notifications (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_key              TEXT NOT NULL,
    metadata_json         TEXT,
    scheduled_for         DATETIME NOT NULL,
    repeat_interval_days  INTEGER DEFAULT 0,   -- 0 = one-time, >0 = recurring
    status                TEXT DEFAULT 'SCHEDULED', -- 'SCHEDULED' | 'EXECUTED' | 'CANCELLED'
    created_at            DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 4. NEW: Idempotent file ingestion ledger
CREATE TABLE IF NOT EXISTS processed_files (
    file_hash    TEXT PRIMARY KEY,   -- SHA-256 of file content
    file_path    TEXT NOT NULL,
    event_type   TEXT,
    processed_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 5. NEW: User-editable message templates (enables runtime customization)
CREATE TABLE IF NOT EXISTS message_templates (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type   TEXT NOT NULL UNIQUE,  -- matches ExtractedEvent.EventType
    channel      TEXT NOT NULL,         -- 'WHATSAPP' | 'DESKTOP_NOTIFICATION'
    template_body TEXT NOT NULL,        -- Go text/template syntax
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**Indices:** `message_outbox(status, next_retry_at)`,
`scheduled_notifications(status, scheduled_for)`,
`processed_files(file_path)` — all B-Tree, matching v1's intent.

**Concurrency settings (new — must be set in `db.go`):**
- `PRAGMA journal_mode = WAL;`
- `PRAGMA busy_timeout = 5000;`
- Open the write path through a single `*sql.DB` with `SetMaxOpenConns(1)`
  for writers; reads (UI dashboard) can use a separate read-only connection
  pool. This avoids intermittent `database is locked` errors from the
  watcher, scheduler, and worker writing concurrently — WAL allows
  concurrent readers but only one writer at a time.

---

## 7. Operational Readiness (New in v2)

v1 had no plan for keeping the watcher actually running and observable.
For an app whose entire job is "notice things silently in the background,"
these aren't optional:

- **Autostart + system tray:** the app must start on Windows login and run
  minimized to the tray — if it's not running, file changes are missed
  until the next manual launch (the catch-up scanner covers *some* of
  this, but not real-time reminders).
- **Structured logging:** rotating local log file (e.g.
  `%APPDATA%/WAPulse/logs`) covering ingestion, dispatch, and errors —
  needed to debug field issues on a machine you don't have shell access to.
- **WhatsApp session health monitoring:** whatsmeow sessions can be logged
  out remotely (user removes the linked device from their phone). The
  dispatcher must detect this state and surface a persistent desktop
  notification/tray badge rather than silently queuing messages forever.
- **Update mechanism:** since this ships to non-technical clients, decide
  now whether updates are a manual reinstall, a simple version-check +
  download prompt, or a proper auto-updater. This affects installer choice
  in Phase 6.

## 8. Data Sensitivity Note

The doctor plugin stores patient name, phone number, and medicine data in
the local SQLite file. This is a deliberate decision to document, not an
oversight: recommend evaluating `SQLCipher` (encrypted SQLite) for the
doctor build specifically, since it handles health-adjacent data, even
though the wholesaler build may not need it.

## 9. Platform Strategy

Windows first, using Wails v2 (WebView2-based). The same Go codebase
cross-compiles for macOS later — no architectural changes needed, but
budget separate QA time for fsnotify's differing behavior on
Windows/macOS (Windows lacks native recursive directory watching; macOS
FSEvents batches differently) and for macOS notarization/code-signing,
which Windows doesn't require.
