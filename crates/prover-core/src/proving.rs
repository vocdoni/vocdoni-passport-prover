use crate::registry::PackagedCircuit;
#[cfg(feature = "native-prover")]
use crate::config::load_compatibility_matrix;
#[cfg(feature = "native-prover")]
use anyhow::Context;
use anyhow::Result;
#[cfg(feature = "native-prover")]
use base64::Engine;
use serde::{Deserialize, Serialize};
#[cfg(feature = "native-prover")]
use std::env;
#[cfg(feature = "native-prover")]
use std::fs;
#[cfg(feature = "native-prover")]
use std::path::Path;
#[cfg(feature = "native-prover")]
use std::path::PathBuf;
#[cfg(feature = "native-prover")]
use std::process::Command;
#[cfg(feature = "native-prover")]
use tempfile::TempDir;

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ProofOracleHash {
    Poseidon2,
    Keccak,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProofOptions {
    pub oracle_hash: ProofOracleHash,
    #[serde(default)]
    pub ipa_accumulation: bool,
    #[serde(default)]
    pub disable_zk: bool,
    #[serde(default)]
    pub low_memory_mode: bool,
    #[serde(default)]
    pub max_storage_usage: Option<u64>,
}

impl ProofOptions {
    pub fn ultra_honk_poseidon2() -> Self {
        Self {
            oracle_hash: ProofOracleHash::Poseidon2,
            ipa_accumulation: false,
            disable_zk: false,
            low_memory_mode: false,
            max_storage_usage: None,
        }
    }

    pub fn ultra_honk_keccak() -> Self {
        Self {
            oracle_hash: ProofOracleHash::Keccak,
            ipa_accumulation: false,
            disable_zk: false,
            low_memory_mode: false,
            max_storage_usage: None,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProofArtifacts {
    pub proof: Vec<u8>,
    pub public_inputs: Vec<u8>,
    pub verification_key: Vec<u8>,
    pub public_inputs_count: u32,
}

#[cfg(feature = "native-prover")]
fn workspace_root() -> PathBuf {
    if let Ok(root) = env::var("VOCDONI_WORKSPACE_ROOT") {
        let root = PathBuf::from(root);
        if root.exists() {
            return root;
        }
    }

    let compiled = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../..");
    if let Ok(root) = compiled.canonicalize() {
        return root;
    }

    if let Ok(exe) = env::current_exe() {
        if let Some(parent) = exe.parent() {
            let candidate = parent.join("..");
            if let Ok(root) = candidate.canonicalize() {
                return root;
            }
        }
    }

    compiled
}

#[cfg(feature = "native-prover")]
fn bb_source_dir() -> PathBuf {
    if let Ok(dir) = env::var("VOCDONI_AZTEC_PACKAGES_CPP_DIR") {
        return PathBuf::from(dir);
    }
    workspace_root()
        .join("../.cache/aztec-packages-src/aztec-packages/barretenberg/cpp")
}

#[cfg(feature = "native-prover")]
fn compatibility_matrix_path() -> PathBuf {
    workspace_root().join("config/compatibility-matrix.json")
}

#[cfg(feature = "native-prover")]
fn bb_release_cache_dir() -> PathBuf {
    workspace_root().join("../.cache/barretenberg-release")
}

#[cfg(feature = "native-prover")]
fn bb_build_dir() -> PathBuf {
    workspace_root().join("../.cache/barretenberg-zkpassport/build")
}

#[cfg(feature = "native-prover")]
fn aztec_source_cache_dir() -> PathBuf {
    workspace_root().join("../.cache/aztec-packages-src")
}

#[cfg(feature = "native-prover")]
fn oracle_hash_arg(oracle_hash: ProofOracleHash) -> &'static str {
    match oracle_hash {
        ProofOracleHash::Poseidon2 => "poseidon2",
        ProofOracleHash::Keccak => "keccak",
    }
}

#[cfg(feature = "native-prover")]
fn run_command(command: &mut Command, description: &str) -> Result<()> {
    let output = command
        .output()
        .with_context(|| format!("failed to execute {description}"))?;
    if output.status.success() {
        return Ok(());
    }

    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);
    anyhow::bail!(
        "{description} failed with status {}.\nstdout:\n{}\nstderr:\n{}",
        output.status,
        stdout.trim(),
        stderr.trim()
    )
}

#[cfg(feature = "native-prover")]
fn release_asset_name() -> Option<&'static str> {
    match (env::consts::OS, env::consts::ARCH) {
        ("linux", "x86_64") => Some("barretenberg-amd64-linux.tar.gz"),
        ("linux", "aarch64") => Some("barretenberg-arm64-linux.tar.gz"),
        ("macos", "x86_64") => Some("barretenberg-amd64-darwin.tar.gz"),
        ("macos", "aarch64") => Some("barretenberg-arm64-darwin.tar.gz"),
        _ => None,
    }
}

#[cfg(feature = "native-prover")]
fn bb_release_url() -> Result<String> {
    if let Ok(url) = env::var("BB_BINARY_DOWNLOAD_URL") {
        if !url.trim().is_empty() {
            return Ok(url);
        }
    }

    let matrix = load_compatibility_matrix(&compatibility_matrix_path())?;
    if let Some(url) = matrix.proving.bb_binary_source {
        return Ok(url);
    }

    let asset = release_asset_name().context("unsupported platform for official bb release binary")?;
    Ok(format!(
        "https://github.com/AztecProtocol/aztec-packages/releases/download/v{version}/{asset}",
        version = matrix.proving.bb_version,
        asset = asset
    ))
}

#[cfg(feature = "native-prover")]
fn ensure_downloaded_bb_binary() -> Result<PathBuf> {
    let matrix = load_compatibility_matrix(&compatibility_matrix_path())?;
    let cache_dir = bb_release_cache_dir().join(format!("v{}", matrix.proving.bb_version));
    let binary = cache_dir.join("bb");
    if binary.exists() {
        return Ok(binary);
    }

    fs::create_dir_all(&cache_dir).with_context(|| {
        format!(
            "failed to create bb release cache directory: {}",
            cache_dir.display()
        )
    })?;

    let temp_dir = TempDir::new().context("failed to create temporary bb release directory")?;
    let archive_path = temp_dir.path().join("bb.tar.gz");
    let url = bb_release_url()?;

    run_command(
        Command::new("curl")
            .arg("-LfsS")
            .arg(&url)
            .arg("-o")
            .arg(&archive_path),
        "download official bb release",
    )?;
    run_command(
        Command::new("tar")
            .arg("-xzf")
            .arg(&archive_path)
            .arg("-C")
            .arg(temp_dir.path()),
        "extract official bb release",
    )?;

    let extracted = temp_dir.path().join("bb");
    if !extracted.exists() {
        anyhow::bail!(
            "official bb release archive did not contain expected `bb` binary: {}",
            url
        );
    }

    fs::copy(&extracted, &binary)
        .with_context(|| format!("failed to cache bb binary at {}", binary.display()))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut permissions = fs::metadata(&binary)
            .with_context(|| format!("failed to stat bb binary: {}", binary.display()))?
            .permissions();
        permissions.set_mode(0o755);
        fs::set_permissions(&binary, permissions).with_context(|| {
            format!("failed to mark bb binary executable: {}", binary.display())
        })?;
    }

    Ok(binary)
}

#[cfg(feature = "native-prover")]
fn ensure_bb_binary() -> Result<PathBuf> {
    if let Ok(path) = env::var("BB_BINARY_PATH") {
        let binary = PathBuf::from(path);
        if binary.exists() {
            return Ok(binary);
        }
        anyhow::bail!("BB_BINARY_PATH does not exist: {}", binary.display());
    }

    if env::var("VOCDONI_ALLOW_BB_SOURCE_BUILD").ok().as_deref() != Some("1") {
        return ensure_downloaded_bb_binary();
    }

    let source_checkout = aztec_source_cache_dir().join("aztec-packages");
    if !source_checkout.exists() {
        fs::create_dir_all(aztec_source_cache_dir()).with_context(|| {
            format!(
                "failed to create aztec source cache directory: {}",
                aztec_source_cache_dir().display()
            )
        })?;
        run_command(
            Command::new("git")
                .arg("clone")
                .arg("https://github.com/zkpassport/aztec-packages")
                .arg(&source_checkout),
            "clone zkpassport aztec-packages",
        )?;
        run_command(
            Command::new("git")
                .current_dir(&source_checkout)
                .arg("checkout")
                .arg("v2.0.3"),
            "checkout zkpassport aztec-packages ref",
        )?;
    }

    let source_dir = bb_source_dir();
    let build_dir = bb_build_dir();
    let binary = build_dir.join("bin/bb");
    if binary.exists() {
        return Ok(binary);
    }

    fs::create_dir_all(&build_dir)
        .with_context(|| format!("failed to create bb build directory: {}", build_dir.display()))?;

    let mut configure = Command::new("cmake");
    configure
        .arg("-S")
        .arg(&source_dir)
        .arg("-B")
        .arg(&build_dir)
        .arg("-G")
        .arg("Ninja")
        .arg("-DCMAKE_BUILD_TYPE=Release")
        .arg("-DCMAKE_C_COMPILER=clang")
        .arg("-DCMAKE_CXX_COMPILER=clang++")
        .arg("-DCMAKE_CXX_FLAGS=-Wno-error=sign-conversion -Wno-error=missing-field-initializers")
        .arg("-DTRACY_ENABLE=OFF");
    run_command(&mut configure, "bb cmake configure")?;
    run_command(
        Command::new("cmake")
            .arg("--build")
            .arg(&build_dir)
            .arg("--target")
            .arg("bb"),
        "bb cmake build",
    )?;
    Ok(binary)
}

#[cfg(feature = "native-prover")]
fn write_circuit_artifact(temp_dir: &TempDir, circuit: &PackagedCircuit) -> Result<PathBuf> {
    let circuit_path = temp_dir.path().join("circuit.json");
    let bytes = serde_json::to_vec_pretty(circuit).context("failed to serialize circuit artifact")?;
    fs::write(&circuit_path, bytes)
        .with_context(|| format!("failed to write circuit artifact: {}", circuit_path.display()))?;
    Ok(circuit_path)
}

#[cfg(feature = "native-prover")]
fn write_witness(temp_dir: &TempDir, witness: &[u8]) -> Result<PathBuf> {
    let witness_path = temp_dir.path().join("witness.gz");
    fs::write(&witness_path, witness)
        .with_context(|| format!("failed to write witness file: {}", witness_path.display()))?;
    Ok(witness_path)
}

#[cfg(feature = "native-prover")]
fn write_packaged_verification_key(temp_dir: &TempDir, circuit: &PackagedCircuit) -> Result<(PathBuf, Vec<u8>)> {
    let vk_path = temp_dir.path().join("vk");
    let vk_bytes = base64::engine::general_purpose::STANDARD
        .decode(circuit.vkey.as_bytes())
        .with_context(|| {
            format!(
                "failed to decode packaged verification key for circuit {:?}",
                circuit.name.as_deref().unwrap_or("<unknown>")
            )
        })?;
    fs::write(&vk_path, &vk_bytes)
        .with_context(|| format!("failed to write packaged vk file: {}", vk_path.display()))?;
    Ok((vk_path, vk_bytes))
}

#[cfg(feature = "native-prover")]
fn read_required_file(path: &Path, description: &str) -> Result<Vec<u8>> {
    fs::read(path).with_context(|| format!("failed to read {description}: {}", path.display()))
}

#[cfg(feature = "native-prover")]
fn public_inputs_count(public_inputs: &[u8]) -> Result<u32> {
    if public_inputs.len() % 32 != 0 {
        anyhow::bail!(
            "public inputs buffer length {} is not a multiple of 32 bytes",
            public_inputs.len()
        );
    }
    Ok((public_inputs.len() / 32) as u32)
}

#[cfg(feature = "native-prover")]
pub fn prove_circuit_with_witness(
    circuit: &PackagedCircuit,
    witness: &[u8],
    options: &ProofOptions,
) -> Result<ProofArtifacts> {
    let bb = ensure_bb_binary()?;
    let temp_dir = TempDir::new().context("failed to create temporary proving directory")?;
    let circuit_path = write_circuit_artifact(&temp_dir, circuit)?;
    let witness_path = write_witness(&temp_dir, witness)?;
    let (vk_path, packaged_verification_key) = write_packaged_verification_key(&temp_dir, circuit)?;
    let output_dir = temp_dir.path().join("proof");
    fs::create_dir_all(&output_dir)
        .with_context(|| format!("failed to create proof output directory: {}", output_dir.display()))?;

    let mut command = Command::new(&bb);
    command
        .arg("prove")
        .arg("--scheme")
        .arg("ultra_honk")
        .arg("-b")
        .arg(&circuit_path)
        .arg("-w")
        .arg(&witness_path)
        .arg("-o")
        .arg(&output_dir)
        .arg("--oracle_hash")
        .arg(oracle_hash_arg(options.oracle_hash))
        .arg("--vk_path")
        .arg(&vk_path);

    if options.ipa_accumulation {
        command.arg("--ipa_accumulation");
    }
    if options.disable_zk {
        command.arg("--disable_zk");
    }
    if options.low_memory_mode {
        command.arg("--slow_low_memory");
    }
    if let Some(max_storage_usage) = options.max_storage_usage {
        command.arg("--storage_budget").arg(max_storage_usage.to_string());
    }

    run_command(&mut command, "bb prove")?;

    let proof = read_required_file(&output_dir.join("proof"), "proof output")?;
    let public_inputs = read_required_file(&output_dir.join("public_inputs"), "public inputs output")?;
    Ok(ProofArtifacts {
        proof,
        public_inputs_count: public_inputs_count(&public_inputs)?,
        public_inputs,
        verification_key: packaged_verification_key,
    })
}

#[cfg(not(feature = "native-prover"))]
pub fn prove_circuit_with_witness(
    _circuit: &PackagedCircuit,
    _witness: &[u8],
    _options: &ProofOptions,
) -> Result<ProofArtifacts> {
    anyhow::bail!("native prover backend is disabled; rebuild with --features native-prover")
}

#[cfg(feature = "native-prover")]
pub fn verify_circuit_proof(
    proof: &[u8],
    public_inputs: &[u8],
    verification_key: &[u8],
    options: &ProofOptions,
) -> Result<bool> {
    let bb = ensure_bb_binary()?;
    let temp_dir = TempDir::new().context("failed to create temporary verification directory")?;
    let proof_path = temp_dir.path().join("proof");
    let public_inputs_path = temp_dir.path().join("public_inputs");
    let vk_path = temp_dir.path().join("vk");
    fs::write(&proof_path, proof)
        .with_context(|| format!("failed to write proof file: {}", proof_path.display()))?;
    fs::write(&public_inputs_path, public_inputs).with_context(|| {
        format!(
            "failed to write public inputs file: {}",
            public_inputs_path.display()
        )
    })?;
    fs::write(&vk_path, verification_key)
        .with_context(|| format!("failed to write vk file: {}", vk_path.display()))?;

    let mut command = Command::new(&bb);
    command
        .arg("verify")
        .arg("--scheme")
        .arg("ultra_honk")
        .arg("-p")
        .arg(&proof_path)
        .arg("-i")
        .arg(&public_inputs_path)
        .arg("-k")
        .arg(&vk_path)
        .arg("--oracle_hash")
        .arg(oracle_hash_arg(options.oracle_hash));

    if options.ipa_accumulation {
        command.arg("--ipa_accumulation");
    }
    if options.disable_zk {
        command.arg("--disable_zk");
    }

    let output = command
        .output()
        .context("failed to execute bb verify command")?;
    Ok(output.status.success())
}

#[cfg(not(feature = "native-prover"))]
pub fn verify_circuit_proof(
    _proof: &[u8],
    _public_inputs: &[u8],
    _verification_key: &[u8],
    _options: &ProofOptions,
) -> Result<bool> {
    anyhow::bail!("native prover backend is disabled; rebuild with --features native-prover")
}

pub fn split_flattened_proof(_proof: &[u8]) -> Result<(Vec<Vec<u8>>, Vec<Vec<u8>>)> {
    anyhow::bail!("proof splitting is not implemented in the v2 prover yet")
}
