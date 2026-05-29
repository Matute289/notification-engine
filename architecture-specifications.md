# Notification Engine — Architecture Specifications

> Implementation reference for the Go notification engine in this repository.
> Mirror to `Notification_System.pdf` (the source design) plus every decision we
> made on top of it.

---

## 1. Overview

The Notification Engine is a Go service that accepts notification requests over
HTTP and delivers them to end users via eight channels: **iOS push** (APNs),
**Android push** (FCM), **SMS** (Twilio-class), **email** (SendGrid-class),
**Telegram**, **WhatsApp**, **Line**, and **Facebook Messenger**.
It is built around an asynchronous, queue-backed pipeline:

```
internal services ──► HTTP API ──► message queue ──► per-channel worker ──► provider ──► user device
                                       │
                                       └─► persistence + analytics
```

**Infrastructure-agnostic design:** The codebase is decoupled from specific technology choices:
- **Databases:** Works with Postgres (included), but schema is SQL-standard; easily swap for MySQL, MariaDB, etc.
- **Cache:** Works with Redis (included), but rate-limiter/deduper are pluggable behind a `Cache` port.
- **Message queue:** Works with RabbitMQ locally (docker-compose), but `EventPublisher` port supports any MQ (Kafka, SQS, GCP Pub/Sub, etc.).
- **Document store:** Works with MongoDB (externally), but `TemplateRepository` port could use DynamoDB, Firestore, or Postgres JSONB.
- **Authentication:** Works with any JWT issuer or HMAC-only mode; no vendor lock-in.
- **Hosting:** Binaries run anywhere (AWS, GCP, Azure, Kubernetes, VPS, on-premises).

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
| Authn (pluggable)| JWT (any OpenID provider) **and/or** HMAC-SHA256           |
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
┌─────────── cmd/{api,worker,janitor,outbox-relay}/main.go ────────────┐
│               composition root: bind concrete adapters               │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  cmd/api/http/{router,handlers/,dto/}   (driving adapter)  │    │
│  │  cmd/worker/consumer/consumer.go         (driving adapter)  │    │
│  │  middleware/                             (HTTP middleware)  │    │
│  └──────────────────────────┬──────────────────────────────────┘    │
│                             │                                        │
│  ┌──────────────────────────▼──────────────────────────────────┐    │
│  │                   internal/service/                         │    │
│  │       (SubmitNotification, ProcessNotification, …)          │    │
│  └──────────────────────────┬──────────────────────────────────┘    │
│                             │ uses                                   │
│  ┌──────────────────────────▼──────────────────────────────────┐    │
│  │                   internal/port/                            │    │
│  │      (interfaces for repos, queue, cache, provider, …)      │    │
│  └──────────────────────────┬──────────────────────────────────┘    │
│                             │ implemented by                         │
│  ┌──────────────────────────▼──────────────────────────────────┐    │
│  │  infrastructure/{postgres,redis,rabbitmq,rendering,provider} │    │
│  │  observability/{logger/,metrics/}                           │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │              internal/domain/                               │    │
│  │   entities · value objects · state machine · errors         │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                      │
│  internal/platform/{auth,config}  (cross-cutting infra)             │
└──────────────────────────────────────────────────────────────────────┘
```

Import direction: **domain ← port ← service ← {infrastructure, middleware, observability, cmd/.../http, cmd/.../consumer} ← cmd/*/main.go**

Note: `cmd/api/http/` and `cmd/worker/consumer/` are **driving adapters** collocated inside their composition root directory — they are not composition roots themselves.

### 2.1 Layer responsibilities

| Layer              | Path                                        | What lives here                                                                                        | Allowed imports                                    |
| ------------------ | ------------------------------------------- | ------------------------------------------------------------------------------------------------------ | -------------------------------------------------- |
| Domain             | `internal/domain/`                          | Entities, value objects, sentinel errors, state machine                                                | stdlib + `uuid`                                    |
| Ports              | `internal/port/`                            | Interfaces describing what services need from the outside world                                        | `domain`, stdlib                                   |
| Services           | `internal/service/`                         | One struct + `Execute` per use case. All orchestration lives here.                                     | `domain`, `internal/port`, stdlib                  |
| HTTP driving adp.  | `cmd/api/http/{handlers/,dto/,router.go}`   | HTTP delivery → service input. No business logic.                                                      | `internal/service`, `internal/port`, `domain`, third-party |
| AMQP driving adp.  | `cmd/worker/consumer/`                      | AMQP delivery → service input. No business logic.                                                      | `internal/service`, `infrastructure/rabbitmq`, `domain` |
| Middleware         | `middleware/`                               | HTTP request pipeline (RequestID, Recoverer, AccessLog, Authenticate, AppKeyRateLimit)                | `internal/port`, `internal/platform/auth`, third-party |
| Infrastructure adp.| `infrastructure/{postgres,redis,rabbitmq,rendering,provider}/` | Concrete implementations of ports.                                              | `internal/port`, `domain`, third-party             |
| Observability      | `observability/{logger/,metrics/}`          | slog logger + Prometheus MetricsRecorder implementation                                                | `internal/port`, stdlib, third-party               |
| Platform           | `internal/platform/{auth,config}/`          | Cross-cutting infra not behind a port: HMAC verifier, env config loader                               | stdlib + third-party                               |
| Composition        | `cmd/{api,worker,janitor,outbox-relay}/main.go` | Wire concrete adapters into services. **Only place infrastructure meets ports.**                   | everything                                         |

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
| `Channel`       | value object  | One of `push_ios`, `push_android`, `sms`, `email`, `telegram`, `whatsapp`, `line`, `facebook_messenger`. |
| `Recipient`     | value object  | UserID and/or raw destination (Email / Phone / DeviceToken / MessagingID). `MessagingID` is used by Telegram, Line, and Facebook Messenger. WhatsApp reuses `Phone`. |
| `Email`/`Phone`/`DeviceToken`/`EventID` | value objects | Self-validating wrappers over `string`.                              |
| `Template`      | aggregate     | Versioned subject + body + optional `MediaURLs` (image/attachment URLs), stored in MongoDB. |
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

### 4.1 Ports (`internal/port/`)

| Port                       | Purpose                                                                                              |
| -------------------------- | ---------------------------------------------------------------------------------------------------- |
| `NotificationRepository`   | Persist + retrieve notifications and analytics events. Also `ListStuckInFlight` for the janitor.     |
| `TxNotificationRepository` | Extends `NotificationRepository` with `SubmitWithOutbox(n, payload)` — atomic notification + outbox. |
| `OutboxRepository`         | `Claim(limit) → ([]OutboxItem, OutboxTx)` for the relay. Uses `FOR UPDATE SKIP LOCKED`.              |
| `TemplateRepository`       | Persist + retrieve templates. Methods: `Create`, `Get`, `Update`, `Delete`, `List`.                  |
| `UserRepository`           | Read users, devices; upsert/delete devices; upsert settings. Methods include `DeleteDevice`.         |
| `EventPublisher`           | `Publish` (typed), `PublishRaw` (bytes, used by relay), `Encode` (typed→bytes), `Retry`.             |
| `RateLimiter`              | Token-bucket per key.                                                                                |
| `Deduper`                  | Best-effort idempotency on `event_id`.                                                               |
| `NotificationProvider`     | Final-mile delivery (APNs/FCM/Twilio/SendGrid/Mock).                                                 |
| `TemplateRenderer`         | Render a stored template against caller-supplied vars.                                               |
| `MetricsRecorder`          | Domain-shaped metric emission.                                                                       |
| `Clock`                    | Test-controlled "now".                                                                               |

### 4.2 Service (`internal/service/`)

| Service                    | Driven by             | Description                                                                                                                                                                                              |
|----------------------------| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `SubmitNotification`       | `httpapi`             | Dedupe → opt-out check → rate limit → hydrate recipient (when UserID supplied) → render template → mark enqueued → `Publisher.Encode` → `Notifications.SubmitWithOutbox` (atomic log+outbox) → metric.    |
| `GetNotification`          | `httpapi`             | Read one notification by id.                                                                                                                                                                            |
| `ProcessNotification`      | `worker` (AMQP)       | `MarkInFlight` → call `NotificationProvider` → on success `MarkSent`; on `ErrTransient` republish via `EventPublisher.Retry` (and `MarkRetrying`/`MarkDeadLetter`); on terminal error `MarkDeadLetter`. |
| `RescueStuckNotifications` | `janitor`             | Periodic. Lists `in_flight` rows older than `StuckThreshold`, republishes each, resets status to `enqueued`.                                                                                              |
| `RelayOutbox`              | `outbox-relay`        | Periodic. Claims a batch of pending outbox rows in one TX, calls `Publisher.PublishRaw` for each, marks rows published, commits.                                                                          |
| `RegisterDevice`           | `httpapi`             | Upsert a (user, channel, token) device.                                                                                                                                                                  |
| `UpdateSetting`            | `httpapi`             | Upsert opt-in for a (user, channel).                                                                                                                                                                     |
| `CreateTemplate`           | `httpapi`             | Validate via `domain.NewTemplate` and persist.                                                                                                                                                           |
| `GetTemplate`              | `httpapi`             | Read one template by id.                                                                                                                                                                                 |
| `UpdateTemplate`           | `httpapi`             | Get → ownership check → `Template.UpdateFields` (name/subject/body/mediaURLs, in-place mutation) → persist. Returns `ErrForbidden` if wrong owner.                                                     |
| `DeleteTemplate`           | `httpapi`             | Get → ownership check → hard-delete. Returns `ErrForbidden` if wrong owner, `ErrNotFound` if missing.                                                                                                   |
| `ListTemplates`            | `httpapi`             | List all templates for an owner, optionally filtered by channel. Returns a slice; handler groups by channel in the response.                                                                             |
| `DeleteDevice`             | `httpapi`             | Validates push channel and non-empty token → `UserRepository.DeleteDevice`. Returns `ErrNotFound` if device not registered.                                                                              |

`SubmitNotification` is the largest use case; everything else is a thin
orchestration around one or two ports. The persist-then-publish-then-mark
order matters for crash recovery (a row in `received` without a queued
message can be re-driven by a janitor; the inverse never happens).

---

## 5. Adapters

### 5.1 Inbound

| Module                           | Drives                  | Notes                                                                                                                                                                         |
| -------------------------------- | ----------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cmd/api/http/handlers/`         | `Submit`/`Get`/`Create`/`Update`/`Delete`/`List` | One file per endpoint (`submit_notification.go`, `get_notification.go`, `create_template.go`, `get_template.go`, `update_template.go`, `delete_template.go`, `list_templates.go`, `update_setting.go`, `register_device.go`, `delete_device.go`). `handler.go` holds the `Handler` struct (fields use `Svc` suffix) and `writeJSON`. `error.go` holds `mapDomainError` + `writeError`. Each file has a matching `*_test.go`; shared fakes live in `fakes_test.go`. |
| `cmd/api/http/dto/`              | n/a                     | One file per exported DTO type. `ToView` helper converts domain `Notification` to `NotificationView`.                                                                         |
| `cmd/api/http/router.go`         | n/a                     | chi router wiring: `NewRouter(h, verifier, limiter, log, cfg)`. Health and metrics outside auth-protected sub-router.                                                         |
| `middleware/`                    | n/a                     | RequestID (UUID per request), Recoverer (panic → 500 + stack log), AccessLog (slog + Prometheus histogram), Authenticate (Clerk JWT + HMAC verification), AppKeyRateLimit.           |
| `cmd/worker/consumer/`           | `ProcessNotification`   | Bounded-concurrency consumer: `Channel.Qos(prefetch)` → semaphore-limited goroutine pool. On service error, `Nack(requeue=true)`; otherwise `Ack`.                           |

### 5.2 Infrastructure (outbound — all pluggable)

| Module                              | Implements                                    | Notes                                                                                                                                                                                              |
| ----------------------------------- | --------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `infrastructure/postgres/`          | `Notification/UserRepository`                 | Example: pgx pool. Could swap for MySQL, SQLite, CockroachDB. JSON columns for `recipient` + `variables`.                                                                                                |
| `infrastructure/mongodb/`           | `TemplateRepository`                          | Example: MongoDB collection. Could swap for Firestore, DynamoDB, or Postgres JSONB. UUID as `_id`; unique index on `(name, channel, locale, version)`.                  |
| `infrastructure/redis/`             | `RateLimiter`, `Deduper`, `TemplateCache`     | Example: Redis. Could swap for Memcached, DynamoDB, or in-memory store. Token-bucket via Lua; `TemplateCache` is a read-through write-through decorator. All three share a single `CircuitBreaker` — see §9.6. |
| `infrastructure/rabbitmq/`          | `EventPublisher`                              | Example: RabbitMQ topology. Could swap for Kafka, SQS, GCP Pub/Sub, NATS. Declares one work + retry + dead queue per channel; dead-letter-with-TTL for retries. |
| `infrastructure/provider/mock/`     | `NotificationProvider`                        | Logs every send; an injected `failureRate` exercises the retry branch in demos. Channel-agnostic — handles all 8 channels.                                                                         |
| `infrastructure/provider/{apns,fcm,twilio,sendgrid}/` | `NotificationProvider`        | Real provider adapters for push (APNs/FCM), SMS (Twilio), email (SendGrid) with full request/response shape and transient/terminal error mapping.                                                  |
| `infrastructure/provider/{telegram,whatsapp,line,fbmessenger}/` | `NotificationProvider` | Social channel provider skeletons: Telegram Bot API, Meta Cloud API (WhatsApp), LINE Messaging API, Meta Graph API (Facebook Messenger). Each follows the same pattern as the existing providers; credential wiring via env vars. |
| `infrastructure/rendering/`         | `TemplateRenderer`                            | Compiles `text/template` (or `html/template` for email auto-escape); per-id in-process cache with TTL.                                                                                            |
| `observability/metrics/`            | `MetricsRecorder`                             | Example: Prometheus. Could swap for OpenTelemetry, DataDog, NewRelic. Counter/histogram set exposed at `/metrics`. Tests use a no-op fake.                                                                                                  |
| `observability/logger/`             | n/a                                           | slog JSON logger (cross-cutting, not behind a port). Example: stdout. Could redirect to any log sink.                                                                                                                                               |

### 5.3 Platform

| Module                              | Notes                                                                                              |
| ----------------------------------- | -------------------------------------------------------------------------------------------------- |
| `internal/platform/auth/`           | `Verifier` (HMAC — no external deps) + `ClerkVerifier` (JWT — works with any OpenID issuer). Constant-time comparison, JWKS caching, issuer + claim validation. Both optional; at least one required. |
| `internal/platform/config/`         | `caarlos0/env` env→struct binding. Loads from environment variables, agnostic to the values themselves.             |

---

## 6. Data Model

### 6.1 PostgreSQL schema (`migrations/0001_init.sql`)

| Table                   | Purpose                                                                 | Notable columns / indexes                                                                                       |
| ----------------------- | ----------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `users`                 | Contact info (PDF Figure 10-8)                                          | `email`, `country_code`, `phone_number`. Unique partial idx on `LOWER(email)`.                                  |
| `devices`               | Push tokens (one user → many devices)                                   | `(channel, device_token)` unique. Index on `user_id`.                                                           |
| `notification_settings` | Per-channel opt-in                                                      | PK `(user_id, channel)`.                                                                                        |
| `notification_log`      | Authoritative life-cycle row per notification                           | UUID PK, **unique** `event_id` (the dedupe backstop), `status` + `attempt` + `last_error`, `recipient` JSONB.    |
| `analytics_events`      | One-to-many event timeline (sent / dead_letter / click / unsubscribe)   | FK to `notification_log`, JSONB `metadata`.                                                                     |

`migrations/0002_seed.sql` registers a demo user and two devices. Template
seeding is skipped — the seed inserts templates directly into MongoDB
(see `migrations/0002_seed.sql` notes). `migrations/0004_template_to_mongodb.sql`
drops the old `notification_templates` Postgres table and the FK from
`notification_log.template_id`.

### 6.2 MongoDB collection (`notification_engine.notification_templates`)

| Field        | Type      | Notes                                                   |
| ------------ | --------- | ------------------------------------------------------- |
| `_id`        | string    | UUID (e.g. `"550e8400-e29b-41d4-a716-446655440000"`)   |
| `name`       | string    |                                                         |
| `channel`    | string    | `push_ios`, `push_android`, `sms`, `email`              |
| `locale`     | string    | Default `en`                                            |
| `subject`    | string    | Go template string (empty for push/sms)                 |
| `body`       | string    | Go template string                                      |
| `media_urls` | []string  | Optional URLs to images / attachments for rich push/MMS |
| `version`    | int       |                                                         |
| `created_at` | date      |                                                         |
| `updated_at` | date      |                                                         |

Unique index: `(name, channel, locale, version)`.

**Template caching (two layers):**
1. **L1 — in-process** (`infrastructure/rendering.Renderer`): compiled `text/template`/`html/template` per id, TTL 5 min.
2. **L2 — Redis** (`infrastructure/redis.TemplateCache`): raw `domain.Template` JSON at key `notif:tmpl:<uuid>`, TTL configurable via `TEMPLATE_CACHE_TTL` (default 5 min).

---

## 7. Queue Topology (RabbitMQ)

Provisioned by `infrastructure/rabbitmq/topology.go::Setup`:

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

All `/v1/*` routes require **at least one** authentication mechanism (JWT or HMAC; see §8.1). Health/metrics live outside.

| Method | Path                          | Use case                  | Success                |
| ------ | ----------------------------- | ------------------------- | ---------------------- |
| POST   | `/v1/notifications`           | `SubmitNotification`      | 202 + `notification_id`; 200 + `duplicate:true` on replay |
| GET    | `/v1/notifications/{id}`      | `GetNotification`         | 200 + view                |
| POST   | `/v1/templates`               | `CreateTemplate`          | 201 + template            |
| GET    | `/v1/templates/{id}`          | `GetTemplate`             | 200 + template            |
| PUT    | `/v1/templates/{id}`          | `UpdateTemplate`          | 200 + updated template    |
| DELETE | `/v1/templates/{id}`          | `DeleteTemplate`          | 204                       |
| GET    | `/v1/templates`               | `ListTemplates`           | 200 + `map[channel][]TemplateView`; optional `?channel=` filter |
| PUT    | `/v1/users/{id}/settings`     | `UpdateSetting`           | 204                       |
| POST   | `/v1/users/{id}/devices`      | `RegisterDevice`          | 204                       |
| DELETE | `/v1/users/{id}/devices`      | `DeleteDevice`            | 204; body `{channel, device_token}` |
| GET    | `/healthz`, `/readyz`         | n/a                       | 200 `{status:"ok"}`       |
| GET    | `/metrics`                    | Prometheus scraper        | 200 text/plain            |

### 8.1 Authentication

All `/v1/*` routes require **at least one of two independent authentication mechanisms**:

#### 8.1.1 JWT (user-facing — optional)

When `CLERK_ISSUER` is configured, the API accepts Bearer tokens:

```
Authorization: Bearer <jwt>
```

The verifier (`internal/platform/auth/clerk.go::ClerkVerifier`):
- Fetches the issuer's JWKS at `CLERK_ISSUER + "/.well-known/jwks.json"` once and caches it with auto-refresh via `lestrrat-go/jwx/v2`.
- Verifies the JWT's RS256 signature locally.
- Validates the `iss` (issuer), `sub` (subject, user id), and optionally `azp` (authorized party) claims.
- If `CLERK_AUTHORIZED_PARTIES` is set, rejects tokens whose `azp` is not in the list.
- If `CLERK_ISSUER` is empty, JWT verification is disabled.

**Works with any OpenID provider:**
- **Clerk** (example shown; https://clerk.dev)
- **Auth0** (https://auth0.com)
- **Okta** (https://okta.com)
- **Cognito** (AWS, https://aws.amazon.com/cognito)
- **Your own** JWT issuer with a standard JWKS endpoint

Users authenticate via your JWT provider's UI (no custom frontend required by this service). The provider issues session tokens that the app forwards to the API as Bearer tokens.

#### 8.1.2 HMAC-SHA256 (server-to-server — always available)

The middleware `Authenticate` also accepts HMAC-authenticated requests:

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

**No external dependencies:** HMAC works in any environment (air-gapped, no internet, no third-party APIs).

If `APP_CLIENTS` is empty, HMAC authentication is disabled.

#### 8.1.3 Unified dispatch

The middleware `Authenticate(clerk, hmac)` in `middleware/middleware.go`:
- If `Authorization: Bearer` header is present and JWT is configured → verify with ClerkVerifier (or your JWT verifier).
- Else if HMAC headers are present and HMAC is configured → verify with HMAC verifier.
- Else → `401 Unauthorized`.

Both mechanisms populate an `Identity{Subject, Kind}` context type (user id vs app key). For backward compatibility, `AppKeyFromContext` is still populated from `Identity.Subject`, so existing rate-limit and logging code works unchanged.

**Configuration options:**
- **JWT only:** set `CLERK_ISSUER` to your provider's URL, leave `APP_CLIENTS` empty.
- **HMAC only:** leave `CLERK_ISSUER` empty, set `APP_CLIENTS` (no external dependencies).
- **Both:** set both — the middleware dispatches by credential type.

**At least one mechanism must be enabled** (`CLERK_ISSUER` or `APP_CLIENTS` non-empty), validated at startup in `config.validate()`.

### 8.2 Error envelope

Every error response is `{"code":"...","message":"..."}`. Mapping
(`cmd/api/http/handles/error.go::mapDomainError`):

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
`..._EMAIL_PER_HOUR=10`, `..._SOCIAL_PER_HOUR=10` (applies to all four social channels). Window = `RATELIMIT_WINDOW` (default 1h). When the
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

### 9.6 Redis Circuit Breaker

All three Redis-backed components (`RateLimiter`, `Deduper`, `TemplateCache`) share a single `CircuitBreaker` instance created in `cmd/api/main.go`. The breaker uses a three-state machine:

```
Closed ── N consecutive errors ──► Open ── timeout elapsed ──► Half-Open
  ▲                                                                  │
  └──────────────── probe success ──────────────────────────────────┘
                    probe failure ──────────────────────► Open (immediately)
```

| Parameter     | Default | Env override (planned) |
| ------------- | ------- | ---------------------- |
| threshold     | 5 consecutive Redis errors | — |
| open timeout  | 30 s    | — |

**Fallback behaviours when open:**

| Component      | Fallback                                                                    |
| -------------- | --------------------------------------------------------------------------- |
| `RateLimiter`  | Fail-open: `Allow` returns `(true, nil)` — no request blocked               |
| `Deduper`      | Fail-open: `Claim` returns `(true, nil)` — DB unique index is the backstop  |
| `TemplateCache`| Bypass Redis: `Get`/`Create` go directly to MongoDB                         |

The same fail-open behaviour applies when a Redis error is returned while the circuit is still closed — the error is absorbed, the failure counter is incremented, and the request proceeds normally. Only `redis.Nil` (key not found) and `context.Canceled` are excluded from the failure count; both are normal operating conditions, not infrastructure signals.

Implementation: `infrastructure/redis/circuit_breaker.go`.

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
POSTGRES_DSN, REDIS_ADDR, RABBITMQ_URL          (required — any Postgres, Redis, RabbitMQ provider)
MONGODB_URI                                     (required — any MongoDB provider or self-hosted)
MONGODB_DATABASE                                (default: notification_engine)
TEMPLATE_CACHE_TTL                              (default: 5m — Redis L2 TTL for templates)

# Authentication — choose at least one; see §8.1 for details
CLERK_ISSUER=https://<slug>.clerk.accounts.dev  (optional — any OpenID provider; leave empty for HMAC-only)
CLERK_AUTHORIZED_PARTIES=https://yourdomain.com (optional — comma-separated allowed origins)
APP_CLIENTS=key1:secret1,key2:secret2,...        (optional — HMAC clients; leave empty for JWT-only)

PROVIDER_MODE=mock|real
MAX_RETRIES=5
DEDUPE_TTL=24h
HMAC_SKEW=5m
RATELIMIT_WINDOW=1h
RATELIMIT_PUSH_PER_HOUR=20
RATELIMIT_SMS_PER_HOUR=5
RATELIMIT_EMAIL_PER_HOUR=10
RATELIMIT_SOCIAL_PER_HOUR=10
WORKER_CHANNEL=push_ios|push_android|sms|email|telegram|whatsapp|line|facebook_messenger   (worker only)
WORKER_CONCURRENCY=8

# Social channel credentials (PROVIDER_MODE=real only)
TELEGRAM_BOT_TOKEN=
WHATSAPP_PHONE_NUMBER_ID=
WHATSAPP_ACCESS_TOKEN=
LINE_CHANNEL_ACCESS_TOKEN=
FB_PAGE_ACCESS_TOKEN=
JANITOR_INTERVAL=30s
JANITOR_STUCK_THRESHOLD=5m
RELAY_INTERVAL=500ms
```

**Flexibility notes:**
- Choose any Postgres, Redis, RabbitMQ, or MongoDB provider — the code doesn't care which.
- Authentication: use JWT (any OpenID issuer), HMAC (no external deps), or both.
- docker-compose and Kubernetes deployments all use the same binaries and env vars.

---

## 12. Deployment

### 12.1 Local development: docker-compose

`deploy/compose/docker-compose.yml` brings up:

- `postgres:16-alpine` (managed)
- `redis:7-alpine` (managed)
- `mongo:7` — template store (port `:27017`, persisted in `mongodata` volume)
- `rabbitmq:3-management` (UI on `:15672`)
- `migrate` (one-shot goose runner; `Dockerfile.migrate`)
- `api` (`Dockerfile.api`) on `:8080`
- four workers — one per channel — each with `WORKER_CHANNEL` set, sharing
  `Dockerfile.worker`.
- `janitor` — rescues stuck notifications
- `outbox-relay` — drains the transactional outbox
- `prometheus` — scrapes metrics from API and workers

Both API and worker images are distroless static binaries with `nonroot` user.
Healthchecks gate dependencies (api waits on `migrate` success and
healthy redis + rabbit; workers wait on api).

### 12.2 Production deployment

**The binaries are cloud-agnostic.** Deploy to any infrastructure by wiring environment variables. Platform-specific deployment blueprints live in dedicated branches (e.g., the `render` branch contains `render.yaml` + `DEPLOY_RENDER.md`).

#### Platforms

The same Docker images run on:

- **AWS** (ECS, EC2, Elastic Beanstalk, AppRunner) + RDS Postgres + ElastiCache Redis + your RabbitMQ/SQS/Kafka choice
- **GCP** (Cloud Run, App Engine, GKE) + Cloud SQL Postgres + Memorystore Redis + Pub/Sub or external queue
- **Azure** (App Service, Container Instances, AKS) + Azure Database Postgres + Azure Cache for Redis + Service Bus or external queue
- **Kubernetes** (any cluster, any cloud) via Helm, kustomize, or kubectl manifests
- **VPS** (DigitalOcean, Linode, Hetzner, etc.) with docker-compose or systemd units
- **On-premises** with your own infrastructure

Just set these env vars anywhere:
```
POSTGRES_DSN=...        (your Postgres instance)
REDIS_ADDR=...          (your Redis instance)
RABBITMQ_URL=...        (your RabbitMQ instance)
MONGODB_URI=...         (your MongoDB instance)
CLERK_ISSUER=...        (your JWT provider, or empty for HMAC-only)
APP_CLIENTS=...         (your HMAC clients, or empty for JWT-only)
```

No code changes needed.

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

- **Social channel credentials** (Telegram, WhatsApp, Line, Facebook Messenger) are checked at startup but the HTTP auth/payload logic is fully implemented; set the relevant env vars (`TELEGRAM_BOT_TOKEN`, `WHATSAPP_PHONE_NUMBER_ID` + `WHATSAPP_ACCESS_TOKEN`, `LINE_CHANNEL_ACCESS_TOKEN`, `FB_PAGE_ACCESS_TOKEN`) and `PROVIDER_MODE=real` to use them. The `notification_settings` channel constraint was extended in migration `0006_social_channels.sql`.
- **APNs JWT signer** is stubbed in `cmd/worker/main.go::buildAPNSAuth`. A real implementation needs an ES256 signer (e.g. `golang-jwt/jwt`) over the `.p8` key with a 50-minute rotation. The HTTP path itself is exercised by tests via a `fakeAuth`.
- **FCM OAuth2 token source** is stubbed in `cmd/worker/main.go::buildFCMTokenSource`. Real wiring uses `golang.org/x/oauth2/google` to mint tokens for the `https://www.googleapis.com/auth/firebase.messaging` scope.
- The relay does not currently retry transient publish failures inside one pass; it marks them failed and the next pass picks them up. Adequate for v1 — bounded by `RELAY_INTERVAL`. Could be tightened with attempt-limited retry inside the use case.
- **`amqp-topic` / FCM v1 `data` payload size** is bounded by the providers, not by us. We don't currently truncate or split — caller responsibility.

---

## 14. Test Strategy

| Level        | Where                                         | What's covered                                                                                                                  |
| ------------ | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| Unit         | `internal/domain/*_test.go`                   | Channel/recipient/event-id parsing, notification state machine (happy + retry + illegal-transition rejection)                   |
| Unit         | `internal/service/*_test.go`                  | Use case behaviour against in-memory port fakes (`fakes_test.go`). SubmitNotification ×7; ProcessNotification ×4.                |
| Unit         | `cmd/api/http/handlers/*_test.go`             | HTTP adapter tests via `httptest`. Parse errors, happy-path status codes, domain-error→HTTP mapping. `error_test.go` has a full table test for `mapDomainError`. Shared fakes in `fakes_test.go`. |
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

*Last reviewed: 2026-05-29 — Template CRUD (update/delete/list), device delete, and 4 social channel providers (Telegram, WhatsApp, Line, Facebook Messenger) added; see §4.2 and §8.*
