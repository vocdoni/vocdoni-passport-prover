# vocdoni-passport-prover

Canonical proving workspace for the `v2` stack.

This repository is the source of truth for:

- `prover-cli`
- proving inputs and aggregation logic
- zkPassport circuit artifacts and compatibility metadata
- the zkPassport-compatible `bb` and CRS build recipe
- helper scripts used by proof aggregation
- the embedded Go server in `server-go/`

If zkPassport versions change, update and validate this repository first. The server should remain an orchestration layer on top of this workspace.

## Scope

This repository owns:

- fixture loading and analysis
- circuit resolution
- witness solving
- inner proof generation
- outer proof generation
- aggregate request handling
- verifier and Solidity diagnostics
- the embedded Go server in `server-go/`

This repository does not own:

- QR scanning
- MRZ camera UX
- NFC reading on the phone
- petition storage

## Workspace Layout

- `Cargo.toml`
  Rust workspace root
- `crates/acvm-witness-jni`
  native ACVM witness crate consumed by the mobile app
- `crates/prover-cli`
  CLI entrypoint
- `crates/prover-core`
  proving pipeline and artifact logic
- `crates/prover-types`
  stable data structures shared across the pipeline
- `config/compatibility-matrix.json`
  pinned compatibility metadata
- `artifacts/`
  packaged zkPassport circuits and metadata
- `scripts/`
  helper scripts for aggregation and diagnostics
- `fixtures/`
  fixture structure and examples
- `snapshots/`
  registry snapshots
- `solidity-check/`
  Solidity verifier checks
- `Dockerfile`
  canonical containerized proving stack
- `server-go/`
  Go HTTP server that consumes the local prover stack

The embedded server is self-contained:

- `server-go/docker-compose.yml` builds from this repository root
- `server-go/apk/` is the local runtime mount for the Android APK

## Why This Repository Stays Canonical

Recursive proof compatibility is sensitive to:

- zkPassport registry artifacts
- compatibility metadata
- `bb` provenance
- Noir and ACIR versions
- helper scripts
- server aggregate invocation

If those decisions are split across several repositories, upgrades become error-prone. The intended rule is:

- change proving and version logic here first
- update downstream consumers only after this repository is validated

## Pinned Upstream Inputs

The repository Dockerfile pins:

- `zkpassport/aztec-packages`
- `zkpassport/zkpassport-packages`

Those refs are explicit in the Dockerfile so upgrades remain deliberate and reviewable.

## Local Rust Usage

Run commands from this repository root.

Basic checks:

```bash
cargo check
cargo test
```

Show the active compatibility matrix:

```bash
cargo run -p prover-cli -- show-matrix
```

Validate a fixture:

```bash
cargo run -p prover-cli -- validate-fixture --dir fixtures/examples/minimal
```

Prove inner circuits for a fixture:

```bash
cargo run -p prover-cli --features native-prover -- \
  prove-fixture-inner \
  --dir /path/to/fixture \
  --artifacts-dir artifacts/registry/minimal-default-0.16.0
```

Prove the outer circuit:

```bash
cargo run -p prover-cli --features native-prover -- \
  prove-fixture-outer \
  --dir /path/to/fixture \
  --artifacts-dir artifacts/registry/minimal-default-0.16.0
```

Aggregate a server-style request:

```bash
cargo run -p prover-cli --features native-prover -- \
  aggregate-request \
  --input /tmp/request.json \
  --output /tmp/result.json
```

## Docker Usage

Build from this repository root:

```bash
docker build -t vocdoni-passport-prover .
```

Example command:

```bash
docker run --rm vocdoni-passport-prover show-matrix
```

The image contains:

- `prover-cli`
- a zkPassport-compatible `bb`
- CRS files
- local prover config, artifacts, and scripts
- the minimum `zkpassport-utils` runtime needed by helper scripts

## Dockerfile Design Notes

The Dockerfile does a few non-obvious things deliberately:

- builds `bb` from the zkPassport-compatible Aztec fork instead of using an arbitrary upstream release
- rewrites the stale `msgpack` commit pin because that historical pin is no longer fetchable
- clones `zkpassport-packages` only to build the runtime pieces of `zkpassport-utils`
- keeps `prover-cli`, `bb`, artifacts, and helper scripts in one image

That keeps proving upgrades centralized in one place.

## Upgrade Guide

### Upgrade zkPassport circuits and manifests

1. Update `artifacts/` and `config/compatibility-matrix.json`.
2. Re-run prover tests and the real-fixture flow.
3. Rebuild the server image after the prover path is validated.

### Upgrade `bb` and `aztec-packages`

1. Change the pinned `AZTEC_PACKAGES_REF` in `Dockerfile`.
2. Verify:
   inner proofs verify against packaged VKs.
3. Verify:
   outer proof verifies locally and through the zkPassport path.
4. Remove or adjust the `msgpack` patch if upstream changed.

Do not treat `bb` as an isolated package bump.

### Upgrade `zkpassport-packages`

1. Change `ZKPASSPORT_PACKAGES_REF` in `Dockerfile`.
2. Verify helper scripts and SDK-side diagnostics still pass.

### Upgrade Noir and ACIR

1. Update the Noir dependencies in `Cargo.toml`.
2. Confirm the packaged circuits still match the declared versions.
3. Re-run fixture solving and proof verification before touching the server or app.

## Relationship With The Server

The server should consume this repository, not compete with it.

Practically, that means:

- proving flags and version pins belong here
- server subprocess invocation should adapt to this repository
- if the output shape changes, update the server after this repository is validated

The embedded Go server lives in `server-go/`.

## Relationship With The Mobile App

The phone generates inner proofs and sends them to the server. The reproducible proving and debugging workflow belongs here.

The intended testing model is:

- capture document data once on device
- replay and debug locally through this workspace
- use the app for capture and integration verification

## Maintenance Rules

- Do not hardcode machine-specific paths or private hostnames in docs or scripts.
- Use repository-relative paths in examples.
- Keep Dockerfiles and READMEs written as public repository artifacts, not session notes.
- Treat version changes as compatibility work, not package bumps.
