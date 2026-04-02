use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::Path;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CompatibilityMatrix {
    pub status: String,
    pub source_of_truth_policy: String,
    pub registry: RegistryConfig,
    pub proving: ProvingConfig,
    pub mobile: MobileConfig,
    pub server: ServerConfig,
    #[serde(default)]
    pub notes: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegistryConfig {
    pub chain_id: u64,
    pub rpc_url: String,
    pub root_registry: String,
    pub certificate_base_url: String,
    pub manifest_base_url: String,
    pub artifact_base_url: String,
    pub circuit_version: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub manifest_root: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProvingConfig {
    pub canonical_backend: String,
    pub preferred_fork: String,
    pub noir_version: String,
    pub bb_version: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub bb_binary_source: Option<String>,
    pub inner_oracle_hash: String,
    pub outer_evm_oracle_hash: String,
    pub outer_evm_ipa_accumulation: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MobileConfig {
    pub target_mode: String,
    #[serde(default)]
    pub ffi_surface: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerConfig {
    pub target_mode: String,
    pub integration_mode: String,
}

pub fn load_compatibility_matrix(path: &Path) -> Result<CompatibilityMatrix> {
    let raw = fs::read_to_string(path)
        .with_context(|| format!("failed to read compatibility matrix: {}", path.display()))?;
    serde_json::from_str(&raw)
        .with_context(|| format!("failed to parse compatibility matrix: {}", path.display()))
}
