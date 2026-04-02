use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum ValidationError {
    #[error("missing required field: {0}")]
    MissingField(&'static str),
    #[error("invalid value for {field}: {message}")]
    InvalidValue {
        field: &'static str,
        message: String,
    },
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
pub struct DocumentInput {
    pub dg1_base64: String,
    pub sod_der_base64: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub dg2_base64: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub document_type: Option<String>,
}

impl DocumentInput {
    pub fn validate(&self) -> Result<(), ValidationError> {
        if self.dg1_base64.trim().is_empty() {
            return Err(ValidationError::MissingField("dg1_base64"));
        }
        if self.sod_der_base64.trim().is_empty() {
            return Err(ValidationError::MissingField("sod_der_base64"));
        }
        Ok(())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
pub struct ProofRequest {
    pub circuit_version: String,
    pub chain_id: u64,
    pub service_scope: String,
    pub service_subscope: String,
    pub current_date: u64,
    #[serde(default)]
    pub query: serde_json::Value,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub petition_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub aggregate_mode: Option<String>,
}

impl ProofRequest {
    pub fn validate(&self) -> Result<(), ValidationError> {
        if self.circuit_version.trim().is_empty() {
            return Err(ValidationError::MissingField("circuit_version"));
        }
        if self.service_scope.trim().is_empty() {
            return Err(ValidationError::MissingField("service_scope"));
        }
        if self.service_subscope.trim().is_empty() {
            return Err(ValidationError::MissingField("service_subscope"));
        }
        if self.current_date == 0 {
            return Err(ValidationError::InvalidValue {
                field: "current_date",
                message: "must be a unix timestamp greater than zero".to_string(),
            });
        }
        Ok(())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
pub struct RecursiveProof {
    #[serde(default)]
    pub circuit_name: String,
    #[serde(default)]
    pub proof: Vec<String>,
    #[serde(default)]
    pub public_inputs: Vec<String>,
    #[serde(default)]
    pub vkey: Vec<String>,
    #[serde(default)]
    pub key_hash: String,
    #[serde(default)]
    pub tree_hash_path: Vec<String>,
    #[serde(default)]
    pub tree_index: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
pub struct InnerProofBundle {
    pub version: String,
    pub current_date: u64,
    pub dsc: RecursiveProof,
    pub id_data: RecursiveProof,
    pub integrity: RecursiveProof,
    #[serde(default)]
    pub disclosures: Vec<RecursiveProof>,
    #[serde(default)]
    pub metadata: BTreeMap<String, String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
pub struct OuterProofResult {
    pub version: String,
    pub name: String,
    pub proof: String,
    #[serde(default)]
    pub public_inputs: Vec<String>,
    pub vkey_hash: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub nullifier: Option<String>,
    #[serde(default)]
    pub metadata: BTreeMap<String, String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
pub struct FixtureMetadata {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub fixture_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub notes: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
pub struct FixtureBundle {
    pub document: DocumentInput,
    pub request: ProofRequest,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub expected_inner: Option<InnerProofBundle>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub expected_outer: Option<OuterProofResult>,
    #[serde(default)]
    pub metadata: FixtureMetadata,
}

impl FixtureBundle {
    pub fn validate(&self) -> Result<(), ValidationError> {
        self.document.validate()?;
        self.request.validate()?;
        Ok(())
    }
}
