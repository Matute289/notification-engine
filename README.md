# Notification Engine

A Go service that delivers notifications to end users over eight channels — **iOS push** (APNs), **Android push** (FCM), **SMS**, **email**, **Telegram**, **WhatsApp**, **Line**, and **Facebook Messenger** — via an asynchronous, queue-backed pipeline. Implements the design from chapter 10 of *System Design Interview Vol. 1* (`Notification_System.pdf` in this repo).

**Infrastructure-agnostic:** Works with any Postgres, Redis, RabbitMQ, and MongoDB providers. Supports any JWT issuer or HMAC-only auth. Deploy to AWS, GCP, Azure, Kubernetes, VPS, or on-premises.

Locally everything runs from a single `docker compose up` with **mock providers**, so the full pipeline works without any third-party credentials.

The codebase follows **hexagonal / ports-and-adapters** architecture. Domain logic has zero infrastructure dependencies; technology choices live behind small interfaces (ports) that adapters implement.

> Full design reference: [`architecture-specifications.md`](./architecture-specifications.md)

---

## Quick start — local development

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

## Deploying to production

The same Docker images run on any infrastructure — just wire the connection strings via environment variables. Platform-specific deployment blueprints live in dedicated branches; for example, the [`render` branch](../../tree/render) contains a `render.yaml` Blueprint and a `DEPLOY_RENDER.md` step-by-step guide for deploying to Render.

### Platforms

The same Docker images work on:
- **AWS** (ECS, EC2, Elastic Beanstalk) + RDS Postgres + ElastiCache Redis + SQS or Kafka for queuing
- **GCP** (Cloud Run, App Engine, GKE) + Cloud SQL + Memorystore + Pub/Sub
- **Azure** (App Service, Container Instances, AKS) + Azure Database + Azure Cache + Service Bus
- **Kubernetes** (any cluster) via Helm or kustomize
- **VPS** (DigitalOcean, Linode, Vultr, etc.) with docker-compose

Just wire the connection strings via environment variables — the code doesn't change.

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

- **API** accepts notification requests at `POST /v1/notifications`, authenticated via JWT or HMAC-SHA256.
  - Validates input, checks per-(user, channel) opt-in and rate-limit (token bucket in cache).
  - Dedupes by caller-supplied `event_id` (cache `SETNX` + DB unique index).
  - Hydrates the recipient from the user record, renders the template from document store, and persists a `notification_log` row plus a matching `notification_outbox` row in one DB transaction.
- **Outbox relay** (separate process) periodically drains pending `notification_outbox` rows with `SELECT … FOR UPDATE SKIP LOCKED` and publishes them to a per-channel message queue.
- **Worker per channel** pulls from its queue, calls the provider, marks the notification `sent`, retries with exponential backoff (30s → 2m → 10m → 1h → 6h) on transient failures, and dead-letters terminal failures.
- Exposes Prometheus metrics, structured JSON logs, and `/healthz` / `/readyz` probes.
- **Resilience**: Circuit breaker (fail-open on latency/outage) protects rate limiter, deduper, and template cache. Janitor rescues notifications stuck in `in_flight` state.

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

### Start the stack (local development)

```bash
make up
```

This builds all Docker images, brings up a complete local environment, runs goose migrations, and starts:

- **api** (`:8080`) — HTTP service with JWT/HMAC auth
- **8 workers** (`worker-push-ios`, `worker-push-android`, `worker-sms`, `worker-email`, and optionally `worker-telegram`, `worker-whatsapp`, `worker-line`, `worker-facebook-messenger`) — per-channel message consumers with admin listeners on `:9090`
- **janitor** — rescues notifications stuck in `in_flight` after worker crashes
- **outbox-relay** — drains `notification_outbox` to the message queue
- **prometheus** (`:9091`) — scrapes metrics from API and workers
- **RabbitMQ management UI** (`:15672`, user `notif`/`notif`)
- **Postgres, Redis, RabbitMQ, MongoDB** — all containerized with default credentials

(The same binaries run in production with your own infrastructure — see **Deploying to production** section.)

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
| **Authentication**        |                                                        |                     |
| `CLERK_ISSUER`            | JWT issuer URL (Clerk, Auth0, Okta, or your own) — `https://<slug>.clerk.accounts.dev` | (optional) |
| `CLERK_AUTHORIZED_PARTIES`| Comma-separated allowed origins for JWT `azp` claim | (optional) |
| `APP_CLIENTS`             | `key:secret,key:secret` for HMAC (no external deps)  | `demo-app:demo-...` |
| **Provider & Storage**    |                                                        |                     |
| `PROVIDER_MODE`           | `mock` or `real`                                      | `mock`              |
| `POSTGRES_DSN`            | Postgres connection string (required)                 |                     |
| `REDIS_ADDR`              | Redis address (required)                             |                     |
| `RABBITMQ_URL`            | RabbitMQ AMQP URL (required)                         |                     |
| `MONGODB_URI`             | MongoDB connection string (required)                 |                     |
| `MONGODB_DATABASE`        | MongoDB database name                                 | `notification_engine` |
| **Caching & Limits**      |                                                        |                     |
| `TEMPLATE_CACHE_TTL`      | L2 Redis cache TTL for templates                     | `5m`                |
| `DEDUPE_TTL`              | Idempotency window in Redis                           | `24h`               |
| `HMAC_SKEW`               | Tolerance on signed timestamps                        | `5m`                |
| `RATELIMIT_PUSH_PER_HOUR` | Per-user/channel hourly cap (push)                    | `20`                |
| `RATELIMIT_SMS_PER_HOUR`  | Per-user/channel hourly cap (SMS)                     | `5`                 |
| `RATELIMIT_EMAIL_PER_HOUR`| Per-user/channel hourly cap (email)                   | `10`                |
| `RATELIMIT_SOCIAL_PER_HOUR`| Per-user/channel hourly cap (Telegram/WhatsApp/Line/Messenger) | `10`       |
| `APP_KEY_RATE_LIMIT`      | Global QPS cap per app key (0 = disabled)            | `0`                 |
| **Worker & Retry**        |                                                        |                     |
| `WORKER_CHANNEL`          | `push_ios` `push_android` `sms` `email` `telegram` `whatsapp` `line` `facebook_messenger` | `push_ios` |
| `WORKER_CONCURRENCY`      | Prefetch + goroutine pool size per worker            | `8`                 |
| `MAX_RETRIES`             | Retry hops before dead-lettering                      | `5`                 |
| **Janitor & Outbox**      |                                                        |                     |
| `JANITOR_INTERVAL`        | How often janitor runs (e.g., `30s`, `5m`)           | `30s`               |
| `JANITOR_STUCK_THRESHOLD` | Age before a notification is considered stuck         | `5m`                |
| `JANITOR_BATCH_SIZE`      | Batch size for stuck-notification recovery           | `100`               |
| `RELAY_INTERVAL`          | How often outbox relay runs                          | `500ms`             |
| `RELAY_BATCH_SIZE`        | Batch size for outbox relay                          | `100`               |

See `.env.example` for the full list.

### Authentication

The API supports **two authentication mechanisms** (choose one or both):

#### 1. JWT (user-facing auth — optional)

For end users signing up via any JWT provider. **Clerk is an example; you can use any JWT issuer.**

```bash
# Users authenticate via your JWT provider (Account Portal, custom frontend, etc.)
# They get a JWT → app forwards it to the API as: Authorization: Bearer <jwt>

curl -X POST http://localhost:8080/v1/notifications \
  -H "Authorization: Bearer eyJhbGc..." \
  -H "Content-Type: application/json" \
  -d '{"event_id":"...","channel":"email",...}'
```

**Configure via** (pick your JWT provider):
- `CLERK_ISSUER` — Clerk example: `https://my-slug.clerk.accounts.dev`
  - Or any OpenID issuer with a JWKS endpoint
- `CLERK_AUTHORIZED_PARTIES` (optional) — comma-separated allowed origins for JWT's `azp` claim

The API verifies the JWT signature against the issuer's JWKS endpoint, caches public keys, and validates claims. Leave `CLERK_ISSUER` empty to disable JWT auth.

#### 2. HMAC-SHA256 (server-to-server — always available)

For internal services, testing, and systems without a JWT provider:

```bash
TS=$(date +%s)
BODY='{"event_id":"...","channel":"email",...}'
SIG=$(printf "%s\n%s\n%s\n%s" "$TS" POST /v1/notifications "$BODY" \
       | openssl dgst -sha256 -hmac demo-secret-please-change | awk '{print $2}')

curl -X POST http://localhost:8080/v1/notifications \
  -H "X-App-Key: demo-app" \
  -H "X-App-Timestamp: $TS" \
  -H "X-App-Signature: $SIG" \
  -H "Content-Type: application/json" \
  -d "$BODY"
```

**Configure via:**
- `APP_CLIENTS` — `key1:secret1,key2:secret2,...` (comma-separated). Always works; no external dependencies.

**Flexibility:**
- **JWT only:** set `CLERK_ISSUER`, leave `APP_CLIENTS` empty
- **HMAC only:** leave `CLERK_ISSUER` empty, set `APP_CLIENTS` (default for local dev)
- **Both:** set both — the middleware dispatches by credential type
- At least one must be configured

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

#### Via HMAC (server-to-server)

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

#### Via Clerk JWT (user-facing)

With `CLERK_ISSUER` configured, test by fetching a JWT from Clerk:

```bash
# 1. Use Clerk's Account Portal or a JWT template to obtain a token
# 2. Issue a request with the Bearer token
JWT="eyJhbGciOiJSUzI1NiIsImtpZCI6Ijxx..." # from Clerk

curl -sS -X POST http://localhost:8080/v1/notifications \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $JWT" \
  -d '{"event_id":"manual-2","channel":"email","recipient":{"user_id":1},"template_id":"11111111-1111-1111-1111-111111111111","variables":{"Name":"You"}}'
```

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
  domain/                      # entities, value objects, state machine, sentinel errors — NO infrastructure deps
  port/                        # interfaces — what services need (pluggable ports, not tied to tech)
  service/                     # orchestration — NO infrastructure deps
  platform/
    auth/                      # JWT (any OpenID) + HMAC verifiers
    config/                    # env-based config loader

infrastructure/                # adapters — pluggable implementations of ports
  postgres/                    # NotificationRepository (swappable: MySQL, SQLite, etc.)
  mongodb/                     # TemplateRepository (swappable: Firestore, DynamoDB, etc.)
  redis/                       # RateLimiter, Deduper (swappable: Memcached, DynamoDB, etc.)
  rabbitmq/                    # EventPublisher (swappable: Kafka, SQS, Pub/Sub, etc.)
  rendering/                   # TemplateRenderer (in-process caching)
  provider/
    mock/                      # mock provider (used in development)
    apns/fcm/twilio/sendgrid/  # real provider adapters (push/SMS/email)
    telegram/whatsapp/line/fbmessenger/  # social channel provider skeletons

middleware/                    # HTTP middleware (auth-agnostic, infra-agnostic)
observability/
  logger/logger.go             # slog logger
  metrics/metrics.go           # Prometheus (pluggable: other metrics backends)

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

| Channel                | Adapter                                      | Required env                                                                    |
| ---------------------- | -------------------------------------------- | ------------------------------------------------------------------------------- |
| `push_ios`             | `infrastructure/provider/apns`               | `APNS_BUNDLE_ID`, `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_AUTH_KEY`               |
| `push_android`         | `infrastructure/provider/fcm`                | `FCM_PROJECT_ID`, `FCM_CREDENTIALS_JSON` (path to service-account JSON)         |
| `sms`                  | `infrastructure/provider/twilio`             | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_FROM_NUMBER`                 |
| `email`                | `infrastructure/provider/sendgrid`           | `SENDGRID_API_KEY`, `SENDGRID_FROM_EMAIL`, `SENDGRID_FROM_NAME` (optional)      |
| `telegram`             | `infrastructure/provider/telegram`           | `TELEGRAM_BOT_TOKEN`                                                            |
| `whatsapp`             | `infrastructure/provider/whatsapp`           | `WHATSAPP_PHONE_NUMBER_ID`, `WHATSAPP_ACCESS_TOKEN`                             |
| `line`                 | `infrastructure/provider/line`               | `LINE_CHANNEL_ACCESS_TOKEN`                                                     |
| `facebook_messenger`   | `infrastructure/provider/fbmessenger`        | `FB_PAGE_ACCESS_TOKEN`                                                          |

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

This codebase follows **hexagonal (ports-and-adapters)** architecture, making it **infrastructure-agnostic**:

- **Domain** (`internal/domain/`) — zero infrastructure dependencies. Entities, value objects, state machine, sentinel errors. Pure business logic.
- **Ports** (`internal/port/`) — small interfaces describing what services need. No implementation details. Examples: `NotificationRepository`, `EventPublisher`, `RateLimiter`, `NotificationProvider`.
- **Services** (`internal/service/`) — orchestration logic. One struct per use case. Depends only on domain + ports; no infrastructure knowledge.
- **Driving adapters** — translate HTTP/AMQP into service input. Examples: `cmd/api/http/`, `cmd/worker/consumer/`.
- **Driven adapters** — concrete implementations of ports. Currently: Postgres, MongoDB, Redis, RabbitMQ. But ports are designed for swapping.

**Benefits of this architecture:**
- Business logic is **testable without containers** (use-case tests run against in-memory port fakes).
- **Technology choices are pluggable:**
  - Swap Postgres → MySQL, SQLite, or any SQL database (same port interface).
  - Swap RabbitMQ → Kafka, SQS, GCP Pub/Sub, or any message queue.
  - Swap Redis → Memcached, DynamoDB, or any cache.
  - Swap MongoDB → Firestore, DynamoDB, or any document store.
  - Swap Prometheus → any metrics backend.
  - Swap Clerk/JWT → any OpenID issuer or HMAC-only.
- **Clear dependency direction** — `domain ← port ← service ← infrastructure ← cmd/*/main.go`. Infrastructure can't sneak into domain.

For a detailed breakdown of layers, ports, aggregates, and the state machine, see [`architecture-specifications.md`](./architecture-specifications.md).

---

## License

(none specified)
