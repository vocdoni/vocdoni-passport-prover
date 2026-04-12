# Vocdoni Passport Server

HTTP server for the Vocdoni Passport proving system.

## Overview

This server provides:

- **Operator Dashboard**: Web UI for creating and managing petitions
- **Mobile API**: Endpoints for the Vocdoni Passport mobile app
- **App Links / Universal Links**: Serves `vocdoni.link` verification files and petition deep links
- **Proof Aggregation**: Orchestrates outer proof generation from inner proofs
- **APK Distribution**: Serves the Android app for easy installation

## Quick Start

```bash
# Copy environment configuration
cp .env.example .env
# Edit .env with your settings (especially VOCDONI_PUBLIC_BASE_URL)

# Start with Docker Compose
docker compose up -d --build

# Check health
curl http://localhost:8080/api/health

# View logs
docker compose logs -f server
```

### Choosing the `bb` CPU target

The server image builds Barretenberg (`bb`) during `docker compose build`.

- `BB_TARGET_ARCH=native` builds for the CPU that runs the build
- this is best when you build on the same machine that will run the server
- if you build on one machine and deploy on another, use an explicit portable target

Common x86-64 targets:

| `BB_TARGET_ARCH` | Typical CPU class | Notes |
|------------------|-------------------|-------|
| `native` | Current build host | Best performance for same-host builds |
| `x86-64-v2` | Intel Nehalem/Westmere+, AMD Bulldozer+/Zen | Broad compatibility |
| `x86-64-v3` | Intel Haswell/Broadwell/Skylake+, AMD Zen family | Good default for most AVX2-capable servers |
| `skylake` | Intel Skylake/Cascade Lake class | Barretenberg preset default |
| `x86-64-v4` | Intel Ice Lake/Sapphire Rapids, AVX-512-capable Zen 4 systems | Highest preset, not portable to older CPUs |

`BB_TUNE_ARCH` controls `-mtune` only. For portable builds, `generic` is a safe value.

Examples:

```bash
# Build for this machine only
docker compose up -d --build

# Build for most modern x86-64 servers
BB_TARGET_ARCH=x86-64-v3 BB_TUNE_ARCH=generic docker compose up -d --build

# Build for older mixed x86-64 fleets
BB_TARGET_ARCH=x86-64-v2 BB_TUNE_ARCH=generic docker compose up -d --build
```

On `arm64` / `aarch64`, Barretenberg uses its ARM build path and ignores the x86-specific target setting.

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Operator dashboard |
| `/api/health` | GET | Health check |
| `/api/request-config` | GET | Petition configuration (JSON) |
| `/api/request-qr.png` | GET | QR code for petition |
| `/api/proofs/aggregate` | POST | Submit inner proofs |
| `/api/petitions` | GET | List petitions |
| `/api/petitions` | POST | Create petition |
| `/api/petitions/{id}` | GET | Get petition details |
| `/api/petitions/{id}/signers` | GET | List petition signers |
| `/downloads/app-release.apk` | GET | Android APK download |

## Configuration

### Environment Variables

Copy `.env.example` to `.env` and configure:

```bash
# Required: Public URL for QR codes and mobile app
VOCDONI_PUBLIC_BASE_URL=https://your-domain.com
VOCDONI_DEEPLINK_BASE_URL=https://vocdoni.link

# Server settings
VOCDONI_SERVER_LISTEN=0.0.0.0:8080
VOCDONI_LOG_LEVEL=info

# MongoDB (for petition storage)
VOCDONI_MONGODB_URI=mongodb://mongo:27017
VOCDONI_MONGODB_DATABASE=vocdoni_passport

# Prover settings
VOCDONI_PROVER_TIMEOUT=10m
VOCDONI_PROVER_MAX_CONCURRENCY=1
VOCDONI_PROVER_LOW_MEMORY_MODE=false
```

See `.env.example` for all available options.

### `vocdoni.link` support

When `VOCDONI_DEEPLINK_BASE_URL` points to `https://vocdoni.link`, the server:

- generates petition QR codes that contain `https://vocdoni.link/passport?sign=<base64url(host|petitionId)>`
- generates generic request QR codes that contain `https://vocdoni.link/passport?request=<base64url(payload-json)>`
- copies and displays `vocdoni.link` petition URLs in the operator dashboard

The mobile app opens `vocdoni.link`, reads the `/passport` namespace, and then:

- if the link contains `sign`, it decodes the compact upstream host and petition ID, reconstructs `https://<host>/petition/<id>`, and fetches the request payload from the real server
- if the link contains `request`, it decodes the embedded request payload directly

This keeps `vocdoni.link` as the verified app-link domain without requiring it to reverse proxy the whole proving service.

App-link verification for `vocdoni.link` is handled outside `server-go`, for example by a Cloudflare Worker serving the `/.well-known/...` files and browser redirects for deeplinks.

## Directory Structure

```
server-go/
├── api/           # HTTP handlers
│   ├── server.go  # Main server setup
│   ├── request.go # Petition/request handlers
│   └── proofs.go  # Proof aggregation handlers
├── cmd/           # Server entrypoint
│   └── main.go
├── proving/       # Prover subprocess bridge
│   └── service.go
├── storage/       # MongoDB storage
│   └── mongo.go
├── presets/       # Petition presets
├── apk/           # APK directory (mount point)
├── Dockerfile     # Server image
├── docker-compose.yml
└── .env.example
```

## Deployment

### With Docker Compose (Recommended)

```bash
docker compose up -d --build
```

This starts:
- The server on port 8080
- MongoDB for petition storage

### Manual Docker

```bash
# Build from repository root
docker build -f server-go/Dockerfile -t vocdoni-passport-server .

# Run
docker run -p 8080:8080 \
  -e VOCDONI_PUBLIC_BASE_URL=https://your-domain.com \
  -e VOCDONI_MONGODB_URI=mongodb://your-mongo:27017 \
  -v /path/to/apk:/opt/vocdoni/downloads:ro \
  vocdoni-passport-server
```

To build a portable x86-64 image explicitly:

```bash
docker build \
  -f server-go/Dockerfile \
  --build-arg BB_TARGET_ARCH=x86-64-v3 \
  --build-arg BB_TUNE_ARCH=generic \
  -t vocdoni-passport-server .
```

### APK Distribution

Place the Android APK at `apk/app-release.apk` before starting:

```bash
# Build APK from vocdoni-passport repository
cd ../vocdoni-passport
make apk
cp out/app-release.apk ../vocdoni-passport-prover/server-go/apk/
```

Or download from the [vocdoni-passport releases](https://github.com/vocdoni/vocdoni-passport/releases).

## Development

### Local Development

```bash
# Install dependencies
go mod download

# Run locally (requires MongoDB)
go run ./cmd

# Run tests
go test -v ./...
```

### Building

```bash
# Build binary
go build -o server ./cmd

# Build Docker image
docker build -f Dockerfile -t vocdoni-passport-server ..
```

With an explicit portable CPU target:

```bash
docker build \
  -f Dockerfile \
  --build-arg BB_TARGET_ARCH=x86-64-v3 \
  --build-arg BB_TUNE_ARCH=generic \
  -t vocdoni-passport-server ..
```

## Architecture

The server is intentionally thin - it handles HTTP and orchestration only. All proving logic lives in the Rust `prover-cli` binary.

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Mobile    │────▶│   Server    │────▶│  prover-cli │
│    App      │     │   (Go)      │     │   (Rust)    │
└─────────────┘     └─────────────┘     └─────────────┘
                           │
                           ▼
                    ┌─────────────┐
                    │   MongoDB   │
                    └─────────────┘
```

## Related

- [vocdoni-passport](https://github.com/vocdoni/vocdoni-passport) - Mobile application
- [Parent README](../README.md) - Full prover documentation
