use crate::registry::{Manifest, PackagedCircuit, RegistryClient};
use anyhow::{bail, Context, Result};
use std::fs;
use std::path::{Path, PathBuf};

#[derive(Debug, Clone)]
pub struct ArtifactStore {
    local_root: Option<PathBuf>,
    registry: Option<RegistryClient>,
}

impl ArtifactStore {
    pub fn local(root: impl Into<PathBuf>) -> Self {
        Self {
            local_root: Some(root.into()),
            registry: None,
        }
    }

    pub fn local_with_registry_fallback(
        root: impl Into<PathBuf>,
        registry: RegistryClient,
    ) -> Self {
        Self {
            local_root: Some(root.into()),
            registry: Some(registry),
        }
    }

    pub fn registry(registry: RegistryClient) -> Self {
        Self {
            local_root: None,
            registry: Some(registry),
        }
    }

    pub fn load_manifest(&self, version: &str) -> Result<Manifest> {
        if let Some(root) = &self.local_root {
            let path = root.join("manifest.json");
            if path.exists() {
                return load_json_file(&path);
            }
        }

        if let Some(registry) = &self.registry {
            return registry.fetch_manifest(version);
        }

        bail!("manifest not available locally and no registry fallback configured")
    }

    pub fn load_packaged_circuit(
        &self,
        version: &str,
        circuit_name: &str,
    ) -> Result<PackagedCircuit> {
        if let Some(root) = &self.local_root {
            let path = root.join("circuits").join(format!("{circuit_name}.json"));
            if path.exists() {
                return load_json_file(&path);
            }
        }

        if let Some(registry) = &self.registry {
            let manifest = self.load_manifest(version)?;
            return registry.fetch_packaged_circuit(&manifest, circuit_name);
        }

        bail!("circuit {circuit_name} not available locally and no registry fallback configured")
    }
}

fn load_json_file<T: serde::de::DeserializeOwned>(path: &Path) -> Result<T> {
    let raw = fs::read_to_string(path)
        .with_context(|| format!("failed to read JSON file: {}", path.display()))?;
    serde_json::from_str(&raw)
        .with_context(|| format!("failed to parse JSON file: {}", path.display()))
}

#[cfg(test)]
mod tests {
    use super::ArtifactStore;
    use crate::registry::{Manifest, ManifestCircuit, PackagedCircuit};
    use std::collections::BTreeMap;
    use tempfile::tempdir;

    #[test]
    fn loads_manifest_and_circuit_from_local_cache() {
        let dir = tempdir().expect("tempdir");
        std::fs::create_dir_all(dir.path().join("circuits")).expect("circuits dir");

        let manifest = Manifest {
            version: "0.16.0".to_string(),
            root: "0xroot".to_string(),
            circuits: BTreeMap::from([(
                "demo".to_string(),
                ManifestCircuit {
                    hash: "0xhash".to_string(),
                    size: 42,
                },
            )]),
        };
        let circuit = PackagedCircuit {
            name: Some("demo".to_string()),
            noir_version: Some("1.0.0-beta.14".to_string()),
            bb_version: Some("2.0.3".to_string()),
            size: Some(42),
            abi: serde_json::json!([]),
            bytecode: "AA==".to_string(),
            vkey: "AA==".to_string(),
            vkey_hash: "0xhash".to_string(),
            hash: Some("0xhash".to_string()),
        };

        std::fs::write(
            dir.path().join("manifest.json"),
            serde_json::to_vec_pretty(&manifest).expect("manifest json"),
        )
        .expect("write manifest");
        std::fs::write(
            dir.path().join("circuits/demo.json"),
            serde_json::to_vec_pretty(&circuit).expect("circuit json"),
        )
        .expect("write circuit");

        let store = ArtifactStore::local(dir.path());
        let loaded_manifest = store.load_manifest("0.16.0").expect("manifest");
        let loaded_circuit = store
            .load_packaged_circuit("0.16.0", "demo")
            .expect("circuit");

        assert_eq!(loaded_manifest.root, "0xroot");
        assert_eq!(loaded_circuit.name.as_deref(), Some("demo"));
    }
}
