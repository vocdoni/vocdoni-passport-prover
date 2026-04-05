# Vocdoni Passport Server

HTTP server for the Vocdoni Passport proving system.

## Overview

This server provides:

- **Operator Dashboard**: Web UI for creating and managing petitions
- **Mobile API**: Endpoints for the Vocdoni Passport mobile app
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
