# Notification Engine

A Go service that delivers notifications to end users over four channels — **iOS push** (APNs), **Android push** (FCM), **SMS**, and **email** — via an asynchronous, queue-backed pipeline. Implements the design from chapter 10 of *System Design Interview Vol. 1* (`Notification_System.pdf` in this repo).

The whole stack — Postgres, Redis, RabbitMQ, the API, and four per-channel workers — runs from a single `docker compose up`. A built-in **mock provider** lets the full pipeline be exercised without any third-party credentials.

The codebase is organised in **hexagonal / ports-and-adapters** style. Domain logic has zero infrastructure dependencies; technology choices live behind small interfaces (ports) that adapters implement.

> Full design reference: [`architecture-specifications.md`](./architecture-specifications.md)

---

## Quick start

```bash
# Bring up the whole stack: postgres + redis + rabbitmq + prometheus + migrate
# + api + 4 workers + janitor + outbox-relay
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
| `http://localhost:8080`           | Notification API                       |
| `http://localhost:8080/metrics`   | Prometheus metrics from the API        |
| `http://localhost:15672`          | RabbitMQ management UI (`notif`/`notif`) |
| `http://localhost:9091`           | Prometheus UI (scrapes API + workers)  |

---

## What it does

- Accepts notification requests at `POST /v1/notifications`, signed with HMAC-SHA256.
- Validates input, checks per-(user, channel) opt-in and rate-limit, dedupes by caller-supplied `event_id`, hydrates the recipient from the user record, renders the template, persists, and publishes to a per-channel queue.
- A worker per channel pulls from its queue, calls the provider, marks the notification `sent`, retries with exponential backoff on transient failures, and dead-letters terminal failures.
- Exposes Prometheus metrics, structured JSON logs, and `/healthz` / `/readyz` probes.

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

This builds the api/worker/migrate images, brings up Postgres, Redis, and RabbitMQ, runs goose migrations against an empty database, then starts the API and the four workers (`worker-push-ios`, `worker-push-android`, `worker-sms`, `worker-email`).

The migration also seeds:

- demo user `id=1` with email `demo@example.com` and devices on push iOS / Android
- opt-in for all four channels
- three templates:
  - `11111111-...` `welcome` (email)
  - `22222222-...` `order_shipped` (sms)
  - `33333333-...` `game_request` (push iOS)

### Configuration

Everything is environment-driven. Defaults live in `.env.example`; the compose stack injects them automatically. Highlights:

| Variable                  | Purpose                                                | Default             |
| ------------------------- | ------------------------------------------------------ | ------------------- |
| `APP_CLIENTS`             | `key:secret,key:secret` for HMAC clients              | `demo-app:demo-...` |
| `PROVIDER_MODE`           | `mock` or `real`                                      | `mock`              |
| `MAX_RETRIES`             | Retry hops before dead-lettering                      | `5`                 |
| `RATELIMIT_*_PER_HOUR`    | Per-channel hourly cap per user                       | 20 / 5 / 10         |
| `WORKER_CHANNEL`          | `push_ios` `push_android` `sms` `email`               | `push_ios`          |
| `DEDUPE_TTL`              | Idempotency window in Redis                           | `24h`               |
| `HMAC_SKEW`               | Tolerance on signed timestamps                        | `5m`                |

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
- **Use cases** (`internal/app/usecase/`) — `SubmitNotification` (×7 scenarios) and `ProcessNotification` (×4 scenarios) run against in-memory port fakes (`fakes_test.go`). No DB, no queue, no Redis.
- **Redis adapter** (`internal/adapter/outbound/redis/`) — RateLimiter and Deduper covered with `miniredis`.
- **HMAC** (`internal/platform/auth/`) — round-trip, bad secret, stale timestamp, unknown key.

Run a single test:

```bash
go test -race -run TestSubmit_HappyPath ./internal/app/usecase/...
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
  api/                       # HTTP API service (composition root)
  worker/                    # per-channel worker (composition root)
  janitor/                   # rescues notifications stuck in_flight
  outbox-relay/              # drains notification_outbox to RabbitMQ
internal/
  domain/                    # entities, value objects, state machine, sentinel errors
  app/
    port/                    # interfaces — outbound adapters implement these
    usecase/                 # one struct per use case (orchestration only)
  adapter/
    inbound/{httpapi,worker} # HTTP + AMQP delivery into use cases
    outbound/                # postgres, redis, rabbitmq, rendering, observability,
                             # provider/{mock,apns,fcm,twilio,sendgrid}
  platform/                  # HMAC, env config, slog logger
deploy/
  docker/                    # Dockerfile.{api,worker,migrate,janitor,outbox-relay}
  compose/                   # docker-compose.yml + prometheus.yml
migrations/                  # goose .sql files (init + seed + outbox)
test/integration/            # end-to-end tests behind the `integration` build tag
scripts/                     # sign-and-submit.sh
configs/                     # config.example.yaml (informational)
architecture-specifications.md
README.md (this file)
```

---

## Common make targets

| Target                | Action                                                                   |
| --------------------- | ------------------------------------------------------------------------ |
| `make tidy`           | `go mod tidy`                                                            |
| `make build`          | `go build ./...`                                                         |
| `make test`           | Unit tests with `-race`                                                  |
| `make test-integration` | Integration tests against the compose stack                            |
| `make lint`           | `go vet ./...`                                                           |
| `make up` / `down`    | Start / tear down the docker-compose stack                               |
| `make logs`           | Follow combined container logs                                           |
| `make migrate`        | Re-run goose migrations against the running stack                        |
| `make curl-submit`    | Sign + POST a sample notification via curl                               |

---

## Wiring a real provider

Switching to real third-party delivery is a config flip plus credentials. Set `PROVIDER_MODE=real` and supply the env vars below; `cmd/worker/main.go::buildProvider` selects the right adapter for `WORKER_CHANNEL`.

| Channel        | Adapter                                            | Required env                                                                  |
| -------------- | -------------------------------------------------- | ----------------------------------------------------------------------------- |
| `push_ios`     | `internal/adapter/outbound/provider/apns`          | `APNS_BUNDLE_ID`, `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_AUTH_KEY`              |
| `push_android` | `internal/adapter/outbound/provider/fcm`           | `FCM_PROJECT_ID`, `FCM_CREDENTIALS_JSON` (path to service-account JSON)       |
| `sms`          | `internal/adapter/outbound/provider/twilio`        | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_FROM_NUMBER`               |
| `email`        | `internal/adapter/outbound/provider/sendgrid`      | `SENDGRID_API_KEY`, `SENDGRID_FROM_EMAIL`, `SENDGRID_FROM_NAME` (optional)    |

The HTTP shape of each adapter is locked in by httptest-backed unit tests (transient vs terminal mapping included). The two pieces still left for a real deployment are pluggable:

- **APNs JWT signer**: `cmd/worker/main.go::buildAPNSAuth` returns an `apns.Authenticator` stub today. Slot in an ES256 signer (e.g. `golang-jwt/jwt`) over the `.p8` key with a 50-minute rotation.
- **FCM OAuth2 token source**: `cmd/worker/main.go::buildFCMTokenSource` is the equivalent stub for FCM. Wire `golang.org/x/oauth2/google` against the `https://www.googleapis.com/auth/firebase.messaging` scope.

Each adapter implements `port.NotificationProvider.Send(ctx, *domain.Notification) error`. Returning `port.ErrTransient` puts the message into the retry queue; any other non-nil error dead-letters it immediately.

---

## License

(none specified)
