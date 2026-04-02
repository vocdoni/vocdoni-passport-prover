# server-go

Go HTTP and orchestration server embedded inside `vocdoni-passport-prover`.

This directory exists so the proving stack and the server that consumes it live in the same repository.

The rule is:

- proving versions, artifacts, and helper scripts are decided by `vocdoni-passport-prover`
- `server-go` is the thin HTTP layer that consumes that local proving stack

## Responsibilities

`server-go` does five things:

- exposes the request JSON payload used by the app
- renders the operator page with QR and copyable request link
- serves the Android APK mounted under `server-go/apk/`
- accepts inner proof bundles from the mobile app
- invokes `prover-cli aggregate-request` and returns the outer proof

It does not implement proving logic itself.

## Directory Layout

- `cmd/main.go`
  process bootstrap and runtime config
- `api/server.go`
  HTTP routes and middleware
- `api/request.go`
  operator page, QR, request payload
- `proving/service.go`
  subprocess bridge to `prover-cli`
- `docker-compose.yml`
  local deployment entrypoint
- `Dockerfile`
  deployable server image
- `.env.example`
  tracked runtime template
- `apk/`
  local directory served at `/downloads/app-release.apk`

## Endpoints

- `GET /`
  operator page
- `GET /api/request-config`
  app-consumable JSON payload
- `GET /api/request-qr.png`
  QR image for the same payload
- `GET /api/health`
  liveness endpoint
- `GET /downloads/app-release.apk`
  current Android APK mounted into the container
- `POST /api/proofs/aggregate`
  accepts the app `InnerProofPackage` and returns the outer proof

## Runtime Contract

The server image contains:

- the Go server binary
- `prover-cli`
- a zkPassport-compatible `bb`
- prover config, artifacts, and helper scripts
- the runtime subset of `zkpassport-utils`

The Android APK is provided at runtime from `server-go/apk/`.

## Runtime Configuration

Set these values in `.env.example`, or copy it to `.env` for local overrides. The top-level `make server-*` targets use `.env` when present and otherwise fall back to `.env.example`.

- `VOCDONI_PUBLIC_BASE_URL`
  full phone-reachable base URL
- `HOST_PORT`
  published local port
- `VOCDONI_SERVER_LISTEN`
  bind address inside the container
- `VOCDONI_LOG_LEVEL`
  zerolog level
- `VOCDONI_APK_PATH`
  path served at `/downloads/app-release.apk`
- `VOCDONI_PROVER_BINARY_PATH`
  path to `prover-cli`
- `BB_BINARY_PATH`
  path to the local zkPassport-compatible `bb`
- `VOCDONI_WORKSPACE_ROOT`
  prover workspace root inside the image
- `VOCDONI_ARTIFACTS_DIR`
  active artifact directory
- `VOCDONI_PROVER_TIMEOUT`
  maximum aggregate job duration
- `VOCDONI_PROVER_LOW_MEMORY_MODE`
  enable low-memory aggregation mode
- `VOCDONI_PROVER_MAX_STORAGE_USAGE`
  storage and memory pressure limiter
- `VOCDONI_PROVER_MAX_CONCURRENCY`
  maximum concurrent aggregate jobs

## Local Operation

Run commands from this directory.

Place the Android APK at `apk/app-release.apk` before startup if you want the download endpoint to serve it.

Start:

```bash
docker compose --env-file .env.example up -d --build
```

Health:

```bash
curl -fsS "$VOCDONI_PUBLIC_BASE_URL/api/health"
```

Logs:

```bash
docker compose --env-file .env.example logs -f server
```

Stop:

```bash
docker compose --env-file .env.example down --remove-orphans
```

## Docker Strategy

The Dockerfile is server-focused, but it compiles the Rust prover from the same repository.

That is intentional:

- one repository decides the proving stack
- the server image packages that exact stack
- no host-side `bb` or `prover-cli` installation is required

## Upgrade Rules

When zkPassport circuits, `bb`, Noir, helper scripts, or the APK change:

1. update and validate the proving stack in the parent repository
2. rebuild this server image
3. verify:
   the request payload flow
4. verify:
   the aggregate flow
5. verify:
   the APK download endpoint

Do not treat `server-go` as a separate versioning authority.

## Why Go Calls `prover-cli`

This is the current clean boundary:

- Go owns HTTP and request handling
- Rust owns proving
- the interface between them is a stable JSON and file contract

That keeps the server small and lets the proving stack evolve in one place.
