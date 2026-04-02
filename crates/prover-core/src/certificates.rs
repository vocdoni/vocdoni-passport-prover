use crate::analysis::DocumentAnalysis;
use crate::config::CompatibilityMatrix;
use crate::resolution::{
    DocumentCircuitContext, HashAlgorithmToken, SignatureCircuitSpec, SignatureKey,
};
use anyhow::{Context, Result};
use flate2::read::GzDecoder;
use reqwest::blocking::Client;
use serde::{Deserialize, Serialize};
use std::io::Read;

const CERTIFICATE_REGISTRY_ID: u64 = 1;
const LATEST_ROOT_WITH_PARAM_SIGNATURE: &str = "0xc3bc16e8";

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PackagedCertificatesFile {
    #[serde(default)]
    pub version: u64,
    #[serde(default)]
    pub timestamp: u64,
    #[serde(default)]
    pub root: String,
    pub certificates: Vec<PackagedCertificate>,
    #[serde(default)]
    pub serialised: Vec<Vec<String>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PackagedCertificate {
    pub country: String,
    pub signature_algorithm: String,
    pub hash_algorithm: String,
    pub public_key: PackagedCertificatePublicKey,
    pub validity: CertificateValidity,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub private_key_usage_period: Option<PrivateKeyUsagePeriod>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub subject_key_identifier: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub authority_key_identifier: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub fingerprint: Option<String>,
    #[serde(default)]
    pub tags: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub r#type: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CertificateValidity {
    pub not_before: u64,
    pub not_after: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PrivateKeyUsagePeriod {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub not_before: Option<u64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub not_after: Option<u64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum PackagedCertificatePublicKey {
    EC {
        curve: String,
        key_size: u32,
        public_key_x: String,
        public_key_y: String,
    },
    RSA {
        key_size: u32,
        modulus: String,
        exponent: u64,
    },
}

#[derive(Debug, Clone)]
pub struct CertificateRegistryClient {
    http: Client,
    rpc_url: String,
    root_registry: String,
    certificate_base_url: String,
}

impl CertificateRegistryClient {
    pub fn from_matrix(matrix: &CompatibilityMatrix) -> Self {
        Self {
            http: Client::new(),
            rpc_url: matrix.registry.rpc_url.clone(),
            root_registry: matrix.registry.root_registry.clone(),
            certificate_base_url: matrix.registry.certificate_base_url.clone(),
        }
    }

    pub fn fetch_latest_root(&self) -> Result<String> {
        let data = format!(
            "{}{:064x}",
            LATEST_ROOT_WITH_PARAM_SIGNATURE, CERTIFICATE_REGISTRY_ID
        );
        let payload = serde_json::json!({
            "jsonrpc": "2.0",
            "id": 1,
            "method": "eth_call",
            "params": [
                {
                    "to": self.root_registry,
                    "data": data,
                },
                "latest"
            ]
        });
        let value: serde_json::Value = self
            .http
            .post(&self.rpc_url)
            .json(&payload)
            .send()
            .with_context(|| format!("failed to call RPC endpoint: {}", self.rpc_url))?
            .error_for_status()
            .with_context(|| format!("RPC request failed: {}", self.rpc_url))?
            .json()
            .context("failed to decode RPC response JSON")?;
        if let Some(error) = value.get("error") {
            anyhow::bail!("RPC returned error: {error}");
        }
        let root = value
            .get("result")
            .and_then(serde_json::Value::as_str)
            .context("RPC result field missing or not a string")?;
        Ok(normalize_hash(root))
    }

    pub fn fetch_certificates(&self, root: Option<&str>) -> Result<PackagedCertificatesFile> {
        let root = match root {
            Some(root) => normalize_hash(root),
            None => self.fetch_latest_root()?,
        };
        let url = format!("{}/{}.json", self.certificate_base_url, root);
        let bytes = self
            .http
            .get(&url)
            .send()
            .with_context(|| format!("failed to GET certificates: {url}"))?
            .error_for_status()
            .with_context(|| format!("certificate request failed: {url}"))?
            .bytes()
            .with_context(|| format!("failed to read certificate bytes: {url}"))?;
        let mut packaged: PackagedCertificatesFile = decode_json_bytes(&bytes, &url)
            .with_context(|| format!("invalid certificate JSON: {url}"))?;
        if packaged.root.is_empty() {
            packaged.root = root.clone();
        }
        if normalize_hash(&packaged.root) != root {
            anyhow::bail!(
                "certificate root mismatch: requested={}, received={}",
                root,
                packaged.root
            );
        }
        Ok(packaged)
    }

    pub fn resolve_csca(
        &self,
        analysis: &DocumentAnalysis,
        packaged: &PackagedCertificatesFile,
    ) -> Option<PackagedCertificate> {
        let same_country = packaged
            .certificates
            .iter()
            .filter(|cert| cert.country.eq_ignore_ascii_case(&analysis.dsc_country))
            .cloned()
            .collect::<Vec<_>>();

        if same_country.is_empty() {
            return None;
        }

        let aki = analysis
            .dsc_authority_key_identifier
            .as_ref()
            .map(|value| normalize_hash(value));
        let mut aki_matches = same_country
            .iter()
            .filter(|cert| {
                aki.as_ref().is_some_and(|aki| {
                    cert.subject_key_identifier
                        .as_ref()
                        .map(|ski| normalize_hash(ski) == *aki)
                        .unwrap_or(false)
                })
            })
            .cloned()
            .collect::<Vec<_>>();

        sort_candidate_certificates(&mut aki_matches, analysis);
        if let Some(cert) = aki_matches.into_iter().next() {
            return Some(cert);
        }

        let mut candidates = same_country;
        sort_candidate_certificates(&mut candidates, analysis);
        candidates.into_iter().next()
    }
}

pub fn derive_document_circuit_context(
    analysis: &DocumentAnalysis,
    csca: &PackagedCertificate,
    outer_circuit: String,
) -> Result<DocumentCircuitContext> {
    let dsc_key = match &csca.public_key {
        PackagedCertificatePublicKey::EC { curve, .. } => SignatureKey::Ecdsa {
            curve: crate::resolution::EcCurveToken::from_zkpassport_name(curve)?,
        },
        PackagedCertificatePublicKey::RSA { key_size, .. } => {
            if csca.signature_algorithm.eq_ignore_ascii_case("RSA-PSS") {
                SignatureKey::RsaPss {
                    key_bits: *key_size,
                }
            } else {
                SignatureKey::RsaPkcs {
                    key_bits: *key_size,
                }
            }
        }
    };

    Ok(DocumentCircuitContext {
        dsc: SignatureCircuitSpec {
            tbs_bucket: analysis.tbs_bucket,
            key: dsc_key,
            hash: HashAlgorithmToken::normalize(&csca.hash_algorithm),
        },
        id_data: SignatureCircuitSpec {
            tbs_bucket: analysis.tbs_bucket,
            key: analysis.id_data_public_key.clone(),
            hash: analysis.sod_signature_hash.clone(),
        },
        dg_hash: analysis.dg_hash.clone(),
        outer_circuit,
    })
}

fn sort_candidate_certificates(certs: &mut [PackagedCertificate], analysis: &DocumentAnalysis) {
    certs.sort_by_key(|cert| {
        let signature_score = if cert
            .signature_algorithm
            .eq_ignore_ascii_case(&analysis.dsc_parent_signature_algorithm)
        {
            0
        } else {
            1
        };
        let hash_score =
            if HashAlgorithmToken::normalize(&cert.hash_algorithm) == analysis.dsc_signature_hash {
                0
            } else {
                1
            };
        (signature_score, hash_score)
    });
}

fn strip_0x(value: &str) -> &str {
    value.strip_prefix("0x").unwrap_or(value)
}

fn normalize_hash(value: &str) -> String {
    format!("0x{:0>64}", strip_0x(value).to_ascii_lowercase())
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

#[cfg(test)]
mod tests {
    use super::{
        derive_document_circuit_context, CertificateRegistryClient, CertificateValidity,
        PackagedCertificate, PackagedCertificatePublicKey, PackagedCertificatesFile,
    };
    use crate::analysis::{DocumentAnalysis, MrzFields};
    use crate::resolution::{HashAlgorithmToken, SignatureKey};

    #[test]
    fn picks_aki_match_before_country_fallback() {
        let analysis = DocumentAnalysis {
            mrz: "MRZ".to_string(),
            mrz_fields: MrzFields::default(),
            document_type: "passport".to_string(),
            tbs_len: 900,
            tbs_bucket: 1000,
            dsc_country: "FRA".to_string(),
            dsc_authority_key_identifier: Some("0xabc".to_string()),
            dsc_signature_hash: HashAlgorithmToken::Sha256,
            dsc_parent_signature_algorithm: "RSA".to_string(),
            dsc_signature_algorithm_oid: "1.2.840.113549.1.1.11".to_string(),
            sod_digest_algorithm_oid: "2.16.840.1.101.3.4.2.1".to_string(),
            sod_signature_algorithm_oid: "1.2.840.113549.1.1.11".to_string(),
            sod_signature_hash: HashAlgorithmToken::Sha256,
            dg_hash: HashAlgorithmToken::Sha256,
            id_data_public_key: SignatureKey::RsaPkcs { key_bits: 2048 },
            econtent_len: 10,
        };
        let packaged = PackagedCertificatesFile {
            version: 1,
            timestamp: 0,
            root: "0x1".to_string(),
            certificates: vec![
                PackagedCertificate {
                    country: "FRA".to_string(),
                    signature_algorithm: "RSA".to_string(),
                    hash_algorithm: "SHA-256".to_string(),
                    public_key: PackagedCertificatePublicKey::RSA {
                        key_size: 4096,
                        modulus: "0x1".to_string(),
                        exponent: 65537,
                    },
                    validity: CertificateValidity {
                        not_before: 0,
                        not_after: 0,
                    },
                    private_key_usage_period: None,
                    subject_key_identifier: Some("0xabc".to_string()),
                    authority_key_identifier: None,
                    fingerprint: None,
                    tags: vec![],
                    r#type: None,
                },
                PackagedCertificate {
                    country: "FRA".to_string(),
                    signature_algorithm: "ECDSA".to_string(),
                    hash_algorithm: "SHA-384".to_string(),
                    public_key: PackagedCertificatePublicKey::EC {
                        curve: "P-256".to_string(),
                        key_size: 256,
                        public_key_x: "0x1".to_string(),
                        public_key_y: "0x2".to_string(),
                    },
                    validity: CertificateValidity {
                        not_before: 0,
                        not_after: 0,
                    },
                    private_key_usage_period: None,
                    subject_key_identifier: Some("0xdef".to_string()),
                    authority_key_identifier: None,
                    fingerprint: None,
                    tags: vec![],
                    r#type: None,
                },
            ],
            serialised: vec![],
        };

        let client = CertificateRegistryClient {
            http: reqwest::blocking::Client::new(),
            rpc_url: String::new(),
            root_registry: String::new(),
            certificate_base_url: String::new(),
        };
        let csca = client.resolve_csca(&analysis, &packaged).expect("match");
        assert_eq!(csca.subject_key_identifier.as_deref(), Some("0xabc"));

        let context =
            derive_document_circuit_context(&analysis, &csca, "outer_evm_count_4".to_string())
                .expect("context");
        assert!(matches!(
            context.dsc.key,
            SignatureKey::RsaPkcs { key_bits: 4096 }
        ));
    }
}
