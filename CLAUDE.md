# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go notification engine implementing the design from chapter 10 of *System Design Interview Vol. 1* (`Notification_System.pdf` in this repo). API service + per-channel workers, backed by Postgres, Redis, and RabbitMQ, all runnable via docker-compose. Channels: iOS push, Android push, SMS, email. Locally everything is wired to a **mock provider** so the stack runs without third-party credentials.

The codebase is organised in **hexagonal / ports-and-adapters** style. Read `CLAUDE_CONTEXT.md` first — it captures the working design, the layer rules, file index, and outstanding follow-ups. The end-user-facing design lives in `architecture-specifications.md` (when present).

## Architectural rules (do not violate)

- **`internal/domain/`** — entities, value objects, sentinel errors. Imports nothing outside its own package and `time`/`uuid`. Owns its invariants (e.g. `Notification.MarkSent()` enforces state-machine transitions).
- **`internal/app/port/`** — interfaces only. Defined by what the application *needs*. No imports of any adapter.
- **`internal/app/usecase/`** — orchestration. Depends only on `domain` + `port`. One struct per use case, single `Execute` method.
- **`internal/adapter/inbound/{httpapi,worker}/`** — driving adapters. Translate transport (HTTP, AMQP) into use-case input. Never contain business logic.
- **`internal/adapter/outbound/{postgres,redis,rabbitmq,provider/mock,rendering,observability}/`** — driven adapters. Implement ports. Each has a compile-time assertion `var _ port.X = (*Y)(nil)`.
- **`internal/platform/{auth,config,observability}/`** — cross-cutting infra (HMAC, env config, slog logger).
- **`cmd/{api,worker}/main.go`** — composition roots. Only place a concrete adapter is bound to a port.

Direction of imports: domain ← app ← adapter ← cmd. Never the other way.

## Commands

```bash
# Build + vet
go build ./...
go vet ./...

# Unit tests (fast, no I/O — use cases run against in-memory port fakes)
go test -race -count=1 ./...

# Run a single test
go test -race -run TestSubmit_HappyPath ./internal/app/usecase/...

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

- Errors: every adapter wraps with `fmt.Errorf("adapter-tag: %w", err)`. Use cases surface `domain.Err*` sentinels (`ErrNotFound`, `ErrInvalidInput`, `ErrOptedOut`, `ErrRateLimited`, `ErrInvalidStatusTransition`). HTTP handler maps these to status codes via `errors.Is`.
- HTTP errors: `{ "code", "message" }` JSON only.
- Domain types are persistence-agnostic. Adapters convert to/from rows / messages.
- Tests of use cases use the fakes in `internal/app/usecase/fakes_test.go` (one fake per port, satisfied by compile-time `var _ port.X = (*Y)(nil)`). Adapter tests only cover the adapter (e.g. miniredis for the Redis adapter); business logic is covered at the use-case layer.
- HMAC: clients sign `timestamp \n method \n path \n raw-body` with SHA-256, send `X-App-Key`, `X-App-Timestamp`, `X-App-Signature`.
