# WAPulse — Master Project Roadmap & Implementation Tasks (v2)

> **v2 changelog:** Phases 1–2 are carried over as-completed from v1.
> Phase 3 is restructured around the extraction/template split. New tasks
> are added for ingestion idempotency (3.x → 4.x), SQLite concurrency
> config (2.1.1 amended), and a new **Phase 7: Operational Readiness**
> covering autostart, tray, logging, session monitoring, and updates —
> none of which existed in v1. Changed/new items are marked **[v2]**.

## Architecture Summary

WAPulse runs as a unified single-process application built on Go, Wails
v2, SQLite (WAL mode), and whatsmeow. Client business logic is isolated
from the core messaging pipeline via two mechanisms: **compile-time**
build tags select which client driver runs (doctor vs. wholesaler), and
**runtime** user-editable templates control what messages say — see the
System Design Document v2 for why both are needed.

- **Pipeline 1: Ingestion & Scheduling** — fsnotify watcher with debounce,
  startup catch-up scan, SHA-256 dedupe, 30s cron/reminder ticker.
- **Pipeline 2: Client Plugin Engine** — extraction-only client drivers
  (Go build tags) feeding a shared runtime template renderer.
- **Pipeline 3: Universal Action Dispatcher** — outbox queue with jittered
  backoff for WhatsApp delivery and native desktop notifications.

---

## Phase 1: Foundation & Project Scaffolding

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 1.1.1 | Verify macOS/Windows Dev Toolchain | N/A | **Completed** | Verified Go, Wails CLI, and NPM setup. |
| 1.1.2 | Initialize Wails App Scaffold | wapulse/* | **Completed** | Scaffolded project using Wails vanilla template; dev server verified. |
| 1.1.3 | Add Core Go Dependencies | go.mod, go.sum | **Completed** | Installed modernc.org/sqlite, go.mau.fi/whatsmeow, github.com/fsnotify/fsnotify. |

## Phase 2: Database Layer & Data Access Objects (DAO)

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 2.1.1 | Create DB Init Service **[v2: amended]** | internal/db/db.go | **Needs Revisit** | SQLite connection wrapper with WAL, foreign keys, `PRAGMA busy_timeout=5000`, and `SetMaxOpenConns(1)` on the write connection. Original task didn't set busy_timeout or cap write connections — add before Phase 4 introduces concurrent writers. |
| 2.1.2 | Core Schema Migration Pipeline **[v2: amended]** | internal/db/migration.go | **Needs Revisit** | Add `processed_files` and `message_templates` tables (see Design Doc v2 §6) alongside the original three. |
| 2.1.3 | Outbox Queue DAO | internal/db/outbox_dao.go | **Completed** | EnqueueAction, FetchPendingActions, MarkActionSent, MarkActionFailedWithBackoff, with tests. |
| 2.1.4 | Scheduled Rules DAO | internal/db/schedule_dao.go | **Completed** | CreateScheduledRule, FetchDueRules, UpdateRuleStatus, with tests. |
| 2.1.5 | Config & Watermark DAO | internal/db/config_dao.go | **Completed** | Key-value config getters/setters for high-watermark timestamps. |
| 2.1.6 | Processed-Files & Template DAO **[v2: new]** | internal/db/ingestion_dao.go, internal/db/template_dao.go | **Pending** | `HasProcessedHash`, `MarkProcessed` (transactional with outbox insert + watermark update); `GetTemplate`, `UpsertTemplate` for the template engine. |

## Phase 3: Domain Plugin Contract & Client Driver Adapters

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 3.1.1 | Define Core Plugin Interface **[v2: revised]** | internal/plugin/plugin.go | **Pending** | Define `ExtractedEvent` struct and `DomainPlugin` interface (Init, OnFileDiscovered, OnScheduledTrigger) that returns structured data only — **no message text**. See Design Doc v2 §5. |
| 3.1.2 | Wholesaler Plugin Driver | internal/plugin/wholesaler.go | **Pending** | Parses bills.pdf, extracts customer phone/balance, returns `ExtractedEvent` with attachment path. Does not construct WhatsApp text. |
| 3.1.3 | Doctor Plugin Driver | internal/plugin/doctor.go | **Pending** | Parses patient notes, extracts medicine summary and next-visit interval, returns `ExtractedEvent`(s) for both the immediate message and the scheduled reminder rule. Executes plugin-specific SQLite tables (patient/visit history). |
| 3.1.4 | Plugin Registry & Build Tags | internal/plugin/registry.go | **Pending** | Wire active driver initialization based on Go build tags (`-tags doctor` vs `-tags footwear`), unchanged from v1. |
| 3.2.1 | Template Rendering Engine **[v2: new]** | internal/template/renderer.go | **Pending** | Given an `ExtractedEvent` and its `event_type`, look up the row in `message_templates`, render with `text/template`, return final message body. Missing-template case falls back to a safe default and logs a warning. |
| 3.2.2 | Default Template Seeding **[v2: new]** | internal/plugin/*/templates.go | **Pending** | Each plugin ships default template rows inserted on first migration, so the app is usable before a client edits anything. |

## Phase 4: Ingestion & Scheduling Pipeline

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 4.1.1 | Folder Watcher Daemon | internal/watcher/watcher.go | **Pending** | fsnotify watcher monitors target directories, forwards file paths downstream. |
| 4.1.2 | File Stability Debounce **[v2: new]** | internal/watcher/debounce.go | **Pending** | Before forwarding a file event, poll size/mtime until unchanged for a configurable quiet window (default 2s) to avoid partial reads and collapse duplicate fsnotify events (esp. on Windows). |
| 4.1.3 | Ingestion Dedupe Gate **[v2: new]** | internal/watcher/dedupe.go | **Pending** | Hash file (SHA-256), check against `processed_files`; skip if already seen. On success, run plugin → template → outbox insert → `processed_files` insert → watermark update **as a single SQLite transaction**, so a crash mid-pipeline can't produce duplicates or silent drops. |
| 4.1.4 | Catch-Up Directory Scanner | internal/watcher/scanner.go | **Pending** | Startup file tree traversal comparing mtime against last_scan_timestamp for files missed during downtime. Routes through the same dedupe gate (4.1.3). |
| 4.1.5 | Scheduler & Recurring Rule Ticker | internal/scheduler/ticker.go | **Pending** | 30s background ticker checks `scheduled_notifications`, invokes plugin `OnScheduledTrigger`, routes result through the template engine (3.2.1), enqueues into `message_outbox`. |

## Phase 5: Action Dispatcher Pipeline

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 5.1.1 | WhatsApp Sender Interface **[v2: revised]** | internal/whatsapp/sender.go | **Pending** | Define a `Sender` interface (Send, SendMedia, ConnectionState) so whatsmeow is a swappable implementation rather than a hard dependency — see Design Doc v2 §2. |
| 5.1.2 | WhatsApp Client Lifecycle & Session Store | internal/whatsapp/client.go | **Pending** | whatsmeow implementation of `Sender`: SQLite session store, auto-reconnect, QR stream events for the Wails frontend. |
| 5.1.3 | Outbox Queue Worker & Anti-Spam Rate Limiter **[v2: amended]** | internal/queue/worker.go | **Pending** | Polling worker fetching PENDING actions, 5–15s jitter between sends, **daily send cap per client** (configurable in app_config), exponential backoff on transient errors, SENT/FAILED_PERMANENT status updates. |
| 5.1.4 | OS Native Desktop Alert Dispatcher | internal/notify/notifier.go | **Pending** | Handler for DESKTOP_NOTIFICATION outbox actions; native macOS/Windows notifications. |
| 5.1.5 | Media Document Attachment Processor | internal/whatsapp/media.go | **Pending** | File reader/media uploader generating WhatsApp document payloads for PDFs/images. |

## Phase 6: Wails Desktop Frontend & UI

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 6.1.1 | Live Outbox Queue Status Dashboard | frontend/src/* | **Pending** | Real-time queue monitoring UI: pending, sent, retrying actions. |
| 6.1.2 | WhatsApp QR Authentication Screen | frontend/src/* | **Pending** | Renders live QR stream from whatsmeow for device pairing. |
| 6.1.3 | Watched Folders & Manual Reminders UI | frontend/src/* | **Pending** | UI to configure watch directories and manually schedule reminders. |
| 6.1.4 | Message Template Editor **[v2: new]** | frontend/src/* | **Pending** | UI for editing `message_templates` rows per event type — this is what actually delivers on client-customizable messages, per Requirement 3. |
| 6.1.5 | Connection Health Indicator **[v2: new]** | frontend/src/* | **Pending** | Tray/UI badge reflecting WhatsApp session state (connected / disconnected / needs re-auth), sourced from 5.1.2's connection events. |

## Phase 7: Operational Readiness **[v2: new phase]**

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 7.1.1 | System Tray & Minimize-to-Tray | internal/app/tray.go | **Pending** | App runs minimized to tray on close; tray menu exposes open/quit and shows connection health (6.1.5). |
| 7.1.2 | Autostart on Login | internal/app/autostart.go | **Pending** | Registers app to launch on Windows login (registry Run key or startup shortcut), toggleable from settings. |
| 7.1.3 | Structured Rotating Logging | internal/logging/logger.go | **Pending** | Rotating log file under `%APPDATA%/WAPulse/logs`, covering ingestion, dispatch, and errors; log level configurable. |
| 7.1.4 | WhatsApp Session Health Monitor | internal/whatsapp/health.go | **Pending** | Detects logout/disconnect events from whatsmeow, surfaces persistent notification when action is needed (re-scan QR), rather than silently accumulating a growing outbox. |
| 7.1.5 | Installer & Update Strategy | build/*, docs/updates.md | **Pending** | Decide and implement update delivery (manual reinstall vs. version-check prompt vs. auto-updater) before first client rollout; document the choice and rationale. |
