SHELL := /bin/bash

.PHONY: tidy build test test-integration lint up down logs migrate seed sign curl-submit

tidy:
	go mod tidy

build:
	go build ./...

test:
	go test -race -count=1 ./...

# Run integration tests against a running compose stack (make up first).
test-integration:
	go test -race -count=1 -tags=integration ./test/integration/...

lint:
	go vet ./...

up:
	docker compose -f deploy/compose/docker-compose.yml up --build -d

down:
	docker compose -f deploy/compose/docker-compose.yml down -v

logs:
	docker compose -f deploy/compose/docker-compose.yml logs -f --tail=200

# Run migrations against a locally-running postgres on :5432.
migrate:
	docker compose -f deploy/compose/docker-compose.yml run --rm migrate

# Sign and submit a sample notification (requires curl + python3 for hmac).
APP_KEY ?= demo-app
APP_SECRET ?= demo-secret-please-change
HOST ?= http://localhost:8080
PATH_ ?= /v1/notifications
BODY ?= '{"event_id":"evt-1","channel":"email","recipient":{"user_id":1},"template_id":"11111111-1111-1111-1111-111111111111","variables":{"Name":"Maticio","Product":"Notif"}}'

curl-submit:
	@TS=$$(date +%s); \
	SIG=$$(printf "%s\n%s\n%s\n%s" $$TS POST $(PATH_) $(BODY) | openssl dgst -sha256 -hmac $(APP_SECRET) | awk '{print $$2}'); \
	echo "ts=$$TS sig=$$SIG"; \
	curl -sS -X POST $(HOST)$(PATH_) \
	  -H "Content-Type: application/json" \
	  -H "X-App-Key: $(APP_KEY)" \
	  -H "X-App-Timestamp: $$TS" \
	  -H "X-App-Signature: $$SIG" \
	  -d $(BODY) | jq .
