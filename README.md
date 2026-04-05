# Vocdoni Passport Prover

Server-side zero-knowledge proof generation for the Vocdoni Passport system.

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![CI](https://github.com/vocdoni/vocdoni-passport-prover/actions/workflows/ci.yml/badge.svg)](https://github.com/vocdoni/vocdoni-passport-prover/actions/workflows/ci.yml)
[![Docker](https://github.com/vocdoni/vocdoni-passport-prover/actions/workflows/docker.yml/badge.svg)](https://github.com/vocdoni/vocdoni-passport-prover/actions/workflows/docker.yml)

## Overview

This repository is the canonical source for the Vocdoni Passport proving stack. It contains:

- **prover-cli**: Command-line tool for zero-knowledge proof generation
- **server-go**: HTTP server that orchestrates proof generation and serves the mobile app
- **acvm-witness-jni**: Native library for witness solving (used by the mobile app)
- **Circuit artifacts**: Packaged zkPassport circuits and verification keys

### Architecture

```
┌─────────────────┐     ┌─────────────────────────────────────┐
│  Mobile App     │     │  vocdoni-passport-prover            │
│  (vocdoni-      │────▶│  ┌─────────────┐  ┌──────────────┐  │
│   passport)     │     │  │  server-go  │──│  prover-cli  │  │
└─────────────────┘     │  └─────────────┘  └──────────────┘  │
                        │         │                │          │
                        │         ▼                ▼          │
                        │  ┌─────────────┐  ┌──────────────┐  │
                        │  │   MongoDB   │  │  bb (prover) │  │
                        │  └─────────────┘  └──────────────┘  │
                        └─────────────────────────────────────┘
```

The mobile app generates **inner proofs** on-device and sends them to this server, which generates the **outer proof** that can be verified on-chain.

## Quick Start

### Using Docker Compose (Recommended)

```bash
cd server-go

# Copy and configure environment
cp .env.example .env
# Edit .env with your settings

# Start the server
docker compose up -d --build

# Check health
curl http://localhost:8080/api/health

# View logs
docker compose logs -f server
```

### Server Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Operator dashboard with QR code |
| `/api/health` | GET | Health check |
| `/api/request-config` | GET | Petition configuration for mobile app |
| `/api/request-qr.png` | GET | QR code image |
| `/api/proofs/aggregate` | POST | Submit inner proofs, receive outer proof |
| `/downloads/app-release.apk` | GET | Android APK download |

## Project Structure

```
├── crates/
│   ├── acvm-witness-jni/   # Native witness solver (mobile FFI)
│   ├── prover-cli/         # CLI for proof generation
│   ├── prover-core/        # Core proving logic
│   └── prover-types/       # Shared data structures
├── server-go/              # HTTP server
│   ├── api/                # HTTP handlers
│   ├── cmd/                # Server entrypoint
│   ├── proving/            # Prover subprocess bridge
│   └── storage/            # Petition storage (MongoDB)
├── artifacts/              # zkPassport circuit artifacts
├── config/                 # Compatibility metadata
├── scripts/                # Helper scripts
├── docker/                 # Docker utilities
└── Dockerfile              # Prover CLI image
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `VOCDONI_PUBLIC_BASE_URL` | Public URL for QR codes | Required |
| `VOCDONI_SERVER_LISTEN` | Server bind address | `0.0.0.0:8080` |
| `VOCDONI_LOG_LEVEL` | Log level (debug/info/warn/error) | `info` |
| `VOCDONI_MONGODB_URI` | MongoDB connection string | `mongodb://mongo:27017` |
| `VOCDONI_PROVER_TIMEOUT` | Max proof generation time | `10m` |
| `VOCDONI_PROVER_MAX_CONCURRENCY` | Max concurrent proofs | `1` |

See `server-go/.env.example` for all options.

## Development

### Prerequisites

- Rust 1.89+
- Go 1.24+
- Docker & Docker Compose

### Building Locally

```bash
# Build Rust components
cargo build --release

# Run tests
cargo test

# Build Go server
cd server-go && go build -o server ./cmd
```

### CLI Usage

```bash
# Show compatibility matrix
cargo run -p prover-cli -- show-matrix

# Validate a fixture
cargo run -p prover-cli -- validate-fixture --dir fixtures/examples/minimal

# Generate inner proofs
cargo run -p prover-cli --features native-prover -- \
  prove-fixture-inner \
  --dir /path/to/fixture \
  --artifacts-dir artifacts/registry/minimal-default-0.16.0

# Generate outer proof
cargo run -p prover-cli --features native-prover -- \
  prove-fixture-outer \
  --dir /path/to/fixture \
  --artifacts-dir artifacts/registry/minimal-default-0.16.0
```

## Docker Images

### Server Image (Recommended)

The server image includes everything needed for production:

```bash
# Build from repository root
docker build -f server-go/Dockerfile -t vocdoni-passport-server .

# Run
docker run -p 8080:8080 vocdoni-passport-server
```

### Prover CLI Image

For standalone proof generation:

```bash
docker build -t vocdoni-passport-prover .
docker run --rm vocdoni-passport-prover show-matrix
```

## Deployment

### Production Checklist

1. Configure `VOCDONI_PUBLIC_BASE_URL` with your public domain
2. Set up MongoDB for petition storage
3. Place the Android APK in `server-go/apk/`
4. Configure TLS termination (nginx, Caddy, etc.)
5. Set appropriate resource limits for proof generation

### Resource Requirements

| Component | CPU | Memory | Notes |
|-----------|-----|--------|-------|
| Server | 2+ cores | 4GB | HTTP handling |
| Prover | 8+ cores | 32GB | Proof generation |
| MongoDB | 1 core | 1GB | Petition storage |

Proof generation benefits significantly from:
- AVX-512 capable CPUs (Intel Skylake-X+, AMD Zen 4+)
- High memory bandwidth
- SSD storage

## Upgrading

### Circuit Artifacts

1. Update `artifacts/` and `config/compatibility-matrix.json`
2. Run prover tests with real fixtures
3. Rebuild server image
4. Deploy and verify end-to-end flow

### Barretenberg (bb)

1. Update `AZTEC_PACKAGES_REF` in Dockerfiles
2. Verify inner proofs against packaged VKs
3. Verify outer proof generation
4. Test full mobile app flow

## Related Projects

- [vocdoni-passport](https://github.com/vocdoni/vocdoni-passport) - Mobile application
- [zkPassport](https://zkpassport.id) - Zero-knowledge passport protocol

## Contributing

Contributions are welcome! Please read our [Contributing Guidelines](CONTRIBUTING.md) before submitting a pull request.

## Security

For security issues, please email security@vocdoni.io instead of opening a public issue.

## License

This project is licensed under the [GNU Affero General Public License v3.0](LICENSE).

---

Built with ❤️ by [Vocdoni](https://vocdoni.io)
