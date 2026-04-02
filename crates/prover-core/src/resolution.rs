use crate::registry::Manifest;
use anyhow::{bail, Context, Result};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum HashAlgorithmToken {
    Sha1,
    Sha224,
    Sha256,
    Sha384,
    Sha512,
}

impl HashAlgorithmToken {
    pub fn normalize(value: &str) -> Self {
        let normalized = value.to_ascii_lowercase().replace('-', "");
        if normalized.contains("512") {
            Self::Sha512
        } else if normalized.contains("384") {
            Self::Sha384
        } else if normalized.contains("224") {
            Self::Sha224
        } else if normalized.contains('1') {
            Self::Sha1
        } else {
            Self::Sha256
        }
    }

    fn as_suffix(&self) -> &'static str {
        match self {
            Self::Sha1 => "sha1",
            Self::Sha224 => "sha224",
            Self::Sha256 => "sha256",
            Self::Sha384 => "sha384",
            Self::Sha512 => "sha512",
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum EcCurveToken {
    NistP192,
    NistP224,
    NistP256,
    NistP384,
    NistP521,
    Brainpool160r1,
    Brainpool160t1,
    Brainpool192r1,
    Brainpool192t1,
    Brainpool224r1,
    Brainpool224t1,
    Brainpool256r1,
    Brainpool256t1,
    Brainpool320r1,
    Brainpool320t1,
    Brainpool384r1,
    Brainpool384t1,
    Brainpool512r1,
    Brainpool512t1,
}

impl EcCurveToken {
    pub fn from_zkpassport_name(value: &str) -> Result<Self> {
        match value {
            "P-192" => Ok(Self::NistP192),
            "P-224" => Ok(Self::NistP224),
            "P-256" => Ok(Self::NistP256),
            "P-384" => Ok(Self::NistP384),
            "P-521" => Ok(Self::NistP521),
            "brainpoolP160r1" => Ok(Self::Brainpool160r1),
            "brainpoolP160t1" => Ok(Self::Brainpool160t1),
            "brainpoolP192r1" => Ok(Self::Brainpool192r1),
            "brainpoolP192t1" => Ok(Self::Brainpool192t1),
            "brainpoolP224r1" => Ok(Self::Brainpool224r1),
            "brainpoolP224t1" => Ok(Self::Brainpool224t1),
            "brainpoolP256r1" => Ok(Self::Brainpool256r1),
            "brainpoolP256t1" => Ok(Self::Brainpool256t1),
            "brainpoolP320r1" => Ok(Self::Brainpool320r1),
            "brainpoolP320t1" => Ok(Self::Brainpool320t1),
            "brainpoolP384r1" => Ok(Self::Brainpool384r1),
            "brainpoolP384t1" => Ok(Self::Brainpool384t1),
            "brainpoolP512r1" => Ok(Self::Brainpool512r1),
            "brainpoolP512t1" => Ok(Self::Brainpool512t1),
            other => bail!("unsupported ECDSA curve for circuit selection: {other}"),
        }
    }

    fn as_suffix(&self) -> &'static str {
        match self {
            Self::NistP192 => "nist_p192",
            Self::NistP224 => "nist_p224",
            Self::NistP256 => "nist_p256",
            Self::NistP384 => "nist_p384",
            Self::NistP521 => "nist_p521",
            Self::Brainpool160r1 => "brainpool_160r1",
            Self::Brainpool160t1 => "brainpool_160t1",
            Self::Brainpool192r1 => "brainpool_192r1",
            Self::Brainpool192t1 => "brainpool_192t1",
            Self::Brainpool224r1 => "brainpool_224r1",
            Self::Brainpool224t1 => "brainpool_224t1",
            Self::Brainpool256r1 => "brainpool_256r1",
            Self::Brainpool256t1 => "brainpool_256t1",
            Self::Brainpool320r1 => "brainpool_320r1",
            Self::Brainpool320t1 => "brainpool_320t1",
            Self::Brainpool384r1 => "brainpool_384r1",
            Self::Brainpool384t1 => "brainpool_384t1",
            Self::Brainpool512r1 => "brainpool_512r1",
            Self::Brainpool512t1 => "brainpool_512t1",
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(tag = "scheme", rename_all = "snake_case")]
pub enum SignatureKey {
    Ecdsa { curve: EcCurveToken },
    RsaPkcs { key_bits: u32 },
    RsaPss { key_bits: u32 },
}

impl SignatureKey {
    fn as_suffix(&self) -> String {
        match self {
            Self::Ecdsa { curve } => format!("ecdsa_{}", curve.as_suffix()),
            Self::RsaPkcs { key_bits } => format!("rsa_pkcs_{key_bits}"),
            Self::RsaPss { key_bits } => format!("rsa_pss_{key_bits}"),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SignatureCircuitSpec {
    pub tbs_bucket: u32,
    pub key: SignatureKey,
    pub hash: HashAlgorithmToken,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct DocumentCircuitContext {
    pub dsc: SignatureCircuitSpec,
    pub id_data: SignatureCircuitSpec,
    pub dg_hash: HashAlgorithmToken,
    pub outer_circuit: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ResolvedDocumentCircuits {
    pub dsc_name: String,
    pub id_data_name: String,
    pub integrity_name: String,
    pub outer_name: String,
}

pub fn resolve_document_circuits(
    manifest: &Manifest,
    context: &DocumentCircuitContext,
) -> Result<ResolvedDocumentCircuits> {
    let dsc_name = format!(
        "sig_check_dsc_tbs_{}_{}_{}",
        context.dsc.tbs_bucket,
        context.dsc.key.as_suffix(),
        context.dsc.hash.as_suffix()
    );
    let id_data_name = format!(
        "sig_check_id_data_tbs_{}_{}_{}",
        context.id_data.tbs_bucket,
        context.id_data.key.as_suffix(),
        context.id_data.hash.as_suffix()
    );
    let integrity_name = format!(
        "data_check_integrity_sa_{}_dg_{}",
        context.id_data.hash.as_suffix(),
        context.dg_hash.as_suffix()
    );

    for name in [
        dsc_name.as_str(),
        id_data_name.as_str(),
        integrity_name.as_str(),
        context.outer_circuit.as_str(),
    ] {
        manifest
            .circuits
            .get(name)
            .with_context(|| format!("derived circuit not found in manifest: {name}"))?;
    }

    Ok(ResolvedDocumentCircuits {
        dsc_name,
        id_data_name,
        integrity_name,
        outer_name: context.outer_circuit.clone(),
    })
}

#[cfg(test)]
mod tests {
    use super::{
        resolve_document_circuits, DocumentCircuitContext, EcCurveToken, HashAlgorithmToken,
        SignatureCircuitSpec, SignatureKey,
    };
    use crate::registry::{Manifest, ManifestCircuit};
    use std::collections::BTreeMap;

    fn manifest_with(names: &[&str]) -> Manifest {
        Manifest {
            version: "0.16.0".to_string(),
            root: "0xroot".to_string(),
            circuits: names
                .iter()
                .map(|name| {
                    (
                        (*name).to_string(),
                        ManifestCircuit {
                            hash: format!("0x{}", name.len()),
                            size: 1,
                        },
                    )
                })
                .collect::<BTreeMap<_, _>>(),
        }
    }

    #[test]
    fn resolves_expected_circuit_names() {
        let manifest = manifest_with(&[
            "sig_check_dsc_tbs_1000_ecdsa_nist_p256_sha256",
            "sig_check_id_data_tbs_700_rsa_pss_2048_sha384",
            "data_check_integrity_sa_sha384_dg_sha256",
            "outer_evm_count_4",
        ]);
        let context = DocumentCircuitContext {
            dsc: SignatureCircuitSpec {
                tbs_bucket: 1000,
                key: SignatureKey::Ecdsa {
                    curve: EcCurveToken::NistP256,
                },
                hash: HashAlgorithmToken::Sha256,
            },
            id_data: SignatureCircuitSpec {
                tbs_bucket: 700,
                key: SignatureKey::RsaPss { key_bits: 2048 },
                hash: HashAlgorithmToken::Sha384,
            },
            dg_hash: HashAlgorithmToken::Sha256,
            outer_circuit: "outer_evm_count_4".to_string(),
        };

        let resolved = resolve_document_circuits(&manifest, &context).expect("resolved circuits");
        assert_eq!(
            resolved.dsc_name,
            "sig_check_dsc_tbs_1000_ecdsa_nist_p256_sha256"
        );
        assert_eq!(
            resolved.id_data_name,
            "sig_check_id_data_tbs_700_rsa_pss_2048_sha384"
        );
        assert_eq!(
            resolved.integrity_name,
            "data_check_integrity_sa_sha384_dg_sha256"
        );
        assert_eq!(resolved.outer_name, "outer_evm_count_4");
    }

    #[test]
    fn normalizes_hash_algorithm_names() {
        assert_eq!(
            HashAlgorithmToken::normalize("SHA-256"),
            HashAlgorithmToken::Sha256
        );
        assert_eq!(
            HashAlgorithmToken::normalize("ecdsa-with-SHA384"),
            HashAlgorithmToken::Sha384
        );
        assert_eq!(
            HashAlgorithmToken::normalize("sha1"),
            HashAlgorithmToken::Sha1
        );
    }

    #[test]
    fn maps_zkpassport_curve_names() {
        assert_eq!(
            EcCurveToken::from_zkpassport_name("brainpoolP256r1").expect("curve"),
            EcCurveToken::Brainpool256r1
        );
    }
}
