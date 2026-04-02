pub mod analysis;
pub mod artifacts;
pub mod certificates;
pub mod circuit;
pub mod config;
pub mod fixture;
pub mod pipeline;
pub mod planning;
pub mod proving;
pub mod registry;
pub mod resolution;
pub mod witness;

pub use analysis::{analyze_document, DocumentAnalysis, MrzFields};
pub use artifacts::ArtifactStore;
pub use certificates::{
    derive_document_circuit_context, CertificateRegistryClient, PackagedCertificate,
    PackagedCertificatePublicKey, PackagedCertificatesFile,
};
pub use config::{load_compatibility_matrix, CompatibilityMatrix};
pub use fixture::{load_fixture_bundle, load_fixture_dir};
pub use pipeline::{PipelineError, ProverPipeline, ResolvedProofPlan};
pub use planning::{build_proof_plan, PlannedCircuit, ProofPlan};
pub use proving::{
    prove_circuit_with_witness, split_flattened_proof, verify_circuit_proof, ProofArtifacts,
    ProofOptions, ProofOracleHash,
};
pub use registry::{
    CircuitFingerprint, Manifest, PackagedCircuit, RegistryClient, RegistrySnapshot,
};
pub use resolution::{
    resolve_document_circuits, DocumentCircuitContext, EcCurveToken, HashAlgorithmToken,
    ResolvedDocumentCircuits, SignatureCircuitSpec, SignatureKey,
};
pub use witness::{
    solve_compressed_witness, solve_compressed_witness_for_circuit, solve_legacy_witness,
    solve_legacy_witness_for_circuit, solve_serialized_legacy_witness,
    solve_serialized_legacy_witness_for_circuit, solve_serialized_witness,
    solve_serialized_witness_for_circuit, WitnessPayload,
};
