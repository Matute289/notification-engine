# Notification Engine

A Go service that delivers notifications to end users over four channels — **iOS push** (APNs), **Android push** (FCM), **SMS**, and **email** — via an asynchronous, queue-backed pipeline. Implements the design from chapter 10 of *System Design Interview Vol. 1* (`Notification_System.pdf` in this repo).

The whole stack — Postgres, Redis, RabbitMQ, the API, and four per-channel workers — runs from a single `docker compose up`. A built-in **mock provider** lets the full pipeline be exercised without any third-party credentials.

The codebase is organised in **hexagonal / ports-and-adapters** style. Domain logic has zero infrastructure dependencies; technology choices live behind small interfaces (ports) that adapters implement.

> Full design reference: [`architecture-specifications.md`](./architecture-specifications.md)

---

## Quick start

```bash
# Bring up the whole stack: postgres, mongo, redis, rabbitmq, prometheus,
# migrations, api, 4 workers, janitor, and outbox-relay
make up

# Tail logs (Ctrl-C to detach; containers keep running)
make logs

# Submit a signed sample notification (uses the demo client + seeded template)
APP_KEY=demo-app APP_SECRET=demo-secret-please-change ./scripts/sign-and-submit.sh

# Tear it all down (drops volumes too)
make down
```

| Endpoint                          | Purpose                                |
| --------------------------------- | -------------------------------------- |
| `http://localhost:8080`           | Notification API (auth-protected)      |
| `http://localhost:8080/metrics`   | Prometheus metrics from the API        |
| `http://localhost:9090`           | Per-worker admin metrics (`:9090`)     |
| `http://localhost:15672`          | RabbitMQ management UI (`notif`/`notif`) |
| `http://localhost:27017`          | MongoDB (templates)                    |
| `http://localhost:9091`           | Prometheus UI (scrapes API + workers)  |

---

## What it does

- **API** accepts notification requests at `POST /v1/notifications`, signed with HMAC-SHA256.
  - Validates input, checks per-(user, channel) opt-in and rate-limit (token bucket in Redis).
  - Dedupes by caller-supplied `event_id` (Redis `SETNX` + DB unique index).
  - Hydrates the recipient from the user record, renders the template from MongoDB, and persists a `notification_log` row plus a matching `notification_outbox` row in one DB transaction.
- **Outbox relay** (separate process) periodically drains pending `notification_outbox` rows with `SELECT … FOR UPDATE SKIP LOCKED` and publishes them to a per-channel RabbitMQ queue.
- **Worker per channel** pulls from its queue, calls the provider, marks the notification `sent`, retries with exponential backoff (30s → 2m → 10m → 1h → 6h) on transient failures, and dead-letters terminal failures.
- Exposes Prometheus metrics, structured JSON logs, and `/healthz` / `/readyz` probes.
- **Resilience**: Redis circuit breaker (fail-open on latency/outage) protects rate limiter, deduper, and template cache. Janitor rescues notifications stuck in `in_flight` state.

```
internal services ──► HTTP API ──► message queue ──► per-channel worker ──► provider ──► user
                                       │
                                       └─► persistence + analytics
```

See [`architecture-specifications.md`](./architecture-specifications.md) for the layered breakdown, state machine, schema, queue topology, and known limitations.

---

## Running locally

### Prerequisites

- Docker (with `docker compose`)
- Go 1.26+ (only if you want to run tests on the host or compile outside Docker)
- `make` and `openssl` (for the signed-submit demo script)

### Start the stack

```bash
make up
```

This builds the api/worker/migrate/janitor/outbox-relay images, brings up Postgres, MongoDB, Redis, and RabbitMQ, runs goose migrations against an empty database, and starts:

- **api** (`:8080`) — HTTP service with HMAC auth
- **4 workers** (`worker-push-ios`, `worker-push-android`, `worker-sms`, `worker-email`) — per-channel message consumers with admin listeners on `:9090`
- **janitor** — rescues notifications stuck in `in_flight` after worker crashes
- **outbox-relay** — drains `notification_outbox` to RabbitMQ
- **prometheus** (`:9091`) — scrapes metrics from API and workers
- **RabbitMQ management UI** (`:15672`, user `notif`/`notif`)

The migration also seeds:

- demo user `id=1` with email `demo@example.com` and devices on push iOS / Android
- opt-in for all four channels
- three templates in MongoDB:
  - `11111111-...` `welcome` (email)
  - `22222222-...` `order_shipped` (sms)
  - `33333333-...` `game_request` (push iOS)

### Configuration

Everything is environment-driven. Defaults live in `.env.example`; the compose stack injects them automatically. Highlights:

| Variable                  | Purpose                                                | Default             |
| ------------------------- | ------------------------------------------------------ | ------------------- |
| `APP_CLIENTS`             | `key:secret,key:secret` for HMAC clients              | `demo-app:demo-...` |
| `PROVIDER_MODE`           | `mock` or `real`                                      | `mock`              |
| `POSTGRES_DSN`            | Postgres connection string (required)                 |                     |
| `REDIS_ADDR`              | Redis address (required)                             |                     |
| `RABBITMQ_URL`            | RabbitMQ AMQP URL (required)                         |                     |
| `MONGODB_URI`             | MongoDB connection string (required)                 |                     |
| `MONGODB_DATABASE`        | MongoDB database name                                 | `notification_engine` |
| `TEMPLATE_CACHE_TTL`      | L2 Redis cache TTL for templates                     | `5m`                |
| `MAX_RETRIES`             | Retry hops before dead-lettering                      | `5`                 |
| `RATELIMIT_*_PER_HOUR`    | Per-channel hourly cap per user (token bucket)        | 20 / 5 / 10         |
| `WORKER_CHANNEL`          | `push_ios` `push_android` `sms` `email`               | `push_ios`          |
| `WORKER_CONCURRENCY`      | Prefetch + goroutine pool size per worker            | `8`                 |
| `DEDUPE_TTL`              | Idempotency window in Redis                           | `24h`               |
| `HMAC_SKEW`               | Tolerance on signed timestamps                        | `5m`                |
| `APP_KEY_RATE_LIMIT`      | Global QPS cap per app key                           |                     |
| `JANITOR_INTERVAL`        | How often janitor runs (e.g., `30s`, `5m`)           | `5m`                |
| `JANITOR_STUCK_THRESHOLD` | Age before a notification is considered stuck         | `1h`                |
| `RELAY_INTERVAL`          | How often outbox relay runs                          | `2s`                |

See `.env.example` for the full list.

---

## Testing

### Unit tests

Pure Go; no containers required.

```bash
go test -race -count=1 ./...
```

What gets exercised:

- **Domain** (`internal/domain/`) — channel validity, recipient/email/phone/event-id parsing, full state-machine of `Notification` (happy path, retry path, illegal-transition rejection).
- **Use cases** (`internal/service/`) — `SubmitNotification` (×7 scenarios) and `ProcessNotification` (×4 scenarios) run against in-memory port fakes. No DB, no queue, no Redis.
- **Redis adapters** (`infrastructure/redis/`) — RateLimiter, Deduper, TemplateCache, and CircuitBreaker covered with `miniredis`. Circuit breaker state machine and fail-open fallbacks tested.
- **HTTP handlers** (`cmd/api/http/handlers/`) — full endpoint tests via `httptest`, domain-error-to-HTTP-status mapping, request parsing.
- **HMAC** (`internal/platform/auth/`) — round-trip, bad secret, stale timestamp, unknown key.
- **Providers** (`infrastructure/provider/{apns,fcm,twilio,sendgrid}/`) — transient vs terminal error classification, request/response shape.
- **Template rendering** (`infrastructure/rendering/`) — template compilation and per-entry TTL cache.

Run a single test:

```bash
go test -race -run TestSubmit_HappyPath ./internal/service/...
```

### Integration tests

Assume the compose stack is up. They issue real signed POSTs against `http://localhost:8080`, then poll until the worker marks the notification `sent`.

```bash
make up                     # if not already running
make test-integration       # equivalent to: go test -race -count=1 -tags=integration ./test/integration/...
```

Two scenarios are covered today:

- `TestSubmitNotificationEndToEnd` — POST → DB row → worker consumes → status transitions to `sent` within ~10s.
- `TestDuplicateEventCollapses` — same `event_id` twice; second call returns `200 { duplicate: true }` with the same `notification_id`.

Override the target with env vars if you point them at a non-local stack:

```bash
API_HOST=https://staging.api APP_KEY=... APP_SECRET=... \
  go test -race -count=1 -tags=integration ./test/integration/...
```

### Manual smoke test

The signed-submit script builds a valid HMAC request and POSTs the seeded `welcome` template:

```bash
APP_KEY=demo-app APP_SECRET=demo-secret-please-change ./scripts/sign-and-submit.sh
```

Sample response:

```json
{ "notification_id": "5c7c…", "status": "enqueued" }
```

Then check the worker logs:

```bash
make logs
# look for: "mock provider delivered notification" channel=email …
```

Or follow the row in Postgres:

```bash
docker compose -f deploy/compose/docker-compose.yml exec postgres \
  psql -U notif -d notif -c \
  "SELECT id, status, attempt, last_error FROM notification_log ORDER BY created_at DESC LIMIT 5;"
```

### Issuing a request by hand

```bash
TS=$(date +%s)
BODY='{"event_id":"manual-1","channel":"email","recipient":{"user_id":1},"template_id":"11111111-1111-1111-1111-111111111111","variables":{"Name":"You","Product":"NotifEngine"}}'
SIG=$(printf "%s\n%s\n%s\n%s" "$TS" POST /v1/notifications "$BODY" \
       | openssl dgst -sha256 -hmac demo-secret-please-change | awk '{print $2}')

curl -sS -X POST http://localhost:8080/v1/notifications \
  -H "Content-Type: application/json" \
  -H "X-App-Key: demo-app" \
  -H "X-App-Timestamp: $TS" \
  -H "X-App-Signature: $SIG" \
  -d "$BODY"
```

The signature covers `timestamp \n method \n path \n raw-body`. See `internal/platform/auth/hmac.go` for the exact algorithm.

---

## Project layout

```
cmd/
  api/
    http/                      # HTTP driving adapter (package httpapi)
      router.go
      handlers/                # one file per endpoint + shared fakes
      dto/                     # one file per exported DTO type
    main.go                    # composition root
  worker/
    consumer/                  # AMQP driving adapter (package consumer)
      consumer.go
    main.go                    # composition root
  janitor/main.go              # rescues notifications stuck in_flight
  outbox-relay/main.go         # drains notification_outbox to RabbitMQ

internal/
  domain/                      # entities, value objects, state machine, sentinel errors
  port/                        # interfaces — what services need from infrastructure
  service/                     # one struct + Execute() per use case
  platform/
    auth/                      # HMAC verifier
    config/                    # env-based config loader

infrastructure/                # driven adapters at repo root
  postgres/                    # NotificationRepository, UserRepository, OutboxRepository
  mongodb/                     # TemplateRepository (templates + media URLs)
  redis/                       # RateLimiter, Deduper, TemplateCache, CircuitBreaker
  rabbitmq/                    # EventPublisher + topology setup
  rendering/                   # TemplateRenderer impl (text/html with L1 cache)
  provider/
    mock/                      # NotificationProvider (logging-only, used locally)
    apns/fcm/twilio/sendgrid/  # real provider skeletons

middleware/                    # HTTP middleware (RequestID, Recoverer, AccessLog, HMACAuth, AppKeyRateLimit)
observability/
  logger/logger.go             # slog logger (package logger)
  metrics/metrics.go           # Prometheus MetricsRecorder (package metrics)

deploy/
  docker/                      # Dockerfile.{api,worker,migrate,janitor,outbox-relay}
  compose/                     # docker-compose.yml + prometheus.yml
migrations/                    # goose .sql files (init + seed + outbox + template_to_mongodb)
test/integration/              # end-to-end tests behind the `integration` build tag
scripts/                       # sign-and-submit.sh
.env.example                   # default env configuration
architecture-specifications.md # full design reference
README.md (this file)
```

---

## Common make targets

| Target                | Action                                                                   |
| --------------------- | ------------------------------------------------------------------------ |
| `make tidy`           | `go mod tidy`                                                            |
| `make build`          | `go build ./...`                                                         |
| `make lint`           | `go vet ./...`                                                           |
| `make test`           | Unit tests with `-race -count=1` (no I/O, ~fast)                       |
| `make test-integration` | Integration tests against the compose stack (assumes `make up` running) |
| `make up`             | Start full docker-compose stack (postgres, mongo, redis, rabbitmq, api, workers, janitor, outbox-relay, prometheus, migrations) |
| `make down`           | Tear down stack and remove volumes                                       |
| `make logs`           | Follow combined container logs (Ctrl-C to detach)                       |
| `make migrate`        | Re-run goose migrations against the running stack                        |
| `make curl-submit`    | Sign + POST a sample notification (uses demo credentials)                |

---

## Wiring a real provider

Switching to real third-party delivery is a config flip plus credentials. Set `PROVIDER_MODE=real` and supply the env vars below; `cmd/worker/main.go::buildProvider` selects the right adapter for `WORKER_CHANNEL`.

| Channel        | Adapter                                  | Required env                                                                  |
| -------------- | ---------------------------------------- | ----------------------------------------------------------------------------- |
| `push_ios`     | `infrastructure/provider/apns`           | `APNS_BUNDLE_ID`, `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_AUTH_KEY`              |
| `push_android` | `infrastructure/provider/fcm`            | `FCM_PROJECT_ID`, `FCM_CREDENTIALS_JSON` (path to service-account JSON)       |
| `sms`          | `infrastructure/provider/twilio`         | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_FROM_NUMBER`               |
| `email`        | `infrastructure/provider/sendgrid`       | `SENDGRID_API_KEY`, `SENDGRID_FROM_EMAIL`, `SENDGRID_FROM_NAME` (optional)    |

The HTTP shape of each adapter is locked in by httptest-backed unit tests (transient vs terminal error mapping included). Each adapter is unit-tested with httptest and implements the `port.NotificationProvider` interface.

The two auth pieces that still need real wiring for production are:

- **APNs JWT signer** (`cmd/worker/main.go::buildAPNSAuth`): Supply an ES256 signer (e.g. `golang-jwt/jwt`) over the `.p8` key with 50-minute token rotation.
- **FCM OAuth2 token source** (`cmd/worker/main.go::buildFCMTokenSource`): Wire `golang.org/x/oauth2/google` against the `https://www.googleapis.com/auth/firebase.messaging` scope.

Each adapter implements `port.NotificationProvider.Send(ctx, *domain.Notification) error`:
- Return `port.ErrTransient` to trigger retry (exponential backoff via RabbitMQ DLQ).
- Return any other non-nil error to skip retries and dead-letter immediately.
- Return `nil` to mark the notification `sent`.

See `architecture-specifications.md` §13.1 for implementation notes.

---

## Architecture

This codebase follows **hexagonal (ports-and-adapters)** architecture:

- **Domain** (`internal/domain/`) — zero infrastructure dependencies. Entities, value objects, state machine, sentinel errors.
- **Ports** (`internal/port/`) — small interfaces describing what services need (repositories, queues, providers, caches, etc.).
- **Services** (`internal/service/`) — orchestration logic. One struct per use case; no infrastructure knowledge.
- **Driving adapters** — translate HTTP/AMQP into service input (`cmd/api/http/`, `cmd/worker/consumer/`).
- **Driven adapters** — concrete implementations of ports (`infrastructure/{postgres,mongodb,redis,rabbitmq,provider,rendering}/`).

This architecture ensures:
- Business logic is **testable without containers** (use-case tests run against in-memory port fakes).
- **Technology choices are pluggable** — swap Postgres for MySQL, RabbitMQ for Kafka, Prometheus for OTel without touching domain or services.
- **Clear dependency direction** — `domain ← port ← service ← infrastructure ← cmd/*/main.go`.

For a detailed breakdown of layers, ports, aggregates, and the state machine, see [`architecture-specifications.md`](./architecture-specifications.md).

---

## License

(none specified)
