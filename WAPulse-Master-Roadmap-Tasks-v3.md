# WAPulse — Master Project Roadmap & Implementation Tasks (v3)

> **v3 changelog:** Removes every whatsmeow/QR-pairing task. Replaces them
> with official WhatsApp Business Platform (Cloud API) integration, called
> directly from the client's desktop using that client's own WABA
> credentials — no server of yours involved. Adds template
> submission/approval tracking. New/changed items marked **[v3]**.

## Architecture Summary

WAPulse runs as a unified single-process application built on Go, Wails
v2, and SQLite (WAL mode). Client business logic is isolated from the core
messaging pipeline via compile-time build tags (which client driver runs)
plus runtime, Meta-approved message templates (what messages say).
WhatsApp delivery goes through the **official Cloud API**, called directly
from the client's own machine with the client's own WABA credentials —
never through any infrastructure you operate.

- **Pipeline 1: Ingestion & Scheduling** — fsnotify watcher with debounce,
  startup catch-up scan, SHA-256 dedupe, 30s cron/reminder ticker.
- **Pipeline 2: Client Plugin Engine** — extraction-only client drivers
  (Go build tags) feeding a shared template renderer.
- **Pipeline 3: Universal Action Dispatcher** — outbox queue sending via
  the WhatsApp Cloud API (approved templates only) and native desktop
  notifications.

---

## Phase 1: Foundation & Project Scaffolding

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 1.1.1 | Verify macOS/Windows Dev Toolchain | N/A | **Completed** | Verified Go, Wails CLI, and NPM setup. |
| 1.1.2 | Initialize Wails App Scaffold | wapulse/* | **Completed** | Scaffolded project using Wails vanilla template; dev server verified. |
| 1.1.3 | Add Core Go Dependencies **[v3: amended]** | go.mod, go.sum | **Completed** | Drop `go.mau.fi/whatsmeow`. Keep `modernc.org/sqlite`, `github.com/fsnotify/fsnotify`. No new WhatsApp-specific dependency needed — the Cloud API is called via Go's standard `net/http`. |

## Phase 2: Database Layer & Data Access Objects (DAO)

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 2.1.1 | Create DB Init Service | internal/db/db.go | **Completed** | WAL, foreign keys, `PRAGMA busy_timeout=5000`, `SetMaxOpenConns(1)` on the write connection. (Unchanged from v2.) |
| 2.1.2 | Core Schema Migration Pipeline **[v3: amended]** | internal/db/migration.go | **Completed** | Add `processed_files` and the **revised** `message_templates` table (Meta approval lifecycle — see Design Doc v3 §6), plus the revised `message_outbox` columns (`template_name`, `template_vars_json`, `AWAITING_TEMPLATE` status). |
| 2.1.3 | Outbox Queue DAO | internal/db/outbox_dao.go | **Completed** | EnqueueAction, FetchPendingActions, MarkActionSent, MarkActionFailedWithBackoff, with tests. |
| 2.1.4 | Scheduled Rules DAO | internal/db/schedule_dao.go | **Completed** | CreateScheduledRule, FetchDueRules, UpdateRuleStatus, with tests. |
| 2.1.5 | Config & Watermark DAO **[v3: amended]** | internal/db/config_dao.go | **Completed** | Key-value config getters/setters, now also storing **encrypted** per-client `waba_id`, `phone_number_id`, and access token (see 7.1.6). |
| 2.1.6 | Processed-Files & Template DAO | internal/db/ingestion_dao.go, internal/db/template_dao.go | **Completed** | `HasProcessedHash`, `MarkProcessed` (transactional with outbox insert + watermark update); `GetTemplate`, `UpsertTemplate`, `UpdateApprovalStatus`. |

## Phase 3: Domain Plugin Contract & Client Driver Adapters

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 3.1.1 | Define Core Plugin Interface | internal/plugin/plugin.go | **Completed** | `ExtractedEvent` struct and `DomainPlugin` interface — structured data only, no message text. Unchanged from v2. |
| 3.1.2 | Wholesaler Plugin Driver | internal/plugin/wholesaler.go | **Completed** | Parses bills.pdf, extracts customer phone/balance, returns `ExtractedEvent` with attachment path. |
| 3.1.3 | Doctor Plugin Driver | internal/plugin/doctor.go | **Pending** | Parses patient notes, extracts medicine summary and next-visit interval, returns immediate + scheduled `ExtractedEvent`s. |
| 3.1.4 | Plugin Registry & Build Tags | internal/plugin/registry.go | **Pending** | Wire active driver via Go build tags (`-tags doctor` vs `-tags footwear`). |
| 3.2.1 | Template Rendering Engine **[v3: revised]** | internal/template/renderer.go | **Pending** | Given an `ExtractedEvent`, look up the `APPROVED` template for its event type, map `Data` fields to the template's `{{1}}, {{2}}, ...` variables per `variable_order`. If no approved template exists, mark the outbox row `AWAITING_TEMPLATE` instead of sending. |
| 3.2.2 | Default Template Seeding **[v3: amended]** | internal/plugin/*/templates.go | **Pending** | Each plugin ships default template bodies as `DRAFT` rows — they still need to go through submission (5.2.2) and Meta approval before use; first-run UX should make this obvious. |

## Phase 4: Ingestion & Scheduling Pipeline

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 4.1.1 | Folder Watcher Daemon | internal/watcher/watcher.go | **Pending** | fsnotify watcher monitors target directories, forwards file paths downstream. |
| 4.1.2 | File Stability Debounce | internal/watcher/debounce.go | **Pending** | Poll size/mtime until unchanged for a quiet window (default 2s) before forwarding. |
| 4.1.3 | Ingestion Dedupe Gate | internal/watcher/dedupe.go | **Pending** | SHA-256 hash check against `processed_files`; plugin → template lookup → outbox insert → `processed_files` insert → watermark update as one transaction. |
| 4.1.4 | Catch-Up Directory Scanner | internal/watcher/scanner.go | **Pending** | Startup traversal vs. `last_scan_timestamp`, routed through the same dedupe gate. |
| 4.1.5 | Scheduler & Recurring Rule Ticker | internal/scheduler/ticker.go | **Pending** | 30s ticker checks `scheduled_notifications`, invokes plugin, routes through template engine, enqueues into `message_outbox`. |

## Phase 5: Action Dispatcher Pipeline

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 5.1.1 | WhatsApp Cloud API Sender **[v3: replaces whatsmeow client]** | internal/whatsapp/sender.go | **Pending** | `Sender` interface implementation using `net/http` against `graph.facebook.com`: send-template-message call, using the active client's `phone_number_id` + access token from config. No session state to manage — it's a stateless authenticated HTTP call per send. |
| 5.1.2 | Rate-Limit & Quality-Rating Awareness **[v3: replaces daily send cap]** | internal/whatsapp/sender.go | **Pending** | Parse Meta's rate-limit/quality-rating response codes; on throttling, back off and resume rather than treating it as a permanent failure. Replaces v2's self-imposed daily cap — Meta's messaging tier is the real limit. |
| 5.1.3 | Outbox Queue Worker | internal/queue/worker.go | **Pending** | Polling worker fetching PENDING actions past `next_retry_at`; distinguishes transient (rate-limited → retry) from permanent (template rejected/disabled → surface to user, don't retry) failures. |
| 5.1.4 | OS Native Desktop Alert Dispatcher | internal/notify/notifier.go | **Pending** | Handler for DESKTOP_NOTIFICATION outbox actions; native macOS/Windows notifications. |
| 5.1.5 | Media Document Attachment Processor | internal/whatsapp/media.go | **Pending** | Uploads PDFs/images to Meta's media endpoint, attaches the resulting media ID to the template send call. |
| 5.2.1 | WABA Credential Setup Flow **[v3: replaces QR auth]** | internal/whatsapp/onboarding.go | **Pending** | Desktop flow for entering/validating a client's `waba_id`, `phone_number_id`, and access token (obtained via Meta Business Manager during per-client onboarding — see Design Doc v3 §2.4); test call to confirm credentials work before saving. |
| 5.2.2 | Template Submission to Meta | internal/whatsapp/templates.go | **Pending** | Graph API call to submit a `DRAFT` template for review; stores Meta's returned template ID; a polling or webhook-based check updates `approval_status` to `APPROVED`/`REJECTED` (with `rejection_reason` on reject). |

## Phase 6: Wails Desktop Frontend & UI

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 6.1.1 | Live Outbox Queue Status Dashboard **[v3: amended]** | frontend/src/* | **Pending** | Real-time queue monitoring: pending, sent, retrying, and **awaiting template** actions. |
| 6.1.2 | WABA Setup Screen **[v3: replaces QR screen]** | frontend/src/* | **Pending** | Guided entry of `waba_id`/`phone_number_id`/access token per Design Doc v3 §2.4, with an inline test-send to confirm setup before going live. |
| 6.1.3 | Watched Folders & Manual Reminders UI | frontend/src/* | **Pending** | UI to configure watch directories and manually schedule reminders. |
| 6.1.4 | Message Template Editor & Approval Tracker **[v3: amended]** | frontend/src/* | **Pending** | Edit template body/variables, submit for Meta review, and show live `DRAFT / PENDING_REVIEW / APPROVED / REJECTED` status with rejection reasons. |
| 6.1.5 | Credential & Delivery Health Indicator **[v3: replaces connection-health badge]** | frontend/src/* | **Pending** | Tray/UI badge reflecting token validity, quality rating, and any templates needing attention — sourced from 7.1.4. |

## Phase 7: Operational Readiness

| Task ID | Task Name | Target Files | Status | Description & Acceptance Criteria |
| --- | --- | --- | --- | --- |
| 7.1.1 | System Tray & Minimize-to-Tray | internal/app/tray.go | **Pending** | App runs minimized to tray on close; tray menu exposes open/quit and shows health status (6.1.5). |
| 7.1.2 | Autostart on Login | internal/app/autostart.go | **Pending** | Registers app to launch on Windows login, toggleable from settings. |
| 7.1.3 | Structured Rotating Logging | internal/logging/logger.go | **Pending** | Rotating log file under `%APPDATA%/WAPulse/logs`, covering ingestion, dispatch, and errors. |
| 7.1.4 | Credential & Delivery Health Monitor **[v3: replaces WhatsApp session health monitor]** | internal/whatsapp/health.go | **Pending** | Detects token expiry/revocation, template rejection/disablement, and quality-rating drops; surfaces each distinctly in the UI/tray rather than letting the outbox grow silently. |
| 7.1.5 | Installer & Update Strategy | build/*, docs/updates.md | **Pending** | Decide and implement update delivery (manual reinstall vs. version-check prompt vs. auto-updater) before first client rollout. |
| 7.1.6 | Credential Encryption at Rest **[v3: new]** | internal/db/db.go | **Pending** | Evaluate and, if adopted, implement SQLCipher (or equivalent) so each client's WABA access token — and, for the doctor build, patient data — isn't stored in a plain-text SQLite file. See Design Doc v3 §8. |
