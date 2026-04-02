# Fixtures

Fixtures are the main proving test input for `vocdoni-passport-prover`.

Rules:

- capture real document data once on device,
- export only replayable proof inputs,
- run local proving tests from these files,
- do not depend on mobile UI state.

Recommended fixture directory layout:

- `fixtures/real/<fixture-id>/document.json`
- `fixtures/real/<fixture-id>/request.json`
- `fixtures/real/<fixture-id>/expected_outer.json`

Useful commands:

```bash
cargo run -p prover-cli -- validate-fixture --dir fixtures/examples/minimal
cargo run -p prover-cli -- plan-request --dir fixtures/examples/minimal
cargo run -p prover-cli -- print-example-fixture
cargo run -p prover-cli -- show-matrix
cargo run -p prover-cli -- resolve-circuits \
  --context fixtures/examples/minimal/document-circuit-context.example.json \
  --manifest artifacts/registry/minimal-default-0.16.0/manifest.json
cargo run -p prover-cli -- analyze-document --dir fixtures/examples/debug-john
cargo run -p prover-cli -- resolve-document-circuits \
  --dir fixtures/examples/debug-john \
  --certificates fixtures/examples/debug-john/certificates.json \
  --artifacts-dir artifacts/registry/minimal-default-0.16.0
```

Offline-first artifact workflow:

```bash
cargo run -p prover-cli -- snapshot-registry \
  --dir fixtures/examples/minimal \
  --circuit data_check_integrity_sa_sha256_dg_sha256 \
  --output snapshots/registry/minimal-default-0.16.0.json

cargo run -p prover-cli -- materialize-snapshot \
  --snapshot snapshots/registry/minimal-default-0.16.0.json \
  --out-dir artifacts/registry/minimal-default-0.16.0
```
