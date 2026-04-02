use anyhow::{Context, Result};
use base64::Engine;
use clap::{Parser, Subcommand};
use prover_core::{
    analyze_document, load_compatibility_matrix, load_fixture_bundle, load_fixture_dir,
    prove_circuit_with_witness, resolve_document_circuits, solve_compressed_witness,
    solve_compressed_witness_for_circuit, solve_serialized_legacy_witness_for_circuit,
    solve_serialized_witness_for_circuit,
    verify_circuit_proof, ArtifactStore, CertificateRegistryClient, DocumentCircuitContext,
    Manifest, PackagedCertificatesFile, PackagedCircuit, ProofArtifacts, ProofOptions,
    ProverPipeline, RegistryClient, WitnessPayload,
};
use prover_types::{InnerProofBundle, OuterProofResult};
use serde::{Deserialize, Serialize};
use std::fs;
use std::io::Write;
use std::path::PathBuf;
use std::process::Command as ProcessCommand;
use tempfile::NamedTempFile;

#[derive(Debug, Clone, Copy, clap::ValueEnum)]
enum WitnessFormatArg {
    Legacy,
    Compact,
}

#[derive(Debug, Parser)]
#[command(name = "prover-cli")]
#[command(about = "Vocdoni Passport prover workspace CLI")]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Debug, Subcommand)]
enum Command {
    ValidateFixture {
        #[arg(long)]
        file: Option<PathBuf>,
        #[arg(long)]
        dir: Option<PathBuf>,
    },
    ShowMatrix,
    FetchManifest {
        #[arg(long)]
        version: Option<String>,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
        #[arg(long)]
        artifacts_dir: Option<PathBuf>,
    },
    InspectCircuit {
        #[arg(long)]
        name: String,
        #[arg(long)]
        version: Option<String>,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
        #[arg(long)]
        artifacts_dir: Option<PathBuf>,
    },
    PlanRequest {
        #[arg(long)]
        file: Option<PathBuf>,
        #[arg(long)]
        dir: Option<PathBuf>,
    },
    SnapshotRegistry {
        #[arg(long)]
        version: Option<String>,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
        #[arg(long)]
        file: Option<PathBuf>,
        #[arg(long)]
        dir: Option<PathBuf>,
        #[arg(long)]
        circuit: Vec<String>,
        #[arg(long)]
        output: Option<PathBuf>,
    },
    MaterializeSnapshot {
        #[arg(long)]
        snapshot: PathBuf,
        #[arg(long)]
        out_dir: PathBuf,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
    },
    ResolveCircuits {
        #[arg(long)]
        context: PathBuf,
        #[arg(long)]
        manifest: Option<PathBuf>,
        #[arg(long)]
        version: Option<String>,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
    },
    PrintExampleFixture,
    AnalyzeDocument {
        #[arg(long)]
        file: Option<PathBuf>,
        #[arg(long)]
        dir: Option<PathBuf>,
    },
    FetchCertificates {
        #[arg(long)]
        root: Option<String>,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
    },
    ResolveDocumentCircuits {
        #[arg(long)]
        file: Option<PathBuf>,
        #[arg(long)]
        dir: Option<PathBuf>,
        #[arg(long)]
        cert_root: Option<String>,
        #[arg(long)]
        certificates: Option<PathBuf>,
        #[arg(long)]
        manifest: Option<PathBuf>,
        #[arg(long)]
        version: Option<String>,
        #[arg(long)]
        artifacts_dir: Option<PathBuf>,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
    },
    ReferenceInputs {
        #[arg(long)]
        dir: PathBuf,
        #[arg(long)]
        cert_root: Option<String>,
        #[arg(long)]
        certificates: Option<PathBuf>,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
        #[arg(long)]
        output: Option<PathBuf>,
    },
    SolveFixtureWitnesses {
        #[arg(long)]
        dir: PathBuf,
        #[arg(long)]
        cert_root: Option<String>,
        #[arg(long)]
        certificates: Option<PathBuf>,
        #[arg(long)]
        artifacts_dir: Option<PathBuf>,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
        #[arg(long)]
        output_dir: Option<PathBuf>,
    },
    ProveFixtureInner {
        #[arg(long)]
        dir: PathBuf,
        #[arg(long)]
        cert_root: Option<String>,
        #[arg(long)]
        certificates: Option<PathBuf>,
        #[arg(long)]
        artifacts_dir: Option<PathBuf>,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
        #[arg(long)]
        output_dir: Option<PathBuf>,
        #[arg(long, default_value_t = false)]
        low_memory_mode: bool,
        #[arg(long)]
        max_storage_usage: Option<u64>,
    },
    ProveFixtureOuter {
        #[arg(long)]
        dir: PathBuf,
        #[arg(long)]
        artifacts_dir: Option<PathBuf>,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
        #[arg(long)]
        proofs_dir: Option<PathBuf>,
        #[arg(long)]
        output_dir: Option<PathBuf>,
        #[arg(long, value_enum, default_value_t = WitnessFormatArg::Legacy)]
        witness_format: WitnessFormatArg,
        #[arg(long, default_value_t = false)]
        low_memory_mode: bool,
        #[arg(long)]
        max_storage_usage: Option<u64>,
    },
    AggregateRequest {
        #[arg(long)]
        input: PathBuf,
        #[arg(long)]
        artifacts_dir: Option<PathBuf>,
        #[arg(long, default_value = "config/compatibility-matrix.json")]
        matrix: PathBuf,
        #[arg(long)]
        output: Option<PathBuf>,
        #[arg(long, value_enum, default_value_t = WitnessFormatArg::Legacy)]
        witness_format: WitnessFormatArg,
        #[arg(long, default_value_t = false)]
        low_memory_mode: bool,
        #[arg(long)]
        max_storage_usage: Option<u64>,
    },
    SolveWitness {
        #[arg(long)]
        payload: Option<PathBuf>,
        #[arg(long)]
        artifact: Option<PathBuf>,
        #[arg(long)]
        inputs: Option<PathBuf>,
    },
    ProveAll {
        #[arg(long)]
        file: Option<PathBuf>,
        #[arg(long)]
        dir: Option<PathBuf>,
    },
}

#[derive(Debug, Deserialize, Serialize)]
struct ReferenceDisclosureInputs {
    circuit_name: String,
    inputs: serde_json::Value,
}

#[derive(Debug, Deserialize, Serialize)]
struct ReferenceInputsFile {
    dsc_inputs: serde_json::Value,
    id_data_inputs: serde_json::Value,
    integrity_inputs: serde_json::Value,
    disclosures: Vec<ReferenceDisclosureInputs>,
}

#[derive(Debug, Deserialize, Serialize)]
struct OuterReferenceFile {
    version: String,
    current_date: u64,
    outer_circuit_name: String,
    inner_bundle: InnerProofBundle,
    outer_inputs: serde_json::Value,
}

#[derive(Debug, Clone)]
struct CircuitSolveJob {
    circuit_name: String,
    inputs: serde_json::Value,
}

fn solve_serialized_witness_for_circuit_with_format(
    circuit: &PackagedCircuit,
    inputs: serde_json::Value,
    witness_format: WitnessFormatArg,
) -> Result<Vec<u8>> {
    match witness_format {
        WitnessFormatArg::Legacy => solve_serialized_legacy_witness_for_circuit(circuit, inputs),
        WitnessFormatArg::Compact => solve_serialized_witness_for_circuit(circuit, inputs),
    }
}

fn packaged_verification_key(circuit: &PackagedCircuit) -> Result<Vec<u8>> {
    base64::engine::general_purpose::STANDARD
        .decode(&circuit.vkey)
        .with_context(|| format!("failed to decode packaged vkey for {}", circuit.name.as_deref().unwrap_or("<unknown>")))
}

fn verification_keys_match(packaged: &[u8], generated: &[u8]) -> bool {
    packaged == generated
}

fn main() -> Result<()> {
    let cli = Cli::parse();
    let pipeline = ProverPipeline::new();

    match cli.command {
        Command::ValidateFixture { file, dir } => {
            let fixture = load_input(file, dir)?;
            pipeline
                .validate_fixture(&fixture)
                .context("fixture validation failed in pipeline")?;
            println!("{}", serde_json::to_string_pretty(&fixture)?);
        }
        Command::ShowMatrix => {
            let matrix =
                load_compatibility_matrix(&PathBuf::from("config/compatibility-matrix.json"))?;
            println!(
                "{}",
                serde_json::to_string_pretty(&pipeline.compatibility_matrix_summary(&matrix))?
            );
        }
        Command::FetchManifest {
            version,
            matrix,
            artifacts_dir,
        } => {
            let matrix = load_compatibility_matrix(&matrix)?;
            let version = version.unwrap_or_else(|| matrix.registry.circuit_version.clone());
            let store = artifact_store_from_options(&matrix, artifacts_dir);
            let manifest = store.load_manifest(&version)?;
            println!("{}", serde_json::to_string_pretty(&manifest)?);
        }
        Command::InspectCircuit {
            name,
            version,
            matrix,
            artifacts_dir,
        } => {
            let matrix = load_compatibility_matrix(&matrix)?;
            let version = version.unwrap_or_else(|| matrix.registry.circuit_version.clone());
            let store = artifact_store_from_options(&matrix, artifacts_dir);
            let manifest = store.load_manifest(&version)?;
            let circuit = store.load_packaged_circuit(&version, &name)?;
            let out = serde_json::json!({
                "manifest_version": manifest.version,
                "manifest_root": manifest.root,
                "circuit_name": name,
                "artifact": circuit,
            });
            println!("{}", serde_json::to_string_pretty(&out)?);
        }
        Command::PlanRequest { file, dir } => {
            let fixture = load_input(file, dir)?;
            let plan = pipeline.plan_fixture(&fixture)?;
            println!("{}", serde_json::to_string_pretty(&plan)?);
        }
        Command::SnapshotRegistry {
            version,
            matrix,
            file,
            dir,
            circuit,
            output,
        } => {
            let matrix = load_compatibility_matrix(&matrix)?;
            let version = version.unwrap_or_else(|| matrix.registry.circuit_version.clone());
            let client = RegistryClient::from_matrix(&matrix);

            let mut circuit_names = circuit;
            if file.is_some() || dir.is_some() {
                let fixture = load_input(file, dir)?;
                let plan = pipeline.plan_fixture(&fixture)?;
                circuit_names.extend(plan.required_circuit_names());
            }
            if circuit_names.is_empty() {
                anyhow::bail!(
                    "provide at least one --circuit or a fixture via --file/--dir to derive query circuits"
                );
            }

            let snapshot = client.snapshot_circuits(&version, &circuit_names)?;
            let rendered = serde_json::to_string_pretty(&snapshot)?;
            if let Some(output) = output {
                fs::write(&output, &rendered).with_context(|| {
                    format!("failed to write snapshot file: {}", output.display())
                })?;
            }
            println!("{rendered}");
        }
        Command::MaterializeSnapshot {
            snapshot,
            out_dir,
            matrix,
        } => {
            let matrix = load_compatibility_matrix(&matrix)?;
            let client = RegistryClient::from_matrix(&matrix);
            let raw = fs::read_to_string(&snapshot)
                .with_context(|| format!("failed to read snapshot file: {}", snapshot.display()))?;
            let snapshot: prover_core::RegistrySnapshot =
                serde_json::from_str(&raw).with_context(|| {
                    format!("failed to parse snapshot file: {}", snapshot.display())
                })?;
            client.materialize_snapshot(&snapshot, &out_dir)?;
            println!(
                "{}",
                serde_json::to_string_pretty(&serde_json::json!({
                    "snapshot": snapshot.manifest_version,
                    "manifest_root": snapshot.manifest_root,
                    "out_dir": out_dir,
                    "circuits": snapshot.circuits.iter().map(|c| c.name.clone()).collect::<Vec<_>>(),
                }))?
            );
        }
        Command::ResolveCircuits {
            context,
            manifest,
            version,
            matrix,
        } => {
            let raw = fs::read_to_string(&context)
                .with_context(|| format!("failed to read context file: {}", context.display()))?;
            let context: DocumentCircuitContext = serde_json::from_str(&raw)
                .with_context(|| format!("failed to parse context file: {}", context.display()))?;

            let manifest = if let Some(manifest) = manifest {
                load_manifest_file(&manifest)?
            } else {
                let matrix = load_compatibility_matrix(&matrix)?;
                let version = version.unwrap_or_else(|| matrix.registry.circuit_version.clone());
                RegistryClient::from_matrix(&matrix).fetch_manifest(&version)?
            };

            let resolved = resolve_document_circuits(&manifest, &context)?;
            println!("{}", serde_json::to_string_pretty(&resolved)?);
        }
        Command::PrintExampleFixture => {
            let example = prover_types::FixtureBundle {
                document: prover_types::DocumentInput {
                    dg1_base64: "<dg1-base64>".to_string(),
                    sod_der_base64: "<sod-der-base64>".to_string(),
                    dg2_base64: None,
                    document_type: Some("passport".to_string()),
                },
                request: prover_types::ProofRequest {
                    circuit_version: "0.16.0".to_string(),
                    chain_id: 11155111,
                    service_scope: "vocdoni-passport".to_string(),
                    service_subscope: "petition".to_string(),
                    current_date: 1_775_041_600,
                    query: serde_json::json!({
                        "nationality": { "disclose": true }
                    }),
                    petition_id: Some("demo-1".to_string()),
                    aggregate_mode: Some("server".to_string()),
                },
                expected_inner: None,
                expected_outer: None,
                metadata: prover_types::FixtureMetadata {
                    fixture_id: Some("example".to_string()),
                    source: Some("manual".to_string()),
                    notes: Some("replace placeholders with real captured data".to_string()),
                },
            };
            println!("{}", serde_json::to_string_pretty(&example)?);
        }
        Command::AnalyzeDocument { file, dir } => {
            let fixture = load_input(file, dir)?;
            let analysis = analyze_document(&fixture.document)?;
            println!("{}", serde_json::to_string_pretty(&analysis)?);
        }
        Command::FetchCertificates { root, matrix } => {
            let matrix = load_compatibility_matrix(&matrix)?;
            let client = CertificateRegistryClient::from_matrix(&matrix);
            let packaged = client.fetch_certificates(root.as_deref())?;
            println!("{}", serde_json::to_string_pretty(&packaged)?);
        }
        Command::ResolveDocumentCircuits {
            file,
            dir,
            cert_root,
            certificates,
            manifest,
            version,
            artifacts_dir,
            matrix,
        } => {
            let fixture = load_input(file, dir)?;
            let matrix = load_compatibility_matrix(&matrix)?;
            let pipeline = ProverPipeline::new();
            let proof_plan = pipeline.plan_fixture(&fixture)?;
            let analysis = pipeline.analyze_document(&fixture)?;

            let cert_client = CertificateRegistryClient::from_matrix(&matrix);
            let packaged = if let Some(certificates) = certificates {
                load_certificates_file(&certificates)?
            } else {
                cert_client.fetch_certificates(cert_root.as_deref())?
            };
            let csca = cert_client
                .resolve_csca(&analysis, &packaged)
                .context("failed to match CSCA certificate from packaged registry")?;

            let manifest = if let Some(manifest) = manifest {
                load_manifest_file(&manifest)?
            } else {
                let version = version.unwrap_or_else(|| matrix.registry.circuit_version.clone());
                artifact_store_from_options(&matrix, artifacts_dir).load_manifest(&version)?
            };
            let resolved_plan = pipeline.resolve_proof_plan(&fixture, &manifest, &csca)?;
            let out = serde_json::json!({
                "proof_plan": proof_plan,
                "analysis": resolved_plan.analysis,
                "csca": resolved_plan.csca,
                "context": resolved_plan.document_context,
                "resolved": resolved_plan.circuits,
            });
            println!("{}", serde_json::to_string_pretty(&out)?);
        }
        Command::ReferenceInputs {
            dir,
            cert_root,
            certificates,
            matrix,
            output,
        } => {
            let matrix = load_compatibility_matrix(&matrix)?;
            let (_packaged, _temp, certificates_path) =
                materialize_certificates_path(&matrix, certificates, cert_root)?;
            let reference = run_reference_inputs_script(&dir, &certificates_path, output.as_ref())?;
            println!("{}", serde_json::to_string_pretty(&reference)?);
        }
        Command::SolveFixtureWitnesses {
            dir,
            cert_root,
            certificates,
            artifacts_dir,
            matrix,
            output_dir,
        } => {
            let matrix = load_compatibility_matrix(&matrix)?;
            let fixture = load_fixture_dir(&dir)?;
            let version = fixture.request.circuit_version.clone();
            let (packaged, _temp, certificates_path) =
                materialize_certificates_path(&matrix, certificates, cert_root)?;
            let reference = run_reference_inputs_script(&dir, &certificates_path, None)?;
            let pipeline = ProverPipeline::new();
            let store = artifact_store_from_options(&matrix, artifacts_dir);
            let manifest = store.load_manifest(&version)?;
            let cert_client = CertificateRegistryClient::from_matrix(&matrix);
            let analysis = pipeline.analyze_document(&fixture)?;
            let csca = cert_client
                .resolve_csca(&analysis, &packaged)
                .context("failed to match CSCA certificate from packaged registry")?;
            let resolved = pipeline.resolve_proof_plan(&fixture, &manifest, &csca)?;
            let jobs = collect_solve_jobs(&reference, &resolved);
            let output_dir = output_dir.unwrap_or_else(|| {
                workspace_root()
                    .join("outputs")
                    .join(fixture_id_for_dir(&dir))
                    .join("witnesses")
            });
            let summary = solve_fixture_jobs(
                &store,
                &version,
                &jobs,
                Some(&output_dir),
                false,
                ProofOptions::ultra_honk_poseidon2(),
            )?;
            println!("{}", serde_json::to_string_pretty(&summary)?);
        }
        Command::ProveFixtureInner {
            dir,
            cert_root,
            certificates,
            artifacts_dir,
            matrix,
            output_dir,
            low_memory_mode,
            max_storage_usage,
        } => {
            let matrix = load_compatibility_matrix(&matrix)?;
            let fixture = load_fixture_dir(&dir)?;
            let version = fixture.request.circuit_version.clone();
            let (packaged, _temp, certificates_path) =
                materialize_certificates_path(&matrix, certificates, cert_root)?;
            let reference = run_reference_inputs_script(&dir, &certificates_path, None)?;
            let pipeline = ProverPipeline::new();
            let store = artifact_store_from_options(&matrix, artifacts_dir);
            let manifest = store.load_manifest(&version)?;
            let cert_client = CertificateRegistryClient::from_matrix(&matrix);
            let analysis = pipeline.analyze_document(&fixture)?;
            let csca = cert_client
                .resolve_csca(&analysis, &packaged)
                .context("failed to match CSCA certificate from packaged registry")?;
            let resolved = pipeline.resolve_proof_plan(&fixture, &manifest, &csca)?;
            let jobs = collect_solve_jobs(&reference, &resolved);
            let output_dir = output_dir.unwrap_or_else(|| {
                workspace_root()
                    .join("outputs")
                    .join(fixture_id_for_dir(&dir))
                    .join("proofs")
            });
            let mut options = ProofOptions::ultra_honk_poseidon2();
            options.low_memory_mode = low_memory_mode;
            options.max_storage_usage = max_storage_usage;
            let summary =
                solve_fixture_jobs(&store, &version, &jobs, Some(&output_dir), true, options)?;
            println!("{}", serde_json::to_string_pretty(&summary)?);
        }
        Command::ProveFixtureOuter {
            dir,
            artifacts_dir,
            matrix,
            proofs_dir,
            output_dir,
            witness_format,
            low_memory_mode,
            max_storage_usage,
        } => {
            let matrix = load_compatibility_matrix(&matrix)?;
            let fixture = load_fixture_dir(&dir)?;
            let version = fixture.request.circuit_version.clone();
            let store = artifact_store_from_options(&matrix, artifacts_dir.clone());
            let outer_artifacts_dir =
                local_artifacts_dir_for_version(&version, artifacts_dir.as_ref());
            let proofs_dir = proofs_dir.unwrap_or_else(|| {
                workspace_root()
                    .join("outputs")
                    .join(fixture_id_for_dir(&dir))
                    .join("proofs")
            });
            let output_dir = output_dir.unwrap_or_else(|| {
                workspace_root()
                    .join("outputs")
                    .join(fixture_id_for_dir(&dir))
                    .join("outer")
            });
            fs::create_dir_all(&output_dir).with_context(|| {
                format!(
                    "failed to create outer output directory: {}",
                    output_dir.display()
                )
            })?;

            let outer_reference = run_outer_inputs_script(&dir, &proofs_dir, &outer_artifacts_dir)?;
            fs::write(
                output_dir.join("inner_bundle.json"),
                serde_json::to_vec_pretty(&outer_reference.inner_bundle)?,
            )
            .with_context(|| {
                format!(
                    "failed to write inner bundle file: {}",
                    output_dir.join("inner_bundle.json").display()
                )
            })?;
            fs::write(
                output_dir.join("outer_inputs.json"),
                serde_json::to_vec_pretty(&outer_reference.outer_inputs)?,
            )
            .with_context(|| {
                format!(
                    "failed to write outer inputs file: {}",
                    output_dir.join("outer_inputs.json").display()
                )
            })?;

            let outer_circuit =
                store.load_packaged_circuit(&version, &outer_reference.outer_circuit_name)?;
            let witness = solve_serialized_witness_for_circuit_with_format(
                &outer_circuit,
                outer_reference.outer_inputs.clone(),
                witness_format,
            )
            .with_context(|| {
                format!(
                    "failed to solve outer witness for {}",
                    outer_reference.outer_circuit_name
                )
            })?;
            let witness_path = output_dir.join(format!("{}.witness.bin", outer_reference.outer_circuit_name));
            fs::write(&witness_path, &witness).with_context(|| {
                format!("failed to write outer witness file: {}", witness_path.display())
            })?;

            let mut proof_options = ProofOptions::ultra_honk_keccak();
            proof_options.ipa_accumulation = false;
            proof_options.disable_zk = true;
            proof_options.low_memory_mode = low_memory_mode;
            proof_options.max_storage_usage = max_storage_usage;

            eprintln!(
                "outer: proving {} with zkPassport bb cli v2.0.3 (keccak, disable_zk, no ipa, witness={})",
                outer_reference.outer_circuit_name,
                match witness_format {
                    WitnessFormatArg::Legacy => "legacy",
                    WitnessFormatArg::Compact => "compact",
                },
            );
            let proof = prove_circuit_with_witness(&outer_circuit, &witness, &proof_options)
                .with_context(|| {
                format!(
                    "failed to generate outer proof for {}",
                    outer_reference.outer_circuit_name
                )
                })?;
            eprintln!(
                "outer: proof generated ({} proof bytes, {} public inputs bytes)",
                proof.proof.len(),
                proof.public_inputs.len()
            );
            eprintln!("outer: verifying {}", outer_reference.outer_circuit_name);
            let packaged_vk = packaged_verification_key(&outer_circuit)?;
            let generated_vk_matches_packaged =
                verification_keys_match(&packaged_vk, &proof.verification_key);
            let verified = verify_inner_proof(&proof, &packaged_vk, &proof_options).with_context(|| {
                format!(
                    "failed to verify outer proof for {}",
                    outer_reference.outer_circuit_name
                )
            })?;
            persist_proof_artifacts(
                &output_dir,
                &outer_reference.outer_circuit_name,
                &proof,
                &packaged_vk,
                generated_vk_matches_packaged,
            )?;

            let public_inputs = split_hex_fields(&proof.public_inputs)?;
            let mut metadata = std::collections::BTreeMap::new();
            metadata.insert("proof_verified".to_string(), verified.to_string());
            metadata.insert(
                "prover_backend".to_string(),
                "zkpassport-bb-cli-v2.0.3".to_string(),
            );
            metadata.insert("oracle_hash".to_string(), "keccak".to_string());
            metadata.insert(
                "generated_vk_matches_packaged".to_string(),
                generated_vk_matches_packaged.to_string(),
            );
            metadata.insert("disable_zk".to_string(), proof_options.disable_zk.to_string());
            metadata.insert(
                "witness_format".to_string(),
                match witness_format {
                    WitnessFormatArg::Legacy => "legacy".to_string(),
                    WitnessFormatArg::Compact => "compact".to_string(),
                },
            );
            metadata.insert(
                "ipa_accumulation".to_string(),
                proof_options.ipa_accumulation.to_string(),
            );
            let outer_result = OuterProofResult {
                version: version.clone(),
                name: outer_reference.outer_circuit_name.clone(),
                proof: bytes_to_hex(&proof.proof),
                public_inputs: public_inputs.clone(),
                vkey_hash: outer_circuit.vkey_hash.clone(),
                nullifier: public_inputs.last().cloned(),
                metadata,
            };
            fs::write(
                output_dir.join("outer_result.json"),
                serde_json::to_vec_pretty(&outer_result)?,
            )
            .with_context(|| {
                format!(
                    "failed to write outer result file: {}",
                    output_dir.join("outer_result.json").display()
                )
            })?;

            println!(
                "{}",
                serde_json::to_string_pretty(&serde_json::json!({
                    "outer_circuit_name": outer_reference.outer_circuit_name,
                    "proof_verified": verified,
                    "proof_bytes": proof.proof.len(),
                    "public_inputs_count": proof.public_inputs_count,
                    "summary_path": output_dir.join("outer_result.json").display().to_string(),
                }))?
            );
        }
        Command::AggregateRequest {
            input,
            artifacts_dir,
            matrix,
            output,
            witness_format,
            low_memory_mode,
            max_storage_usage,
        } => {
            let matrix = load_compatibility_matrix(&matrix)?;
            let artifacts_dir = local_artifacts_dir_for_version(
                &matrix.registry.circuit_version,
                artifacts_dir.as_ref(),
            );
            let store = artifact_store_from_options(&matrix, Some(artifacts_dir.clone()));
            let outer_reference = run_aggregate_inputs_script(&input, &artifacts_dir)?;

            let outer_circuit = store.load_packaged_circuit(
                &outer_reference.version,
                &outer_reference.outer_circuit_name,
            )?;
            let witness = solve_serialized_witness_for_circuit_with_format(
                &outer_circuit,
                outer_reference.outer_inputs.clone(),
                witness_format,
            )
            .with_context(|| {
                format!(
                    "failed to solve outer witness for {}",
                    outer_reference.outer_circuit_name
                )
            })?;

            let mut proof_options = ProofOptions::ultra_honk_keccak();
            proof_options.ipa_accumulation = false;
            proof_options.disable_zk = true;
            proof_options.low_memory_mode = low_memory_mode;
            proof_options.max_storage_usage = max_storage_usage;

            let proof = prove_circuit_with_witness(&outer_circuit, &witness, &proof_options)
                .with_context(|| {
                    format!(
                        "failed to generate outer proof for {}",
                        outer_reference.outer_circuit_name
                    )
                })?;
            let packaged_vk = packaged_verification_key(&outer_circuit)?;
            let generated_vk_matches_packaged =
                verification_keys_match(&packaged_vk, &proof.verification_key);
            let verified =
                verify_inner_proof(&proof, &packaged_vk, &proof_options).with_context(|| {
                    format!(
                        "failed to verify outer proof for {}",
                        outer_reference.outer_circuit_name
                    )
                })?;
            let public_inputs = split_hex_fields(&proof.public_inputs)?;

            let mut metadata = std::collections::BTreeMap::new();
            metadata.insert("proof_verified".to_string(), verified.to_string());
            metadata.insert(
                "prover_backend".to_string(),
                "zkpassport-bb-cli-v2.0.3".to_string(),
            );
            metadata.insert("oracle_hash".to_string(), "keccak".to_string());
            metadata.insert(
                "generated_vk_matches_packaged".to_string(),
                generated_vk_matches_packaged.to_string(),
            );
            metadata.insert("disable_zk".to_string(), proof_options.disable_zk.to_string());
            metadata.insert(
                "witness_format".to_string(),
                match witness_format {
                    WitnessFormatArg::Legacy => "legacy".to_string(),
                    WitnessFormatArg::Compact => "compact".to_string(),
                },
            );
            metadata.insert(
                "ipa_accumulation".to_string(),
                proof_options.ipa_accumulation.to_string(),
            );

            let outer_result = OuterProofResult {
                version: outer_reference.version.clone(),
                name: outer_reference.outer_circuit_name.clone(),
                proof: bytes_to_hex(&proof.proof),
                public_inputs: public_inputs.clone(),
                vkey_hash: outer_circuit.vkey_hash.clone(),
                nullifier: public_inputs.last().cloned(),
                metadata,
            };

            if let Some(output) = output {
                if let Some(parent) = output.parent() {
                    fs::create_dir_all(parent).with_context(|| {
                        format!("failed to create output directory: {}", parent.display())
                    })?;
                }
                fs::write(&output, serde_json::to_vec_pretty(&outer_result)?).with_context(
                    || format!("failed to write aggregate output file: {}", output.display()),
                )?;
            }

            println!("{}", serde_json::to_string_pretty(&outer_result)?);
        }
        Command::SolveWitness {
            payload,
            artifact,
            inputs,
        } => match (payload, artifact, inputs) {
            (Some(payload), None, None) => {
                let raw = fs::read_to_string(&payload).with_context(|| {
                    format!("failed to read witness payload: {}", payload.display())
                })?;
                let payload: WitnessPayload = serde_json::from_str(&raw).with_context(|| {
                    format!("failed to parse witness payload: {}", payload.display())
                })?;
                let witness = solve_compressed_witness(&payload)?;
                println!(
                    "{}",
                    serde_json::to_string_pretty(&serde_json::json!({
                        "witness_bytes": witness.len(),
                    }))?
                );
            }
            (None, Some(artifact), Some(inputs)) => {
                let circuit = load_packaged_circuit_file(&artifact)?;
                let inputs = load_json_value_file(&inputs)?;
                let witness = solve_compressed_witness_for_circuit(&circuit, inputs)?;
                println!(
                    "{}",
                    serde_json::to_string_pretty(&serde_json::json!({
                        "witness_bytes": witness.len(),
                    }))?
                );
            }
            _ => anyhow::bail!("provide either --payload or both --artifact and --inputs"),
        },
        Command::ProveAll { file, dir } => {
            let fixture = load_input(file, dir)?;
            let inner = pipeline.prove_inner(&fixture);
            let outer = pipeline.prove_outer(&fixture);
            let out = serde_json::json!({
                "inner": inner.as_ref().map_err(ToString::to_string).err().unwrap_or_else(|| "ok".to_string()),
                "outer": outer.as_ref().map_err(ToString::to_string).err().unwrap_or_else(|| "ok".to_string()),
            });
            println!("{}", serde_json::to_string_pretty(&out)?);
        }
    }

    Ok(())
}

fn load_input(file: Option<PathBuf>, dir: Option<PathBuf>) -> Result<prover_types::FixtureBundle> {
    match (file, dir) {
        (Some(file), None) => load_fixture_bundle(&file),
        (None, Some(dir)) => load_fixture_dir(&dir),
        _ => anyhow::bail!("provide exactly one of --file or --dir"),
    }
}

fn load_manifest_file(path: &PathBuf) -> Result<Manifest> {
    let raw = fs::read_to_string(path)
        .with_context(|| format!("failed to read manifest file: {}", path.display()))?;
    serde_json::from_str(&raw)
        .with_context(|| format!("failed to parse manifest file: {}", path.display()))
}

fn load_packaged_circuit_file(path: &PathBuf) -> Result<PackagedCircuit> {
    let raw = fs::read_to_string(path)
        .with_context(|| format!("failed to read packaged circuit file: {}", path.display()))?;
    serde_json::from_str(&raw)
        .with_context(|| format!("failed to parse packaged circuit file: {}", path.display()))
}

fn load_certificates_file(path: &PathBuf) -> Result<prover_core::PackagedCertificatesFile> {
    let raw = fs::read_to_string(path)
        .with_context(|| format!("failed to read certificates file: {}", path.display()))?;
    serde_json::from_str(&raw)
        .with_context(|| format!("failed to parse certificates file: {}", path.display()))
}

fn load_json_value_file(path: &PathBuf) -> Result<serde_json::Value> {
    let raw = fs::read_to_string(path)
        .with_context(|| format!("failed to read JSON value file: {}", path.display()))?;
    serde_json::from_str(&raw)
        .with_context(|| format!("failed to parse JSON value file: {}", path.display()))
}

fn artifact_store_from_options(
    matrix: &prover_core::CompatibilityMatrix,
    artifacts_dir: Option<PathBuf>,
) -> ArtifactStore {
    let registry = RegistryClient::from_matrix(matrix);
    match artifacts_dir {
        Some(dir) => ArtifactStore::local_with_registry_fallback(dir, registry),
        None => ArtifactStore::registry(registry),
    }
}

fn workspace_root() -> PathBuf {
    if let Ok(root) = std::env::var("VOCDONI_WORKSPACE_ROOT") {
        let root = PathBuf::from(root);
        if root.exists() {
            return root;
        }
    }

    let compiled = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../..");
    if let Ok(root) = compiled.canonicalize() {
        return root;
    }

    if let Ok(exe) = std::env::current_exe() {
        if let Some(parent) = exe.parent() {
            let candidate = parent.join("..");
            if let Ok(root) = candidate.canonicalize() {
                return root;
            }
        }
    }

    compiled
}

fn fixture_id_for_dir(dir: &PathBuf) -> String {
    dir.file_name()
        .map(|value| value.to_string_lossy().into_owned())
        .unwrap_or_else(|| "fixture".to_string())
}

fn materialize_certificates_path(
    matrix: &prover_core::CompatibilityMatrix,
    certificates: Option<PathBuf>,
    cert_root: Option<String>,
) -> Result<(PackagedCertificatesFile, Option<NamedTempFile>, PathBuf)> {
    match certificates {
        Some(path) => {
            let packaged = load_certificates_file(&path)?;
            Ok((packaged, None, path))
        }
        None => {
            let client = CertificateRegistryClient::from_matrix(matrix);
            let packaged = client.fetch_certificates(cert_root.as_deref())?;
            let mut file =
                NamedTempFile::new().context("failed to create temporary certificates file")?;
            serde_json::to_writer_pretty(file.as_file_mut(), &packaged)
                .context("failed to write temporary certificates file")?;
            file.as_file_mut()
                .flush()
                .context("failed to flush temporary certificates file")?;
            let path = file.path().to_path_buf();
            Ok((packaged, Some(file), path))
        }
    }
}

fn run_reference_inputs_script(
    fixture_dir: &PathBuf,
    certificates_path: &PathBuf,
    output: Option<&PathBuf>,
) -> Result<ReferenceInputsFile> {
    let script_path = workspace_root().join("scripts/reference-inputs.cjs");
    let mut temp_output = None;
    let output_path = match output {
        Some(path) => path.clone(),
        None => {
            let file =
                NamedTempFile::new().context("failed to create temporary reference output file")?;
            let path = file.path().to_path_buf();
            temp_output = Some(file);
            path
        }
    };

    let result = ProcessCommand::new("node")
        .arg(&script_path)
        .arg("--fixture-dir")
        .arg(fixture_dir)
        .arg("--certificates")
        .arg(certificates_path)
        .arg("--out")
        .arg(&output_path)
        .output()
        .with_context(|| format!("failed to execute node script: {}", script_path.display()))?;

    if !result.status.success() {
        let stderr = String::from_utf8_lossy(&result.stderr);
        anyhow::bail!("reference input generation failed: {}", stderr.trim());
    }

    let raw = fs::read_to_string(&output_path).with_context(|| {
        format!(
            "failed to read reference inputs file: {}",
            output_path.display()
        )
    })?;
    drop(temp_output);
    serde_json::from_str(&raw).with_context(|| {
        format!(
            "failed to parse reference inputs file: {}",
            output_path.display()
        )
    })
}

fn run_outer_inputs_script(
    fixture_dir: &PathBuf,
    proof_dir: &PathBuf,
    artifacts_dir: &PathBuf,
) -> Result<OuterReferenceFile> {
    let script_path = workspace_root().join("scripts/outer-inputs.cjs");
    let temp_output =
        NamedTempFile::new().context("failed to create temporary outer input output file")?;
    let output_path = temp_output.path().to_path_buf();

    let result = ProcessCommand::new("node")
        .arg(&script_path)
        .arg("--fixture-dir")
        .arg(fixture_dir)
        .arg("--proof-dir")
        .arg(proof_dir)
        .arg("--artifacts-dir")
        .arg(artifacts_dir)
        .arg("--out")
        .arg(&output_path)
        .output()
        .with_context(|| format!("failed to execute node script: {}", script_path.display()))?;

    if !result.status.success() {
        let stderr = String::from_utf8_lossy(&result.stderr);
        anyhow::bail!("outer input generation failed: {}", stderr.trim());
    }

    let raw = fs::read_to_string(&output_path)
        .with_context(|| format!("failed to read outer input file: {}", output_path.display()))?;
    serde_json::from_str(&raw)
        .with_context(|| format!("failed to parse outer input file: {}", output_path.display()))
}

fn run_aggregate_inputs_script(
    request_path: &PathBuf,
    artifacts_dir: &PathBuf,
) -> Result<OuterReferenceFile> {
    let script_path = workspace_root().join("scripts/aggregate-inputs.cjs");
    let temp_output =
        NamedTempFile::new().context("failed to create temporary aggregate output file")?;
    let output_path = temp_output.path().to_path_buf();

    let result = ProcessCommand::new("node")
        .arg(&script_path)
        .arg("--request")
        .arg(request_path)
        .arg("--artifacts-dir")
        .arg(artifacts_dir)
        .arg("--out")
        .arg(&output_path)
        .output()
        .with_context(|| format!("failed to execute node script: {}", script_path.display()))?;

    if !result.status.success() {
        let stderr = String::from_utf8_lossy(&result.stderr);
        anyhow::bail!("aggregate input generation failed: {}", stderr.trim());
    }

    let raw = fs::read_to_string(&output_path)
        .with_context(|| format!("failed to read aggregate input file: {}", output_path.display()))?;
    serde_json::from_str(&raw)
        .with_context(|| format!("failed to parse aggregate input file: {}", output_path.display()))
}

fn local_artifacts_dir_for_version(version: &str, artifacts_dir: Option<&PathBuf>) -> PathBuf {
    artifacts_dir
        .cloned()
        .unwrap_or_else(|| workspace_root().join("artifacts").join("registry").join(format!("minimal-default-{version}")))
}

fn collect_solve_jobs(
    reference: &ReferenceInputsFile,
    resolved: &prover_core::ResolvedProofPlan,
) -> Vec<CircuitSolveJob> {
    let mut jobs = vec![
        CircuitSolveJob {
            circuit_name: resolved.circuits.dsc_name.clone(),
            inputs: reference.dsc_inputs.clone(),
        },
        CircuitSolveJob {
            circuit_name: resolved.circuits.id_data_name.clone(),
            inputs: reference.id_data_inputs.clone(),
        },
        CircuitSolveJob {
            circuit_name: resolved.circuits.integrity_name.clone(),
            inputs: reference.integrity_inputs.clone(),
        },
    ];

    jobs.extend(
        reference
            .disclosures
            .iter()
            .map(|disclosure| CircuitSolveJob {
                circuit_name: disclosure.circuit_name.clone(),
                inputs: disclosure.inputs.clone(),
            }),
    );
    jobs
}

fn split_hex_fields(bytes: &[u8]) -> Result<Vec<String>> {
    if bytes.len() % 32 != 0 {
        anyhow::bail!(
            "buffer length {} is not a multiple of 32 bytes",
            bytes.len()
        );
    }
    Ok(bytes
        .chunks(32)
        .map(|chunk| format!("0x{}", bytes_to_hex(chunk)))
        .collect())
}

fn bytes_to_hex(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        use std::fmt::Write as _;
        let _ = write!(out, "{byte:02x}");
    }
    out
}

fn solve_fixture_jobs(
    store: &ArtifactStore,
    version: &str,
    jobs: &[CircuitSolveJob],
    output_dir: Option<&PathBuf>,
    generate_proofs: bool,
    proof_options: ProofOptions,
) -> Result<serde_json::Value> {
    if let Some(output_dir) = output_dir {
        fs::create_dir_all(output_dir).with_context(|| {
            format!(
                "failed to create output directory: {}",
                output_dir.display()
            )
        })?;
    }

    let mut circuit_summaries = Vec::new();
    for job in jobs {
        let circuit = store.load_packaged_circuit(version, &job.circuit_name)?;
        let witness = if generate_proofs {
            solve_serialized_legacy_witness_for_circuit(&circuit, job.inputs.clone())
        } else {
            solve_compressed_witness_for_circuit(&circuit, job.inputs.clone())
        }
        .with_context(|| format!("failed to solve witness for {}", job.circuit_name))?;

        let mut entry = serde_json::json!({
            "circuit_name": job.circuit_name,
            "witness_bytes": witness.len(),
        });

        if let Some(output_dir) = output_dir {
            let input_path = output_dir.join(format!("{}.inputs.json", job.circuit_name));
            fs::write(&input_path, serde_json::to_vec_pretty(&job.inputs)?).with_context(|| {
                format!(
                    "failed to write circuit input file: {}",
                    input_path.display()
                )
            })?;
            let witness_path = output_dir.join(format!("{}.witness.bin", job.circuit_name));
            fs::write(&witness_path, &witness).with_context(|| {
                format!("failed to write witness file: {}", witness_path.display())
            })?;
            entry["inputs_path"] = serde_json::Value::String(input_path.display().to_string());
            entry["witness_path"] = serde_json::Value::String(witness_path.display().to_string());
        }

        if generate_proofs {
            let proof = prove_circuit_with_witness(&circuit, &witness, &proof_options)
                .with_context(|| format!("failed to generate proof for {}", job.circuit_name))?;
            let packaged_vk = packaged_verification_key(&circuit)?;
            let generated_vk_matches_packaged =
                verification_keys_match(&packaged_vk, &proof.verification_key);
            let verified = verify_inner_proof(&proof, &packaged_vk, &proof_options)
                .with_context(|| format!("failed to verify proof for {}", job.circuit_name))?;
            if let Some(output_dir) = output_dir {
                persist_proof_artifacts(
                    output_dir,
                    &job.circuit_name,
                    &proof,
                    &packaged_vk,
                    generated_vk_matches_packaged,
                )?;
            }
            entry["proof_bytes"] = serde_json::Value::from(proof.proof.len() as u64);
            entry["public_inputs_count"] = serde_json::Value::from(proof.public_inputs_count);
            entry["proof_verified"] = serde_json::Value::Bool(verified);
            entry["generated_vk_matches_packaged"] =
                serde_json::Value::Bool(generated_vk_matches_packaged);
        }

        circuit_summaries.push(entry);
    }

    let mut summary = serde_json::json!({
        "generate_proofs": generate_proofs,
        "circuits": circuit_summaries,
    });
    if let Some(output_dir) = output_dir {
        let summary_path = output_dir.join("summary.json");
        fs::write(&summary_path, serde_json::to_vec_pretty(&summary)?)
            .with_context(|| format!("failed to write summary file: {}", summary_path.display()))?;
        summary["summary_path"] = serde_json::Value::String(summary_path.display().to_string());
    }
    Ok(summary)
}

fn verify_inner_proof(
    proof: &ProofArtifacts,
    verification_key: &[u8],
    proof_options: &ProofOptions,
) -> Result<bool> {
    verify_circuit_proof(
        &proof.proof,
        &proof.public_inputs,
        verification_key,
        proof_options,
    )
}

fn persist_proof_artifacts(
    output_dir: &PathBuf,
    circuit_name: &str,
    proof: &ProofArtifacts,
    packaged_verification_key: &[u8],
    generated_vk_matches_packaged: bool,
) -> Result<()> {
    let proof_path = output_dir.join(format!("{}.proof.bin", circuit_name));
    fs::write(&proof_path, &proof.proof)
        .with_context(|| format!("failed to write proof file: {}", proof_path.display()))?;
    let vk_path = output_dir.join(format!("{}.vk.bin", circuit_name));
    fs::write(&vk_path, packaged_verification_key).with_context(|| {
        format!(
            "failed to write verification key file: {}",
            vk_path.display()
        )
    })?;
    if !generated_vk_matches_packaged {
        let generated_vk_path = output_dir.join(format!("{}.generated_vk.bin", circuit_name));
        fs::write(&generated_vk_path, &proof.verification_key).with_context(|| {
            format!(
                "failed to write generated verification key file: {}",
                generated_vk_path.display()
            )
        })?;
    }
    let public_inputs_path = output_dir.join(format!("{}.public_inputs.bin", circuit_name));
    fs::write(&public_inputs_path, &proof.public_inputs).with_context(|| {
        format!(
            "failed to write public inputs file: {}",
            public_inputs_path.display()
        )
    })?;
    Ok(())
}
