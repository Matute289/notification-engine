# Notification Engine — Architecture Specifications

> Implementation reference for the Go notification engine in this repository.
> Mirror to `Notification_System.pdf` (the source design) plus every decision we
> made on top of it.

---

## 1. Overview

The Notification Engine is a Go service that accepts notification requests over
HTTP and delivers them to end users via four channels: **iOS push** (APNs),
**Android push** (FCM), **SMS** (Twilio-class), and **email**
(SendGrid-class). It is built around an asynchronous, queue-backed pipeline:

```
internal services ──► HTTP API ──► message queue ──► per-channel worker ──► provider ──► user device
                                       │
                                       └─► persistence + analytics
```

Locally everything (Postgres, Redis, RabbitMQ, the API, four workers, a
one-shot migration job) runs from a single `docker compose up`. A **mock
provider** logs every send so the full pipeline can be exercised without
real third-party credentials.

### 1.1 Goals

| Goal             | Approach                                                   |
| ---------------- | ---------------------------------------------------------- |
| At-least-once    | DB persistence + retry + DLQ                               |
| Idempotency      | Caller-supplied `event_id` + Redis `SETNX` + DB unique idx |
| Soft real-time   | Async pipeline; workers scale horizontally                 |
| Resilience       | Retry with exponential backoff + dead-letter queue + alert |
| Opt-out          | Per-(user, channel) `notification_settings` row            |
| Anti-fatigue     | Per-(user, channel) hourly rate limit                      |
| Authn            | HMAC-SHA256 (AppKey + AppSecret + signed timestamp)        |
| Observability    | structured `slog` logs, Prometheus metrics, health probes  |
| Testability      | Hexagonal architecture; use cases tested with port fakes   |

### 1.2 Non-goals (v1)

- Real APNs/FCM/Twilio/SendGrid credentials. Adapters exist as mock today;
  real ones slot in via `cmd/worker/main.go::buildProvider` without changing
  the use-case layer.
- Multi-region replication.
- Web push.
- Click/open tracking webhooks (events are recorded by the engine but not
  ingested back from providers).
- Strict exactly-once delivery (we offer at-least-once with dedupe).

### 1.3 Scale targets (from the source design, daily)

10 million mobile push, 1 million SMS, 5 million email.

---

## 2. Hexagonal Layout

The codebase is organised in strict ports-and-adapters style. The two
guarantees this gives us:

1. The **domain** never depends on a database, a queue, an HTTP framework, or
   any other piece of infrastructure.
2. **Use cases** never depend on adapters. They depend on small interfaces
   (ports) defined alongside them in the application layer.

This means business logic is testable without containers, and the technology
choices behind any port (Postgres → MySQL, RabbitMQ → Kafka, Prometheus →
OTel) can change without touching domain or use cases.

```
┌────────────────────── cmd/{api,worker}/main.go ──────────────────────┐
│              composition root: bind concrete adapters                │
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │            adapter/inbound       adapter/outbound              │  │
│  │       ┌──────────────────┐    ┌──────────────────────────┐     │  │
│  │       │ httpapi          │    │ postgres (3 repos)       │     │  │
│  │       │ worker (AMQP)    │    │ redis (RateLimiter,      │     │  │
│  │       └────────┬─────────┘    │        Deduper)          │     │  │
│  │                │              │ rabbitmq (EventPublisher)│     │  │
│  │                ▼              │ provider/mock            │     │  │
│  │       ┌──────────────────┐    │ rendering                │     │  │
│  │       │ app/usecase      │◄───┤ observability (Prometheus)│    │  │
│  │       │  + app/port      │    └──────────────────────────┘     │  │
│  │       └────────┬─────────┘                                     │  │
│  │                ▼                                               │  │
│  │       ┌──────────────────┐                                     │  │
│  │       │ domain (entities,│                                     │  │
│  │       │  state machine,  │                                     │  │
│  │       │  value objects)  │                                     │  │
│  │       └──────────────────┘                                     │  │
│  │                                                                │  │
│  │  platform/{auth,config,observability}  (cross-cutting infra)   │  │
│  └────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────┘
```

Import direction: **domain ← app ← adapter ← cmd**. Never the other way.

### 2.1 Layer responsibilities

| Layer         | Path                                | What lives here                                                                             | Allowed imports                                  |
| ------------- | ----------------------------------- | ------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| Domain        | `internal/domain/`                  | Entities, value objects, sentinel errors, state machine                                     | stdlib + `uuid`                                  |
| Ports         | `internal/app/port/`                | Interfaces describing what use cases need from the outside world                            | `domain`, stdlib                                 |
| Use cases     | `internal/app/usecase/`             | One struct + `Execute` per use case. All orchestration lives here.                          | `domain`, `app/port`, stdlib                     |
| Inbound adp.  | `internal/adapter/inbound/...`      | HTTP / AMQP delivery → use case input. No business logic.                                   | `app/usecase`, `domain`, `platform`, third-party |
| Outbound adp. | `internal/adapter/outbound/...`     | Concrete implementations of ports (Postgres, Redis, RabbitMQ, Prometheus, mock provider).   | `app/port`, `domain`, third-party                |
| Platform      | `internal/platform/...`             | Cross-cutting infra not behind a port: HMAC verifier, env config loader, slog logger        | stdlib + third-party                             |
| Composition   | `cmd/{api,worker}/main.go`          | Wire concrete adapters into use cases. **Only place adapters meet ports.**                  | everything                                       |

Each outbound adapter has a compile-time check `var _ port.X = (*Y)(nil)` so a
drift between port and implementation breaks `go build`, not production.

---

## 3. Domain Model

The domain is the heart of the system. It has zero infrastructure dependencies
and owns its own invariants — adapters cannot put a domain entity into an
illegal state.

### 3.1 Aggregates and value objects

| Element         | Kind          | Purpose                                                                                |
| --------------- | ------------- | -------------------------------------------------------------------------------------- |
| `Notification`  | aggregate     | Central entity. Holds recipient, channel, content, status, attempt count, timestamps.  |
| `Status`        | value object  | `received → enqueued → in_flight → sent` (+ failed/retrying/dead_letter branches).     |
| `Channel`       | value object  | One of `push_ios`, `push_android`, `sms`, `email`.                                     |
| `Recipient`     | value object  | UserID and/or raw destination (Email / Phone / DeviceToken).                           |
| `Email`/`Phone`/`DeviceToken`/`EventID` | value objects | Self-validating wrappers over `string`.                              |
| `Template`      | aggregate     | Versioned subject + body, parameterised with `text/template` (or `html/template`).    |
| `User`          | entity        | Contact info captured at signup.                                                       |
| `Device`        | entity        | One push token (per channel) belonging to a user.                                      |
| `Setting`       | entity        | Per-(user, channel) opt-in flag. Default = opt-in.                                     |

### 3.4 Transactional outbox

To avoid the "row exists, message lost" failure mode, `SubmitNotification`
no longer calls the publisher directly. Instead, it writes a
`notification_log` row plus a matching `notification_outbox` row in one DB
transaction (`TxNotificationRepository.SubmitWithOutbox`). A separate
`outbox-relay` process drains pending outbox rows with
`SELECT … FOR UPDATE SKIP LOCKED` and publishes them via
`EventPublisher.PublishRaw`. Multiple relay replicas are safe.

### 3.2 Notification life-cycle (state machine)

```
                    ┌────────────────────────────────────────┐
                    │                                        │
            ┌──► received ──► enqueued ──► in_flight ──► sent (terminal)
            │       │              │            │  ▲
            │       │              │            │  │
            │       │              ▼            │  └──── (no transitions)
            │       │           failed          │
            │       │              │            │
            │       │              ▼            ▼
            │       └────────► retrying ────► in_flight  (next attempt)
            │                      │
            │                      ▼
            └─────────────► dead_letter (terminal)
```

The set of valid transitions is encoded in `internal/domain/status.go`; the
`Notification.transitionTo` method rejects every other move with
`domain.ErrInvalidStatusTransition`. This means a buggy adapter cannot mark a
`Sent` notification as `Retrying`, for example.

### 3.3 Domain errors

Sentinels exposed by `internal/domain/errors.go`:

`ErrNotFound`, `ErrAlreadyExists`, `ErrInvalidInput`, `ErrOptedOut`,
`ErrRateLimited`, `ErrDuplicateEvent`, `ErrUnauthenticated`,
`ErrInvalidStatusTransition`. Use cases wrap these with context; the HTTP
adapter maps them to status codes via `errors.Is`.

---

## 4. Application Layer

### 4.1 Ports (`internal/app/port/`)

| Port                       | Purpose                                                                                              |
| -------------------------- | ---------------------------------------------------------------------------------------------------- |
| `NotificationRepository`   | Persist + retrieve notifications and analytics events. Also `ListStuckInFlight` for the janitor.     |
| `TxNotificationRepository` | Extends `NotificationRepository` with `SubmitWithOutbox(n, payload)` — atomic notification + outbox. |
| `OutboxRepository`         | `Claim(limit) → ([]OutboxItem, OutboxTx)` for the relay. Uses `FOR UPDATE SKIP LOCKED`.              |
| `TemplateRepository`       | Persist + retrieve templates.                                                                        |
| `UserRepository`           | Read users, devices; upsert devices and settings.                                                    |
| `EventPublisher`           | `Publish` (typed), `PublishRaw` (bytes, used by relay), `Encode` (typed→bytes), `Retry`.             |
| `RateLimiter`              | Token-bucket per key.                                                                                |
| `Deduper`                  | Best-effort idempotency on `event_id`.                                                               |
| `NotificationProvider`     | Final-mile delivery (APNs/FCM/Twilio/SendGrid/Mock).                                                 |
| `TemplateRenderer`         | Render a stored template against caller-supplied vars.                                               |
| `MetricsRecorder`          | Domain-shaped metric emission.                                                                       |
| `Clock`                    | Test-controlled "now".                                                                               |

### 4.2 Use cases (`internal/app/usecase/`)

| Use case                  | Driven by             | Description                                                                                                                                                                                              |
| ------------------------- | --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `SubmitNotification`      | `httpapi`             | Dedupe → opt-out check → rate limit → hydrate recipient (when UserID supplied) → render template → mark enqueued → `Publisher.Encode` → `Notifications.SubmitWithOutbox` (atomic log+outbox) → metric.    |
| `GetNotification`         | `httpapi`             | Read one notification by id.                                                                                                                                                                            |
| `ProcessNotification`     | `worker` (AMQP)       | `MarkInFlight` → call `NotificationProvider` → on success `MarkSent`; on `ErrTransient` republish via `EventPublisher.Retry` (and `MarkRetrying`/`MarkDeadLetter`); on terminal error `MarkDeadLetter`. |
| `RescueStuckNotifications`| `janitor`             | Periodic. Lists `in_flight` rows older than `StuckThreshold`, republishes each, resets status to `enqueued`.                                                                                              |
| `RelayOutbox`             | `outbox-relay`        | Periodic. Claims a batch of pending outbox rows in one TX, calls `Publisher.PublishRaw` for each, marks rows published, commits.                                                                          |
| `RegisterDevice`          | `httpapi`             | Upsert a (user, channel, token) device.                                                                                                                                                                  |
| `UpdateSetting`           | `httpapi`             | Upsert opt-in for a (user, channel).                                                                                                                                                                     |
| `CreateTemplate`          | `httpapi`             | Validate via `domain.NewTemplate` and persist.                                                                                                                                                           |
| `GetTemplate`             | `httpapi`             | Read one template by id.                                                                                                                                                                                 |

`SubmitNotification` is the largest use case; everything else is a thin
orchestration around one or two ports. The persist-then-publish-then-mark
order matters for crash recovery (a row in `received` without a queued
message can be re-driven by a janitor; the inverse never happens).

---

## 5. Adapters

### 5.1 Inbound

| Module                                | Drives                  | Notes                                                                                                                                                                          |
| ------------------------------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `adapter/inbound/httpapi/`            | `Submit`/`Get`/`Create` | chi router, six routes under `/v1`, plus `/healthz`, `/readyz`, `/metrics` outside the auth-protected sub-router. DTOs in `dto.go` keep the JSON shape decoupled from domain.  |
| `adapter/inbound/httpapi/middleware/` | n/a                     | RequestID (UUID per request), Recoverer (panic → 500 + stack log), AccessLog (slog + Prometheus histogram), HMACAuth (verifies `X-App-*` headers).                              |
| `adapter/inbound/worker/`             | `ProcessNotification`   | Bounded-concurrency consumer: `Channel.Qos(prefetch)` → semaphore-limited goroutine pool. On use-case error, `Nack(requeue=true)`; otherwise `Ack`.                            |

### 5.2 Outbound

| Module                                | Implements                                  | Notes                                                                                                                                                                                              |
| ------------------------------------- | ------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `adapter/outbound/postgres/`          | `Notification/Template/UserRepository`      | pgx pool. Aggregates split per-file; one type per port. JSON columns for `recipient` + `variables`.                                                                                                |
| `adapter/outbound/redis/`             | `RateLimiter`, `Deduper`                    | Fixed-window counter via `INCR` + `EXPIRE`; dedupe via `SETNX` + TTL.                                                                                                                              |
| `adapter/outbound/rabbitmq/`          | `EventPublisher`                            | Topology declared by `Setup(channels)` — one work + retry + dead queue per channel; retries use the dead-letter-with-TTL pattern. Wire format uses an explicit `publishedNotification` struct so domain stays stable. |
| `adapter/outbound/provider/mock/`     | `NotificationProvider`                      | Logs every send; an injected `failureRate` exercises the retry branch in demos.                                                                                                                    |
| `adapter/outbound/rendering/`         | `TemplateRenderer`                          | Compiles `text/template` (or `html/template` for email auto-escape); per-id in-process cache.                                                                                                       |
| `adapter/outbound/observability/`     | `MetricsRecorder`                           | Prometheus-backed counter / histogram set. Exposed at `/metrics`. Tests use a no-op fake instead.                                                                                                  |

### 5.3 Platform

| Module                          | Notes                                                                                              |
| ------------------------------- | -------------------------------------------------------------------------------------------------- |
| `platform/auth/`                | `Verifier.Verify(...)` + `Sign(...)`. Constant-time comparison. Skew-bounded timestamp check.      |
| `platform/config/`              | `caarlos0/env` env→struct binding. `RateLimit.AsMap()` produces the channel-keyed map.             |
| `platform/observability/`       | `slog` JSON logger configured by level. (Prometheus impl lives in the outbound adapter.)           |

---

## 6. Data Model

### 6.1 Schema (`migrations/0001_init.sql`)

| Table                   | Purpose                                                                 | Notable columns / indexes                                                                                       |
| ----------------------- | ----------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `users`                 | Contact info (PDF Figure 10-8)                                          | `email`, `country_code`, `phone_number`. Unique partial idx on `LOWER(email)`.                                  |
| `devices`               | Push tokens (one user → many devices)                                   | `(channel, device_token)` unique. Index on `user_id`.                                                           |
| `notification_settings` | Per-channel opt-in                                                      | PK `(user_id, channel)`.                                                                                        |
| `notification_templates`| Versioned, parameterised messages                                       | Unique `(name, channel, locale, version)`.                                                                      |
| `notification_log`      | Authoritative life-cycle row per notification                           | UUID PK, **unique** `event_id` (the dedupe backstop), `status` + `attempt` + `last_error`, `recipient` JSONB.    |
| `analytics_events`      | One-to-many event timeline (sent / dead_letter / click / unsubscribe)   | FK to `notification_log`, JSONB `metadata`.                                                                     |

`migrations/0002_seed.sql` registers a demo user, two devices, three
templates so a freshly-booted stack accepts a meaningful POST immediately.

---

## 7. Queue Topology (RabbitMQ)

Provisioned by `adapter/outbound/rabbitmq/topology.go::Setup`:

```
exchange "notifications" (topic, durable)
   ├── queue notifications.<channel>          (work queue, dead-letters to retry)
   ├── queue notifications.<channel>.retry    (TTL backoff; dead-letters back to work)
   └── queue notifications.<channel>.dead     (terminal)
```

Backoff schedule (per attempt index): **30s → 2min → 10min → 1h → 6h**, then
the message is published to the terminal `dead` queue and the notification is
marked `dead_letter`. The schedule is in `BackoffSchedule` and `MaxRetries`
defaults to 5 (env-overridable).

The retry hop count is carried on a `x-attempt` header so workers don't have
to consult Postgres for it.

---

## 8. HTTP API (v1)

All `/v1/*` routes require HMAC headers. Health/metrics live outside.

| Method | Path                          | Use case                  | Success                |
| ------ | ----------------------------- | ------------------------- | ---------------------- |
| POST   | `/v1/notifications`           | `SubmitNotification`      | 202 + `notification_id`; 200 + `duplicate:true` on replay |
| GET    | `/v1/notifications/{id}`      | `GetNotification`         | 200 + view                |
| POST   | `/v1/templates`               | `CreateTemplate`          | 201 + template            |
| GET    | `/v1/templates/{id}`          | `GetTemplate`             | 200 + template            |
| PUT    | `/v1/users/{id}/settings`     | `UpdateSetting`           | 204                       |
| POST   | `/v1/users/{id}/devices`      | `RegisterDevice`          | 204                       |
| GET    | `/healthz`, `/readyz`         | n/a                       | 200 `{status:"ok"}`       |
| GET    | `/metrics`                    | Prometheus scraper        | 200 text/plain            |

### 8.1 Authentication

The middleware `HMACAuth` requires three headers on every protected request:

```
X-App-Key:        <client identifier>
X-App-Timestamp:  <unix seconds>
X-App-Signature:  hex( HMAC-SHA256( appSecret, ts || "\n" || method || "\n" || path || "\n" || body ) )
```

The verifier rejects:

- missing headers,
- unknown `X-App-Key`,
- a timestamp outside ±`HMAC_SKEW` (default 5 minutes),
- a signature that doesn't match (constant-time compare).

Clients are configured via `APP_CLIENTS=key1:secret1,key2:secret2,...`.

### 8.2 Error envelope

Every error response is `{"code":"...","message":"..."}`. Mapping
(`internal/adapter/inbound/httpapi/handlers.go::mapDomainError`):

| Sentinel                              | Status |
| ------------------------------------- | ------ |
| `ErrNotFound`                         | 404    |
| `ErrInvalidInput` / `ErrInvalidStatusTransition` | 400 |
| `ErrOptedOut`                         | 403    |
| `ErrRateLimited`                      | 429 (with `Retry-After`) |
| `ErrUnauthenticated`                  | 401    |
| `ErrAlreadyExists`                    | 409    |
| anything else                         | 500    |

---

## 9. Cross-cutting Concerns

### 9.1 Idempotency

Caller-supplied `event_id` (1..256 chars). Two layers:

1. **Hot-path Redis claim** (`SETNX notif:dedupe:<event_id> EX <ttl>`).
   `DEDUPE_TTL` defaults to 24h. Avoids hitting Postgres for replays.
2. **Authoritative DB unique index** on `notification_log.event_id`. If the
   Redis claim is lost (eviction, restore) but the row exists, the use case
   reads it back and returns `Duplicate=true`.

### 9.2 Rate limiting

Per-(user, channel) fixed window. Keys look like `notif:rl:<user_id>:<channel>`.
Limits via env: `RATELIMIT_PUSH_PER_HOUR=20`, `..._SMS_PER_HOUR=5`,
`..._EMAIL_PER_HOUR=10`. Window = `RATELIMIT_WINDOW` (default 1h). When the
user is *not* identified by `user_id` (raw email/phone/token), no per-user
limit applies — global QPS would be enforced upstream of the API in
production.

### 9.3 Retry & dead-lettering

Workers translate provider errors into one of three outcomes:

- **`OutcomeSent`** (`MarkSent` + `notification_sent_total`).
- **`OutcomeRetry`** when the provider returns `port.ErrTransient` and the
  attempt counter is below `MAX_RETRIES`. The use case calls
  `EventPublisher.Retry`, which republishes to `<work>.retry` with TTL =
  `BackoffSchedule[attempt]`. RabbitMQ dead-letters the message back to the
  work queue when the TTL expires; the next worker sees `attempt+1`.
- **`OutcomeDeadLetter`** when retries are exhausted *or* the provider
  returns a non-transient error (treated as 4xx-class). The notification is
  marked `dead_letter` and an analytics event is recorded.

### 9.4 Templates

`text/template` for SMS/push, `html/template` for email (auto-escape). Cached
in-process by template id; we don't currently invalidate the cache on
update — version bumps are expected to use a new id, which the seed migration
demonstrates.

### 9.5 Notification settings (opt-out)

`notification_settings(user_id, channel, opt_in)`. Default (no row) is
opt-in (PDF: "Users will be able to opt-in or opt-out").
`SubmitNotification` short-circuits with `domain.ErrOptedOut` before any
expensive work happens.

---

## 10. Observability

### 10.1 Logs

Structured JSON via `slog`, level controlled by `LOG_LEVEL`. Every HTTP
request logs `request_id`, `app_key`, `method`, `path`, `status`,
`duration_ms`. Workers log per-message decisions with `notification_id`,
`channel`, `attempt`.

### 10.2 Metrics

Prometheus, scraped at `/metrics` (API on `:8080`, each worker on its admin
listener `:9090`):

| Metric                                | Labels                  | Where                          |
| ------------------------------------- | ----------------------- | ------------------------------ |
| `notifications_accepted_total`        | `channel`               | API (`SubmitNotification`)     |
| `notifications_sent_total`            | `channel`               | Worker (`ProcessNotification`) |
| `notifications_failed_total`          | `channel`               | Worker, on transient retry     |
| `notifications_dead_letter_total`     | `channel`               | Worker, on terminal failure    |
| `worker_process_duration_seconds`     | `channel`, `outcome`    | Worker                         |
| `http_request_duration_seconds`       | `method`, `route`, `status` | API middleware             |

### 10.3 Health

`/healthz` and `/readyz` return `200 {"status":"ok"}`. `/readyz` will gain a
real readiness check (pool ping, queue ping) once the AMQP auto-reconnect
work below lands.

---

## 11. Configuration

All config is environment-driven via `internal/platform/config/`.
See `.env.example` for the canonical list. Highlights:

```
LOG_LEVEL, HTTP_ADDR
POSTGRES_DSN, REDIS_ADDR, RABBITMQ_URL          (required)
APP_CLIENTS=key1:secret1,key2:secret2,...        (required)
PROVIDER_MODE=mock|real
MAX_RETRIES=5
DEDUPE_TTL=24h
HMAC_SKEW=5m
RATELIMIT_WINDOW=1h
RATELIMIT_PUSH_PER_HOUR=20
RATELIMIT_SMS_PER_HOUR=5
RATELIMIT_EMAIL_PER_HOUR=10
WORKER_CHANNEL=push_ios|push_android|sms|email   (worker only)
WORKER_CONCURRENCY=8
```

---

## 12. Deployment

`deploy/compose/docker-compose.yml` brings up:

- `postgres:16-alpine`
- `redis:7-alpine`
- `rabbitmq:3-management` (UI on `:15672`)
- `migrate` (one-shot goose runner; `Dockerfile.migrate`)
- `api` (`Dockerfile.api`) on `:8080`
- four workers — one per channel — each with `WORKER_CHANNEL` set, sharing
  `Dockerfile.worker`.

Both API and worker images are distroless static binaries with `nonroot` user.
Healthchecks gate dependencies (api waits on `migrate` success and
healthy redis + rabbit; workers wait on api).

---

## 13. Failure Modes & Resilience

The list below was the v1 limitation matrix. Since the resilience pass we
addressed all nine; the table now records each as **resolved** with a
pointer to the implementation. Anything genuinely outstanding is in §13.1.

| ID  | Original concern | Resolution |
| --- | ---------------- | ---------- |
| L1  | No AMQP auto-reconnect | `adapter/outbound/rabbitmq/topology.go::Conn` watches `NotifyClose`; `reconnectWithBackoff` redials with exponential backoff (500 ms → 30 s cap) and re-runs `declareTopology`. Worker `Consumer.Run` wraps `runOnce` in an outer loop that calls `Conn.AfterReconnect` before re-subscribing. |
| L2  | Stuck `in_flight` rows on worker crash | New `cmd/janitor` binary periodically calls `RescueStuckNotifications` → `NotificationRepository.ListStuckInFlight(threshold)` → re-publish + reset status to `enqueued`. Configurable via `JANITOR_INTERVAL`, `JANITOR_STUCK_THRESHOLD`, `JANITOR_BATCH_SIZE`. |
| L3  | Best-effort post-publish status update | **Transactional outbox** (`migrations/0003_outbox.sql`). `SubmitNotification` now calls `TxNotificationRepository.SubmitWithOutbox(n, payload)` — `notification_log` row + `notification_outbox` row written in one DB transaction. New `cmd/outbox-relay` binary drains pending rows with `SELECT … FOR UPDATE SKIP LOCKED`, calls `EventPublisher.PublishRaw`, and marks them published. |
| L4  | Fixed-window rate limiter | Replaced with a **token-bucket** implemented as a single Lua script in Redis (`adapter/outbound/redis/ratelimit.go`). Atomic refill+consume; effective rate = `limit/window`/sec. |
| L5  | Single shared AMQP channel | Publisher owns its own confirm-mode channel (`Conn.Channel()` + `ch.Confirm(false)`); every Publish waits for the broker ack via `PublishWithDeferredConfirmWithContext`. Consumers open per-loop channels via `Conn.ConsumeChannel(prefetch)`. Channels are reopened transparently after reconnect. |
| L6  | Template cache never invalidates | In-process cache now stores per-entry `expiresAt` (default 5 min, override via `NewWithTTL`). Expired entries trigger a re-fetch from `TemplateRepository`. |
| L7  | No real provider adapters | Skeleton implementations under `adapter/outbound/provider/{apns,fcm,twilio,sendgrid}/`, each with full request/response shape, header set, and transient/terminal error mapping driven by httptest tests. Auth (APNs JWT, FCM OAuth2) is behind `Authenticator` / `TokenSource` ports — the worker wires real signers into them; until then `PROVIDER_MODE=real` errors clearly. `cmd/worker/main.go::buildProvider` selects per-channel. |
| L8  | No global QPS cap | `httpapi/middleware.AppKeyRateLimit` runs after HMAC auth and gates the whole `/v1` group on `notif:rl:appkey:<key>`. Configurable via `APP_KEY_RATE_LIMIT` / `APP_KEY_RATE_WINDOW`. |
| L9  | Worker `/metrics` not scraped | `deploy/compose/prometheus.yml` defines two scrape jobs (`notification-api` on `:8080`, `notification-workers` on `:9090`); a `prometheus` service is wired into compose on host port `9091`. |

### 13.1 Outstanding follow-ups

- **APNs JWT signer** is stubbed in `cmd/worker/main.go::buildAPNSAuth`. A real implementation needs an ES256 signer (e.g. `golang-jwt/jwt`) over the `.p8` key with a 50-minute rotation. The HTTP path itself is exercised by tests via a `fakeAuth`.
- **FCM OAuth2 token source** is stubbed in `cmd/worker/main.go::buildFCMTokenSource`. Real wiring uses `golang.org/x/oauth2/google` to mint tokens for the `https://www.googleapis.com/auth/firebase.messaging` scope.
- The relay does not currently retry transient publish failures inside one pass; it marks them failed and the next pass picks them up. Adequate for v1 — bounded by `RELAY_INTERVAL`. Could be tightened with attempt-limited retry inside the use case.
- **`amqp-topic` / FCM v1 `data` payload size** is bounded by the providers, not by us. We don't currently truncate or split — caller responsibility.

---

## 14. Test Strategy

| Level        | Where                                         | What's covered                                                                                                                  |
| ------------ | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| Unit         | `internal/domain/*_test.go`                   | Channel/recipient/event-id parsing, notification state machine (happy + retry + illegal-transition rejection)                   |
| Unit         | `internal/app/usecase/*_test.go`              | Use case behaviour against in-memory port fakes (`fakes_test.go`). SubmitNotification ×7; ProcessNotification ×4.                |
| Unit         | `internal/adapter/outbound/redis/cache_test.go` | RateLimiter (×4) and Deduper (×2) under miniredis (TTL fast-forward, isolation, etc.)                                          |
| Unit         | `internal/platform/auth/hmac_test.go`         | HMAC verify (round-trip, bad secret, stale ts, unknown key)                                                                     |
| Integration  | `test/integration/api_test.go` (`integration` build tag) | End-to-end signed POST → DB row → worker → status `sent`. Dedupe collapse test.                                       |

Run unit tests with `go test -race -count=1 ./...`. Integration tests assume
`make up` is running.

---

## 15. Glossary

- **Aggregate** — a cluster of domain objects with a single root entity; the
  unit of consistency.
- **Adapter** — a piece of code that adapts external technology (HTTP, DB,
  queue, …) to a port.
- **Composition root** — the only place in the codebase where adapters are
  instantiated and bound to ports. For us: `cmd/api/main.go` and
  `cmd/worker/main.go`.
- **Driving (inbound) adapter** — calls into the application (HTTP request,
  AMQP message).
- **Driven (outbound) adapter** — is called by the application (DB write,
  queue publish, provider call).
- **Port** — an interface defined by the application layer, expressing what it
  needs from the world.
- **Use case** — an orchestration class. One conceptual operation
  (`SubmitNotification`, `ProcessNotification`, …); one `Execute` method.

---

*Last reviewed: 2026-05-08 — after the hexagonal refactor.*
