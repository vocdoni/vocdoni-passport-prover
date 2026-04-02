use crate::analysis::{analyze_document, DocumentAnalysis};
use crate::certificates::{derive_document_circuit_context, PackagedCertificate};
use crate::config::CompatibilityMatrix;
use crate::planning::{build_proof_plan, ProofPlan};
use crate::registry::Manifest;
use crate::resolution::{
    resolve_document_circuits, DocumentCircuitContext, ResolvedDocumentCircuits,
};
use prover_types::{FixtureBundle, InnerProofBundle, OuterProofResult};
use std::collections::BTreeMap;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum PipelineError {
    #[error("invalid fixture: {0}")]
    InvalidFixture(String),
    #[error("not implemented yet: {0}")]
    NotImplemented(&'static str),
}

#[derive(Debug, Default, Clone)]
pub struct ProverPipeline;

#[derive(Debug, Clone)]
pub struct ResolvedProofPlan {
    pub proof_plan: ProofPlan,
    pub analysis: DocumentAnalysis,
    pub csca: PackagedCertificate,
    pub document_context: DocumentCircuitContext,
    pub circuits: ResolvedDocumentCircuits,
}

impl ProverPipeline {
    pub fn new() -> Self {
        Self
    }

    pub fn validate_fixture(&self, fixture: &FixtureBundle) -> Result<(), PipelineError> {
        fixture
            .validate()
            .map_err(|error| PipelineError::InvalidFixture(error.to_string()))?;
        Ok(())
    }

    pub fn plan_fixture(&self, fixture: &FixtureBundle) -> Result<ProofPlan, PipelineError> {
        self.validate_fixture(fixture)?;
        build_proof_plan(fixture).map_err(|error| PipelineError::InvalidFixture(error.to_string()))
    }

    pub fn analyze_document(
        &self,
        fixture: &FixtureBundle,
    ) -> Result<DocumentAnalysis, PipelineError> {
        self.validate_fixture(fixture)?;
        analyze_document(&fixture.document)
            .map_err(|error| PipelineError::InvalidFixture(error.to_string()))
    }

    pub fn resolve_proof_plan(
        &self,
        fixture: &FixtureBundle,
        manifest: &Manifest,
        csca: &PackagedCertificate,
    ) -> Result<ResolvedProofPlan, PipelineError> {
        let proof_plan = self.plan_fixture(fixture)?;
        let analysis = self.analyze_document(fixture)?;
        let csca = csca.clone();
        let document_context =
            derive_document_circuit_context(&analysis, &csca, proof_plan.outer_circuit.clone())
                .map_err(|error| PipelineError::InvalidFixture(error.to_string()))?;
        let circuits = resolve_document_circuits(manifest, &document_context)
            .map_err(|error| PipelineError::InvalidFixture(error.to_string()))?;

        Ok(ResolvedProofPlan {
            proof_plan,
            analysis,
            csca,
            document_context,
            circuits,
        })
    }

    pub fn prove_inner(&self, fixture: &FixtureBundle) -> Result<InnerProofBundle, PipelineError> {
        let plan = self.plan_fixture(fixture)?;
        let message = if plan.unresolved_requirements.is_empty() {
            "inner proof generation is not wired yet"
        } else {
            "inner proof generation is blocked until document-derived circuit resolution is implemented"
        };
        Err(PipelineError::NotImplemented(message))
    }

    pub fn prove_outer(&self, fixture: &FixtureBundle) -> Result<OuterProofResult, PipelineError> {
        let _plan = self.plan_fixture(fixture)?;
        Err(PipelineError::NotImplemented(
            "outer proof generation will be implemented after the canonical backend is wrapped in Rust",
        ))
    }

    pub fn compatibility_matrix_summary(
        &self,
        matrix: &CompatibilityMatrix,
    ) -> BTreeMap<&'static str, String> {
        BTreeMap::from([
            ("status", matrix.status.clone()),
            (
                "canonical_backend",
                matrix.proving.canonical_backend.clone(),
            ),
            ("circuit_version", matrix.registry.circuit_version.clone()),
            ("noir_version", matrix.proving.noir_version.clone()),
            ("bb_version", matrix.proving.bb_version.clone()),
            (
                "oracle_hash_inner",
                matrix.proving.inner_oracle_hash.clone(),
            ),
            (
                "oracle_hash_outer_evm",
                matrix.proving.outer_evm_oracle_hash.clone(),
            ),
        ])
    }
}
