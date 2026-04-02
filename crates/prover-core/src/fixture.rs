use anyhow::{Context, Result};
use prover_types::{DocumentInput, FixtureBundle, OuterProofResult, ProofRequest};
use std::fs;
use std::path::Path;

pub fn load_fixture_bundle(path: &Path) -> Result<FixtureBundle> {
    let raw = fs::read_to_string(path)
        .with_context(|| format!("failed to read fixture bundle: {}", path.display()))?;
    let bundle: FixtureBundle = serde_json::from_str(&raw)
        .with_context(|| format!("failed to parse fixture bundle: {}", path.display()))?;
    bundle
        .validate()
        .context("fixture bundle validation failed")?;
    Ok(bundle)
}

pub fn load_fixture_dir(dir: &Path) -> Result<FixtureBundle> {
    let document_path = dir.join("document.json");
    let request_path = dir.join("request.json");
    let expected_outer_path = dir.join("expected_outer.json");

    let document: DocumentInput = serde_json::from_str(
        &fs::read_to_string(&document_path)
            .with_context(|| format!("failed to read {}", document_path.display()))?,
    )
    .with_context(|| format!("failed to parse {}", document_path.display()))?;

    let request: ProofRequest = serde_json::from_str(
        &fs::read_to_string(&request_path)
            .with_context(|| format!("failed to read {}", request_path.display()))?,
    )
    .with_context(|| format!("failed to parse {}", request_path.display()))?;

    let expected_outer = if expected_outer_path.exists() {
        let result: OuterProofResult = serde_json::from_str(
            &fs::read_to_string(&expected_outer_path)
                .with_context(|| format!("failed to read {}", expected_outer_path.display()))?,
        )
        .with_context(|| format!("failed to parse {}", expected_outer_path.display()))?;
        Some(result)
    } else {
        None
    };

    let bundle = FixtureBundle {
        document,
        request,
        expected_inner: None,
        expected_outer,
        metadata: Default::default(),
    };
    bundle
        .validate()
        .context("fixture directory validation failed")?;
    Ok(bundle)
}
