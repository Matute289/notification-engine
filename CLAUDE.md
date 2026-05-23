# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go notification engine implementing the design from chapter 10 of *System Design Interview Vol. 1* (`Notification_System.pdf` in this repo). API service + per-channel workers, backed by Postgres, Redis, and RabbitMQ, all runnable via docker-compose. Channels: iOS push, Android push, SMS, email. Locally everything is wired to a **mock provider** so the stack runs without third-party credentials.

Read `CLAUDE_CONTEXT.md` first — it captures the working design, the layer rules, file index, and outstanding follow-ups. The end-user-facing design lives in `architecture-specifications.md`.

## Project layout

```
NotificationEngine/
  cmd/
    api/
      http/               ← HTTP driving adapter (routing, middleware wiring, RouterConfig)
        router.go         (package httpapi)
        handlers/
          handler.go            (Handler struct + writeJSON)
          error.go              (mapDomainError + writeError)
          submit_notification.go
          get_notification.go
          create_template.go
          get_template.go
          update_setting.go
          register_device.go
          fakes_test.go         (shared port fakes + withURLParam helper)
          *_test.go             (one test file per handler + error_test.go)
        dto/              (package dto — one file per exported DTO type)
      main.go             ← composition root for API service
    worker/
      consumer/
        consumer.go       (package consumer — AMQP Consumer struct)
      main.go             ← composition root for channel worker
    janitor/main.go       ← composition root for stuck-notification janitor
    outbox-relay/main.go  ← composition root for outbox relay
  middleware/             ← HTTP middleware (RequestID, Recoverer, AccessLog, HMACAuth, AppKeyRateLimit)
  observability/
    logger/logger.go      (package logger — slog NewLogger)
    metrics/metrics.go    (package metrics — Prometheus MetricsRecorder impl)
  infrastructure/
    postgres/             ← NotificationRepository, UserRepository, OutboxRepository
    mongodb/              ← TemplateRepository (templates + media URLs)
    redis/                ← RateLimiter, Deduper, TemplateCache (L2 decorator)
    rabbitmq/             ← EventPublisher + topology setup
    rendering/            ← TemplateRenderer impl (in-process L1 cache)
    provider/
      mock/               ← mock NotificationProvider (used locally)
      apns/, fcm/, twilio/, sendgrid/  ← real provider skeletons
  internal/
    domain/               ← entities, value objects, sentinel errors, state machine
    port/                 ← outbound port interfaces (what services need from infrastructure)
    service/              ← one struct + Execute() per use case (SubmitNotification, ProcessNotification, …)
    platform/
      auth/               ← HMAC verifier
      config/             ← env-based config loader
  migrations/             ← goose SQL migrations
  deploy/                 ← Dockerfiles + docker-compose
  test/integration/       ← end-to-end test (build tag: integration)
  configs/, scripts/, Makefile
```

## Architectural rules

- **`internal/domain/`** — entities, value objects, sentinel errors. Imports nothing outside its own package and `time`/`uuid`. Owns its invariants.
- **`internal/port/`** — interfaces only. Defined by what services need. No imports of any infrastructure package.
- **`internal/service/`** — orchestration. Depends only on `domain` + `port`. One struct per use case, single `Execute` method.
- **`middleware/`** — HTTP middleware. Imports `internal/port` and `internal/platform/auth`.
- **`infrastructure/{postgres,mongodb,redis,rabbitmq,rendering,provider}/`** — driven adapters. Implement ports. Each has a compile-time assertion `var _ port.X = (*Y)(nil)`.
- **`observability/logger/`** — slog logger (cross-cutting).
- **`observability/metrics/`** — Prometheus MetricsRecorder implementation.
- **`cmd/api/http/`** and **`cmd/worker/consumer/`** — driving adapters that live inside their composition root directories. They translate HTTP/AMQP into service input; they contain no business logic.
- **`cmd/{api,worker,janitor,outbox-relay}/main.go`** — composition roots. Only place a concrete infrastructure adapter is bound to a port.

Import direction: `domain ← port ← service ← {infrastructure, middleware, observability, cmd/.../http, cmd/.../consumer} ← cmd/*/main.go`

**Exception**: `cmd/api/http/` and `cmd/worker/consumer/` are driving adapters that live inside `cmd/` for colocation with their composition root — they are NOT composition roots themselves.

Packages outside `internal/` (`infrastructure/`, `middleware/`, `observability/`) are importable by other modules; they contain no public API surface beyond what the engine exposes internally.

## Commands

```bash
# Build + vet
go build ./...
go vet ./...

# Unit tests (fast, no I/O — services run against in-memory port fakes)
go test -race -count=1 ./...

# Run a single test
go test -race -run TestSubmit_HappyPath ./internal/service/...

# Bring up the full stack (postgres + redis + rabbitmq + api + 4 workers + one-shot migrate)
make up
make logs
make down

# Integration tests against the running stack
make test-integration

# Issue a signed sample notification
APP_KEY=demo-app APP_SECRET=demo-secret-please-change ./scripts/sign-and-submit.sh
```

## Conventions

- Errors: every infrastructure adapter wraps with `fmt.Errorf("adapter-tag: %w", err)`. Services surface `domain.Err*` sentinels (`ErrNotFound`, `ErrInvalidInput`, `ErrOptedOut`, `ErrRateLimited`, `ErrInvalidStatusTransition`). HTTP handler maps these to status codes via `errors.Is`.
- HTTP errors: `{ "code", "message" }` JSON only.
- Domain types are persistence-agnostic. Infrastructure adapters convert to/from rows / messages.
- Tests of services use the fakes in `internal/service/fakes_test.go` (one fake per port, satisfied by compile-time `var _ port.X = (*Y)(nil)`). Infrastructure adapter tests only cover the adapter (e.g. miniredis for the Redis adapter); business logic is covered at the service layer.
- Handler tests in `cmd/api/http/handlers/` use `package handlers` (white-box) to access unexported `mapDomainError`/`writeError`. Each handler has its own `*_test.go`; shared port fakes live in `fakes_test.go`. Tests drive handlers via `httptest.NewRecorder` + chi route-context injection (`withURLParam`).
- HMAC: clients sign `timestamp \n method \n path \n raw-body` with SHA-256, send `X-App-Key`, `X-App-Timestamp`, `X-App-Signature`.
- Handler struct fields use the `Svc` suffix (`SubmitSvc`, `GetSvc`, `CreateTemplateSvc`, …) to avoid name collisions with the exported handler methods.
