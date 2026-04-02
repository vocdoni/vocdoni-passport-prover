use crate::config::CompatibilityMatrix;
use anyhow::{Context, Result};
use flate2::read::GzDecoder;
use reqwest::blocking::Client;
use serde::{Deserialize, Serialize};
use std::fs;
use std::io::Read;
use std::path::Path;
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Manifest {
    pub version: String,
    pub root: String,
    pub circuits: std::collections::BTreeMap<String, ManifestCircuit>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ManifestCircuit {
    pub hash: String,
    pub size: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PackagedCircuit {
    pub name: Option<String>,
    pub noir_version: Option<String>,
    pub bb_version: Option<String>,
    pub size: Option<u64>,
    pub abi: serde_json::Value,
    pub bytecode: String,
    pub vkey: String,
    pub vkey_hash: String,
    pub hash: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegistrySnapshot {
    pub generated_at_unix: u64,
    pub manifest_version: String,
    pub manifest_root: String,
    pub circuits: Vec<CircuitFingerprint>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CircuitFingerprint {
    pub name: String,
    pub manifest_hash: String,
    pub manifest_size: u64,
    pub noir_version: Option<String>,
    pub bb_version: Option<String>,
    pub vkey_hash: String,
}

#[derive(Debug, Clone)]
pub struct RegistryClient {
    http: Client,
    manifest_base_url: String,
    artifact_base_url: String,
}

impl RegistryClient {
    pub fn from_matrix(matrix: &CompatibilityMatrix) -> Self {
        Self {
            http: Client::new(),
            manifest_base_url: matrix.registry.manifest_base_url.clone(),
            artifact_base_url: matrix.registry.artifact_base_url.clone(),
        }
    }

    pub fn fetch_manifest(&self, version: &str) -> Result<Manifest> {
        let url = format!("{}/{}/manifest.json", self.manifest_base_url, version);
        let bytes = self
            .http
            .get(&url)
            .send()
            .with_context(|| format!("failed to GET manifest: {url}"))?
            .error_for_status()
            .with_context(|| format!("manifest request failed: {url}"))?
            .bytes()
            .with_context(|| format!("failed to read manifest bytes: {url}"))?;
        decode_json_bytes::<Manifest>(&bytes, &url)
    }

    pub fn fetch_packaged_circuit(
        &self,
        manifest: &Manifest,
        circuit_name: &str,
    ) -> Result<PackagedCircuit> {
        let node = manifest
            .circuits
            .get(circuit_name)
            .with_context(|| format!("circuit not found in manifest: {circuit_name}"))?;
        let url = format!("{}/{}.json", self.artifact_base_url, node.hash);
        let bytes = self
            .http
            .get(&url)
            .send()
            .with_context(|| format!("failed to GET artifact: {url}"))?
            .error_for_status()
            .with_context(|| format!("artifact request failed: {url}"))?
            .bytes()
            .with_context(|| format!("failed to read artifact bytes: {url}"))?;
        decode_json_bytes::<PackagedCircuit>(&bytes, &url)
    }

    pub fn snapshot_circuits(
        &self,
        version: &str,
        circuit_names: &[String],
    ) -> Result<RegistrySnapshot> {
        let manifest = self.fetch_manifest(version)?;
        let mut unique_circuits = circuit_names.to_vec();
        unique_circuits.sort();
        unique_circuits.dedup();

        let circuits = unique_circuits
            .into_iter()
            .map(|name| {
                let node = manifest
                    .circuits
                    .get(&name)
                    .with_context(|| format!("circuit not found in manifest: {name}"))?;
                let artifact = self.fetch_packaged_circuit(&manifest, &name)?;
                Ok(CircuitFingerprint {
                    name,
                    manifest_hash: node.hash.clone(),
                    manifest_size: node.size,
                    noir_version: artifact.noir_version,
                    bb_version: artifact.bb_version,
                    vkey_hash: artifact.vkey_hash,
                })
            })
            .collect::<Result<Vec<_>>>()?;

        Ok(RegistrySnapshot {
            generated_at_unix: SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .context("system clock is before unix epoch")?
                .as_secs(),
            manifest_version: manifest.version,
            manifest_root: manifest.root,
            circuits,
        })
    }

    pub fn materialize_snapshot(&self, snapshot: &RegistrySnapshot, out_dir: &Path) -> Result<()> {
        let manifest = self.fetch_manifest(&snapshot.manifest_version)?;
        if manifest.root != snapshot.manifest_root {
            anyhow::bail!(
                "manifest root mismatch: snapshot={}, live={}",
                snapshot.manifest_root,
                manifest.root
            );
        }

        fs::create_dir_all(out_dir)
            .with_context(|| format!("failed to create output directory: {}", out_dir.display()))?;
        let circuits_dir = out_dir.join("circuits");
        fs::create_dir_all(&circuits_dir).with_context(|| {
            format!(
                "failed to create circuits output directory: {}",
                circuits_dir.display()
            )
        })?;

        let manifest_path = out_dir.join("manifest.json");
        fs::write(&manifest_path, serde_json::to_vec_pretty(&manifest)?).with_context(|| {
            format!("failed to write manifest file: {}", manifest_path.display())
        })?;

        for fingerprint in &snapshot.circuits {
            let node = manifest
                .circuits
                .get(&fingerprint.name)
                .with_context(|| format!("circuit not found in manifest: {}", fingerprint.name))?;
            if node.hash != fingerprint.manifest_hash {
                anyhow::bail!(
                    "manifest hash mismatch for {}: snapshot={}, live={}",
                    fingerprint.name,
                    fingerprint.manifest_hash,
                    node.hash
                );
            }
            if node.size != fingerprint.manifest_size {
                anyhow::bail!(
                    "manifest size mismatch for {}: snapshot={}, live={}",
                    fingerprint.name,
                    fingerprint.manifest_size,
                    node.size
                );
            }

            let artifact = self.fetch_packaged_circuit(&manifest, &fingerprint.name)?;
            if artifact.vkey_hash != fingerprint.vkey_hash {
                anyhow::bail!(
                    "vkey hash mismatch for {}: snapshot={}, live={}",
                    fingerprint.name,
                    fingerprint.vkey_hash,
                    artifact.vkey_hash
                );
            }
            if artifact.noir_version != fingerprint.noir_version {
                anyhow::bail!(
                    "noir version mismatch for {}: snapshot={:?}, live={:?}",
                    fingerprint.name,
                    fingerprint.noir_version,
                    artifact.noir_version
                );
            }
            if artifact.bb_version != fingerprint.bb_version {
                anyhow::bail!(
                    "bb version mismatch for {}: snapshot={:?}, live={:?}",
                    fingerprint.name,
                    fingerprint.bb_version,
                    artifact.bb_version
                );
            }

            let circuit_path = circuits_dir.join(format!("{}.json", fingerprint.name));
            fs::write(&circuit_path, serde_json::to_vec_pretty(&artifact)?).with_context(|| {
                format!(
                    "failed to write circuit artifact: {}",
                    circuit_path.display()
                )
            })?;
        }

        Ok(())
    }
}

fn decode_json_bytes<T: serde::de::DeserializeOwned>(bytes: &[u8], url: &str) -> Result<T> {
    let json_bytes = if is_gzip(bytes) {
        let mut decoder = GzDecoder::new(bytes);
        let mut out = Vec::new();
        decoder
            .read_to_end(&mut out)
            .with_context(|| format!("failed to gunzip response body: {url}"))?;
        out
    } else {
        bytes.to_vec()
    };

    serde_json::from_slice::<T>(&json_bytes)
        .with_context(|| format!("failed to decode JSON payload: {url}"))
}

fn is_gzip(bytes: &[u8]) -> bool {
    bytes.len() >= 2 && bytes[0] == 0x1f && bytes[1] == 0x8b
}
