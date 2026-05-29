# Deploying to Render

This guide covers deploying the Notification Engine to **Render** using the
`render.yaml` Blueprint in this branch. All services track the **`render` branch**
(not `main`), so Render always builds from the Render-specific commit.

> **This file only exists on the `render` branch.** `main` contains no
> Render-specific files, making it safe to fork or clone without picking up
> any platform config.

---

## What the Blueprint provisions

| Resource | Type | Notes |
| -------- | ---- | ----- |
| `notification-postgres` | Postgres (Render-managed) | Free plan; upgrade to `starter`/`standard` for production |
| `notification-redis` | Redis (Render Key Value) | Free plan; private network only |
| `notification-api` | Web service (Docker) | Exposes `:8080`; health check at `/healthz` |
| `worker-push-ios` | Background worker (Docker) | Consumes `push_ios` queue |
| `worker-push-android` | Background worker (Docker) | Consumes `push_android` queue |
| `worker-sms` | Background worker (Docker) | Consumes `sms` queue |
| `worker-email` | Background worker (Docker) | Consumes `email` queue |
| `janitor` | Background worker (Docker) | Rescues stuck `in_flight` notifications |
| `outbox-relay` | Background worker (Docker) | Drains `notification_outbox` → RabbitMQ |
| `worker-shared` | Env var group | Shared config inherited by all workers |

**External services** (not managed by Render — choose your own providers):

- **RabbitMQ**: [CloudAMQP](https://cloudamqp.com) free "Lemur" plan, or any
  RabbitMQ provider / self-hosted instance.
- **MongoDB**: [MongoDB Atlas](https://mongodb.com/atlas) free M0 cluster, or any
  MongoDB provider / self-hosted instance.
- **Authentication** (optional): [Clerk](https://clerk.dev) or any OpenID JWT
  issuer; or skip JWT entirely and use HMAC-only mode.

---

## Step-by-step deployment

### 1. Set up external services

Before applying the Blueprint you need connection strings for RabbitMQ and MongoDB.

**RabbitMQ (example: CloudAMQP)**
1. Sign up at https://cloudamqp.com → Create a new instance (free "Lemur" plan).
2. Copy the **AMQP URL** — it looks like `amqps://user:pass@host/vhost`.

**MongoDB (example: Atlas)**
1. Sign up at https://mongodb.com/atlas → Create a free M0 cluster.
2. Database Access → Add a database user with `readWrite` on `notification_engine`.
3. Network Access → Allow connections from `0.0.0.0/0` (or Render's IP ranges).
4. Clusters → Connect → Drivers → copy the connection string.
5. **Encode any special characters in the password** (e.g. `@` → `%40`). The URI
   must be a valid `mongodb+srv://` string.

### 2. Apply the Blueprint

1. Render dashboard → **New → Blueprint** → select this repo → choose branch **`render`**.
2. Fill in the `sync: false` secrets that appear in the Blueprint form:
   - `RABBITMQ_URL` — the AMQP URL from CloudAMQP (or your provider)
   - `MONGODB_URI` — the MongoDB connection string
   - `CLERK_ISSUER` — e.g. `https://<slug>.clerk.accounts.dev` (leave empty for HMAC-only)
   - `CLERK_AUTHORIZED_PARTIES` — comma-separated allowed origins (optional)
   - `APP_CLIENTS` — `key1:secret1,key2:secret2,...` (optional when using Clerk JWT)
3. Click **Apply**.

### 3. Verify the deployment

- **API** (`notification-api`): check the public URL → `GET /healthz` should return
  `{"status":"ok"}`.
- **Workers**: all 6 background workers should show status "running" on the Render
  services page.

> **Known limitation — `envVarGroups` and `sync: false`**: Render's Blueprint form
> does not surface `sync: false` variables that live only inside `envVarGroups`. After
> the Blueprint apply, navigate to **Render dashboard → Env Groups → `worker-shared`**
> and manually set `RABBITMQ_URL` and `MONGODB_URI` there if workers show connection
> errors at startup. The API service already has these vars at the service level (they
> do appear in the form), so it starts correctly without this step.

---

## CI/CD — releases and deploys

Releases are gated and explicit. The workflow is:

```
feature branch
    └─PR──► development ──PR──► main ──(auto-sync)──► render ──(manual release)──► Render
```

| Action | Who | How |
| ------ | --- | --- |
| Sync `render` from `main` | Automatic | `sync-render.yml` runs on every push to `main` |
| Tag + deploy | Manual | GitHub → Actions → **Release to Render** → enter version → Run |

### Setting up the Deploy Hook

The `release-render.yml` workflow calls a Render Deploy Hook URL to trigger the
deploy. To wire it up:

1. Render dashboard → `notification-api` → **Settings → Deploy Hook** → copy the URL.
2. GitHub repo → **Settings → Secrets → Actions → New repository secret**:
   - Name: `RENDER_DEPLOY_HOOK_URL`
   - Value: the URL from step 1.
3. In Render, **disable "Auto-Deploy"** on the `render` branch for all services so
   that pushes from `sync-render.yml` do not trigger redundant deploys — the
   `Release to Render` workflow is the sole deploy trigger.

### Triggering a release

1. GitHub → **Actions → Release to Render → Run workflow**.
2. Enter the semantic version (e.g. `1.2.3`). A tag `render-v1.2.3` will be created
   on the `render` branch and the Deploy Hook will be called.
3. The workflow runs `go test -race -count=1 ./...` on the `render` HEAD before
   tagging — a failing test aborts the release.

---

## Branch protection for `render`

To prevent accidental direct pushes:

1. GitHub → **Settings → Branches → Add rule** → Branch name pattern: `render`.
2. Enable **"Restrict who can push to matching branches"**.
3. Add the GitHub Actions bot (`github-actions[bot]`) or a dedicated PAT user as
   the only allowed pusher.

> If branch protection blocks the `GITHUB_TOKEN` used by `sync-render.yml`, create
> a fine-grained PAT with **Contents: Write** permission, store it as the
> `SYNC_TOKEN` secret, and replace `secrets.GITHUB_TOKEN` in `sync-render.yml`.

---

## Re-pointing existing Render services to the `render` branch

If services were previously tracking `main`, update each one:

Render dashboard → service → **Settings → Branch** → change to `render` → Save.
