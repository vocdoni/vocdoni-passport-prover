use crate::resolution::{HashAlgorithmToken, SignatureKey};
use anyhow::{bail, Context, Result};
use base64::Engine;
use prover_types::DocumentInput;
use serde::{Deserialize, Serialize};
use x509_parser::extensions::ParsedExtension;
use x509_parser::prelude::parse_x509_certificate;

const OID_RSA_ENCRYPTION: &str = "1.2.840.113549.1.1.1";
const OID_RSASSA_PSS: &str = "1.2.840.113549.1.1.10";
const OID_EC_PUBLIC_KEY: &str = "1.2.840.10045.2.1";

#[derive(Debug, Clone, Serialize, Deserialize, Default, PartialEq, Eq)]
pub struct MrzFields {
    pub issuing_country: String,
    pub document_number: String,
    pub nationality: String,
    pub date_of_birth: String,
    pub gender: String,
    pub expiry_date: String,
    pub last_name: String,
    pub first_name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct DocumentAnalysis {
    pub mrz: String,
    pub mrz_fields: MrzFields,
    pub document_type: String,
    pub tbs_len: usize,
    pub tbs_bucket: u32,
    pub dsc_country: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub dsc_authority_key_identifier: Option<String>,
    pub dsc_signature_hash: HashAlgorithmToken,
    pub dsc_parent_signature_algorithm: String,
    pub dsc_signature_algorithm_oid: String,
    pub sod_digest_algorithm_oid: String,
    pub sod_signature_algorithm_oid: String,
    pub sod_signature_hash: HashAlgorithmToken,
    pub dg_hash: HashAlgorithmToken,
    pub id_data_public_key: SignatureKey,
    pub econtent_len: usize,
}

pub fn analyze_document(document: &DocumentInput) -> Result<DocumentAnalysis> {
    document.validate()?;

    let dg1 = base64::engine::general_purpose::STANDARD
        .decode(&document.dg1_base64)
        .context("failed to decode dg1_base64")?;
    let sod = base64::engine::general_purpose::STANDARD
        .decode(&document.sod_der_base64)
        .context("failed to decode sod_der_base64")?;

    let mrz = extract_mrz_from_dg1(&dg1)?;
    let mrz_fields = parse_mrz(&mrz);
    let parsed_sod = parse_sod_envelope(&sod)?;

    let (_, cert) = parse_x509_certificate(&parsed_sod.certificate_der)
        .map_err(|error| anyhow::anyhow!("failed to parse DSC certificate: {error}"))?;

    let authority_key_identifier =
        cert.extensions()
            .iter()
            .find_map(|extension| match extension.parsed_extension() {
                ParsedExtension::AuthorityKeyIdentifier(aki) => aki
                    .key_identifier
                    .as_ref()
                    .map(|value| format!("0x{}", hex_lower(value.0))),
                _ => None,
            });

    let dsc_country = extract_country_from_name(&cert.issuer().to_string())
        .or_else(|| extract_country_from_name(&cert.subject().to_string()))
        .unwrap_or_default();

    let id_data_public_key = parse_subject_public_key_info(
        cert.tbs_certificate
            .subject_pki
            .algorithm
            .algorithm
            .to_id_string()
            .as_str(),
        cert.tbs_certificate
            .subject_pki
            .algorithm
            .parameters
            .as_ref()
            .map(|value| value.data)
            .unwrap_or(&[]),
        &cert.tbs_certificate.subject_pki.subject_public_key.data,
    )?;

    Ok(DocumentAnalysis {
        mrz,
        mrz_fields,
        document_type: if document.document_type.as_deref() == Some("id_card") {
            "id_card".to_string()
        } else if document.document_type.as_deref() == Some("passport") {
            "passport".to_string()
        } else if document.document_type.as_deref().is_some() {
            document.document_type.clone().unwrap_or_default()
        } else if extract_mrz_from_dg1(&dg1)?.len() == 88 {
            "passport".to_string()
        } else {
            "id_card".to_string()
        },
        tbs_len: cert.tbs_certificate.as_ref().len(),
        tbs_bucket: bucket_tbs_len(cert.tbs_certificate.as_ref().len()),
        dsc_country: normalize_country_code(&dsc_country),
        dsc_authority_key_identifier: authority_key_identifier,
        dsc_signature_hash: hash_from_signature_and_digest_oids(
            &parsed_sod.certificate_signature_algorithm_oid,
            "",
        ),
        dsc_parent_signature_algorithm: signature_algorithm_family(
            &parsed_sod.certificate_signature_algorithm_oid,
        )
        .to_string(),
        dsc_signature_algorithm_oid: parsed_sod.certificate_signature_algorithm_oid,
        sod_digest_algorithm_oid: parsed_sod.digest_algorithm_oid.clone(),
        sod_signature_algorithm_oid: parsed_sod.signer_signature_algorithm_oid.clone(),
        sod_signature_hash: hash_from_signature_and_digest_oids(
            &parsed_sod.signer_signature_algorithm_oid,
            &parsed_sod.digest_algorithm_oid,
        ),
        dg_hash: hash_from_oid(&parsed_sod.econtent_hash_algorithm_oid).unwrap_or_else(|| {
            HashAlgorithmToken::normalize(&parsed_sod.econtent_hash_algorithm_oid)
        }),
        id_data_public_key,
        econtent_len: parsed_sod.econtent.len(),
    })
}

fn bucket_tbs_len(tbs_len: usize) -> u32 {
    if tbs_len <= 700 {
        700
    } else if tbs_len <= 1000 {
        1000
    } else if tbs_len <= 1200 {
        1200
    } else {
        1600
    }
}

fn normalize_country_code(value: &str) -> String {
    let clean = value.trim().to_ascii_uppercase();
    if clean.len() == 2 {
        isocountry::CountryCode::for_alpha2(&clean)
            .map(|country| country.alpha3().to_string())
            .unwrap_or(clean)
    } else {
        clean
    }
}

fn signature_algorithm_family(oid: &str) -> &'static str {
    if oid == OID_RSASSA_PSS {
        "RSA-PSS"
    } else if oid.starts_with("1.2.840.113549.1.1.") {
        "RSA"
    } else if oid.starts_with("1.2.840.10045.4.") {
        "ECDSA"
    } else {
        "UNKNOWN"
    }
}

fn hash_from_signature_and_digest_oids(
    signature_oid: &str,
    digest_oid: &str,
) -> HashAlgorithmToken {
    if signature_oid.contains("4.3.2") || signature_oid.ends_with(".11") {
        HashAlgorithmToken::Sha256
    } else if signature_oid.contains("4.3.3") || signature_oid.ends_with(".12") {
        HashAlgorithmToken::Sha384
    } else if signature_oid.contains("4.3.4") || signature_oid.ends_with(".13") {
        HashAlgorithmToken::Sha512
    } else if signature_oid.ends_with(".14") {
        HashAlgorithmToken::Sha224
    } else if signature_oid.ends_with(".5") {
        HashAlgorithmToken::Sha1
    } else {
        hash_from_oid(digest_oid).unwrap_or_else(|| HashAlgorithmToken::normalize(digest_oid))
    }
}

fn hash_from_oid(oid: &str) -> Option<HashAlgorithmToken> {
    match oid {
        "1.3.14.3.2.26" => Some(HashAlgorithmToken::Sha1),
        "2.16.840.1.101.3.4.2.4" => Some(HashAlgorithmToken::Sha224),
        "2.16.840.1.101.3.4.2.1" => Some(HashAlgorithmToken::Sha256),
        "2.16.840.1.101.3.4.2.2" => Some(HashAlgorithmToken::Sha384),
        "2.16.840.1.101.3.4.2.3" => Some(HashAlgorithmToken::Sha512),
        _ => None,
    }
}

fn parse_subject_public_key_info(
    algorithm_oid: &str,
    parameters_bytes: &[u8],
    subject_public_key: &[u8],
) -> Result<SignatureKey> {
    match algorithm_oid {
        OID_EC_PUBLIC_KEY => {
            let curve_oid = parse_oid_bytes(parameters_bytes)?;
            let curve = match curve_oid.as_str() {
                "1.2.840.10045.3.1.1" => "P-192",
                "1.3.132.0.33" => "P-224",
                "1.2.840.10045.3.1.7" => "P-256",
                "1.3.132.0.34" => "P-384",
                "1.3.132.0.35" => "P-521",
                "1.3.36.3.3.2.8.1.1.1" => "brainpoolP160r1",
                "1.3.36.3.3.2.8.1.1.2" => "brainpoolP160t1",
                "1.3.36.3.3.2.8.1.1.3" => "brainpoolP192r1",
                "1.3.36.3.3.2.8.1.1.4" => "brainpoolP192t1",
                "1.3.36.3.3.2.8.1.1.5" => "brainpoolP224r1",
                "1.3.36.3.3.2.8.1.1.6" => "brainpoolP224t1",
                "1.3.36.3.3.2.8.1.1.7" => "brainpoolP256r1",
                "1.3.36.3.3.2.8.1.1.8" => "brainpoolP256t1",
                "1.3.36.3.3.2.8.1.1.9" => "brainpoolP320r1",
                "1.3.36.3.3.2.8.1.1.10" => "brainpoolP320t1",
                "1.3.36.3.3.2.8.1.1.11" => "brainpoolP384r1",
                "1.3.36.3.3.2.8.1.1.12" => "brainpoolP384t1",
                "1.3.36.3.3.2.8.1.1.13" => "brainpoolP512r1",
                "1.3.36.3.3.2.8.1.1.14" => "brainpoolP512t1",
                _ => bail!("unsupported EC curve OID: {curve_oid}"),
            };
            Ok(SignatureKey::Ecdsa {
                curve: crate::resolution::EcCurveToken::from_zkpassport_name(curve)?,
            })
        }
        OID_RSA_ENCRYPTION | OID_RSASSA_PSS => {
            let key_bits = parse_rsa_key_size_bits(subject_public_key)?;
            if algorithm_oid == OID_RSASSA_PSS {
                Ok(SignatureKey::RsaPss { key_bits })
            } else {
                Ok(SignatureKey::RsaPkcs { key_bits })
            }
        }
        other => bail!("unsupported subjectPublicKeyInfo algorithm OID: {other}"),
    }
}

fn parse_rsa_key_size_bits(bit_string_bytes: &[u8]) -> Result<u32> {
    let sequence = parse_tlv(bit_string_bytes, 0).context("invalid RSA public key sequence")?;
    let modulus =
        parse_tlv(bit_string_bytes, sequence.content_start).context("missing RSA modulus")?;
    if modulus.tag != 0x02 {
        bail!("invalid RSA modulus tag: {}", modulus.tag);
    }
    let modulus_bytes =
        &bit_string_bytes[modulus.content_start..modulus.content_start + modulus.content_length];
    let modulus_bytes = if modulus_bytes.first() == Some(&0) {
        &modulus_bytes[1..]
    } else {
        modulus_bytes
    };
    Ok((modulus_bytes.len() * 8) as u32)
}

fn extract_mrz_from_dg1(dg1: &[u8]) -> Result<String> {
    for index in 0..dg1.len().saturating_sub(2) {
        if dg1[index] == 0x5f && dg1[index + 1] == 0x1f {
            let len = dg1[index + 2] as usize;
            let start = index + 3;
            if start + len <= dg1.len() {
                return String::from_utf8(dg1[start..start + len].to_vec())
                    .context("DG1 MRZ is not valid UTF-8/ASCII");
            }
        }
    }
    String::from_utf8(dg1.to_vec()).context("DG1 fallback ASCII decode failed")
}

fn parse_mrz(mrz: &str) -> MrzFields {
    let clean = mrz.replace('\n', "").replace(' ', "");
    if clean.len() >= 88 && clean.starts_with('P') {
        let names = clean[5..44].split("<<").collect::<Vec<_>>();
        MrzFields {
            issuing_country: clean[2..5].replace('<', ""),
            document_number: clean[44..53].replace(['<', '_'], ""),
            nationality: clean[54..57].replace('<', ""),
            date_of_birth: clean[57..63].replace('<', ""),
            gender: clean[64..65].replace('<', ""),
            expiry_date: clean[65..71].replace(['<', '_'], ""),
            last_name: names
                .first()
                .copied()
                .unwrap_or_default()
                .replace('<', " ")
                .trim()
                .to_string(),
            first_name: names
                .get(1)
                .copied()
                .unwrap_or_default()
                .replace('<', " ")
                .trim()
                .to_string(),
        }
    } else {
        let names = clean
            .get(60..90)
            .unwrap_or_default()
            .split("<<")
            .collect::<Vec<_>>();
        MrzFields {
            issuing_country: clean.get(2..5).unwrap_or_default().replace('<', ""),
            document_number: clean.get(5..14).unwrap_or_default().replace(['<', '_'], ""),
            nationality: clean.get(45..48).unwrap_or_default().replace('<', ""),
            date_of_birth: clean.get(30..36).unwrap_or_default().replace('<', ""),
            gender: clean.get(37..38).unwrap_or_default().replace('<', ""),
            expiry_date: clean
                .get(38..44)
                .unwrap_or_default()
                .replace(['<', '_'], ""),
            last_name: names
                .first()
                .copied()
                .unwrap_or_default()
                .replace('<', " ")
                .trim()
                .to_string(),
            first_name: names
                .get(1)
                .copied()
                .unwrap_or_default()
                .replace('<', " ")
                .trim()
                .to_string(),
        }
    }
}

struct ParsedSodEnvelope {
    certificate_der: Vec<u8>,
    certificate_signature_algorithm_oid: String,
    digest_algorithm_oid: String,
    signer_signature_algorithm_oid: String,
    econtent_hash_algorithm_oid: String,
    econtent: Vec<u8>,
}

fn parse_sod_envelope(sod: &[u8]) -> Result<ParsedSodEnvelope> {
    let mut start_offset = 0usize;
    if sod.first() == Some(&0x77) {
        let wrapper = parse_tlv(sod, 0).context("invalid SOD wrapper")?;
        start_offset = wrapper.content_start;
    }

    let outer = parse_tlv(sod, start_offset).context("invalid SOD outer sequence")?;
    let mut pos = outer.content_start;
    let outer_end = outer.content_start + outer.content_length;
    while pos < outer_end {
        let tlv = parse_tlv(sod, pos).context("invalid SOD outer child")?;
        if tlv.tag == 0xa0 {
            let signed_data = parse_tlv(sod, tlv.content_start).context("invalid SignedData")?;
            return parse_signed_data(sod, signed_data.content_start, signed_data.content_length);
        }
        pos = tlv.content_start + tlv.content_length;
    }
    bail!("SignedData payload not found in SOD")
}

fn parse_signed_data(buf: &[u8], start: usize, len: usize) -> Result<ParsedSodEnvelope> {
    let mut pos = start;
    let end = start + len;
    let mut field_index = 0usize;
    let mut digest_algorithm_oid = None;
    let mut certificate_der = None;
    let mut certificate_signature_algorithm_oid = None;
    let mut signer_signature_algorithm_oid = None;
    let mut econtent = None;
    let mut econtent_hash_algorithm_oid = None;

    while pos < end {
        let tlv = parse_tlv(buf, pos).context("invalid SignedData child")?;
        if field_index == 1 && tlv.tag == 0x31 {
            let algo_seq =
                parse_tlv(buf, tlv.content_start).context("invalid digest algorithm set")?;
            let oid =
                parse_tlv(buf, algo_seq.content_start).context("invalid digest algorithm oid")?;
            digest_algorithm_oid = Some(parse_oid_raw(
                &buf[oid.content_start..oid.content_start + oid.content_length],
            ));
        }
        if field_index == 2 && tlv.tag == 0x30 {
            let (hash_oid, content) =
                parse_econtent_info(buf, tlv.content_start, tlv.content_length)?;
            econtent_hash_algorithm_oid = Some(hash_oid);
            econtent = Some(content);
        }
        if tlv.tag == 0xa0 && field_index >= 3 {
            let cert_seq =
                parse_tlv(buf, tlv.content_start).context("invalid certificate container")?;
            certificate_der = Some(
                buf[cert_seq.offset..cert_seq.content_start + cert_seq.content_length].to_vec(),
            );
            let (_, parsed_cert) = parse_x509_certificate(certificate_der.as_ref().unwrap())
                .map_err(|error| {
                    anyhow::anyhow!("failed to parse embedded certificate: {error}")
                })?;
            certificate_signature_algorithm_oid =
                Some(parsed_cert.signature_algorithm.algorithm.to_id_string());
        }
        if tlv.tag == 0x31 && field_index >= 4 {
            let signer_info = parse_tlv(buf, tlv.content_start).context("invalid signer info")?;
            signer_signature_algorithm_oid = Some(parse_signer_info_signature_algorithm(
                buf,
                signer_info.content_start,
                signer_info.content_length,
            )?);
        }
        pos = tlv.content_start + tlv.content_length;
        field_index += 1;
    }

    Ok(ParsedSodEnvelope {
        certificate_der: certificate_der.context("certificate not found in SOD")?,
        certificate_signature_algorithm_oid: certificate_signature_algorithm_oid
            .context("certificate signature algorithm not found")?,
        digest_algorithm_oid: digest_algorithm_oid.context("digest algorithm not found")?,
        signer_signature_algorithm_oid: signer_signature_algorithm_oid
            .context("signer signature algorithm not found")?,
        econtent_hash_algorithm_oid: econtent_hash_algorithm_oid
            .context("eContent hash algorithm not found")?,
        econtent: econtent.context("eContent not found")?,
    })
}

fn parse_econtent_info(buf: &[u8], start: usize, len: usize) -> Result<(String, Vec<u8>)> {
    let end = start + len;
    let mut pos = start;
    let mut octet_string = None;
    while pos < end {
        let tlv = parse_tlv(buf, pos).context("invalid EncapContentInfo child")?;
        if tlv.tag == 0xa0 {
            let octet =
                parse_tlv(buf, tlv.content_start).context("invalid eContent OCTET STRING")?;
            octet_string =
                Some(buf[octet.content_start..octet.content_start + octet.content_length].to_vec());
            break;
        }
        pos = tlv.content_start + tlv.content_length;
    }
    let econtent = octet_string.context("missing eContent OCTET STRING")?;
    let hash_oid = parse_lds_hash_algorithm_oid(&econtent)?;
    Ok((hash_oid, econtent))
}

fn parse_lds_hash_algorithm_oid(econtent: &[u8]) -> Result<String> {
    let root = parse_tlv(econtent, 0).context("invalid LDS security object")?;
    let version = parse_tlv(econtent, root.content_start).context("invalid LDS version")?;
    let digest_algo = parse_tlv(econtent, version.content_start + version.content_length)
        .context("invalid LDS digest algorithm")?;
    let oid = parse_tlv(econtent, digest_algo.content_start).context("invalid LDS digest OID")?;
    Ok(parse_oid_raw(
        &econtent[oid.content_start..oid.content_start + oid.content_length],
    ))
}

fn parse_signer_info_signature_algorithm(buf: &[u8], start: usize, len: usize) -> Result<String> {
    let mut pos = start;
    let end = start + len;
    let mut field_index = 0usize;
    while pos < end {
        let tlv = parse_tlv(buf, pos).context("invalid SignerInfo child")?;
        if tlv.tag == 0x30 && field_index >= 3 {
            let oid =
                parse_tlv(buf, tlv.content_start).context("invalid SignerInfo signature OID")?;
            return Ok(parse_oid_raw(
                &buf[oid.content_start..oid.content_start + oid.content_length],
            ));
        }
        pos = tlv.content_start + tlv.content_length;
        field_index += 1;
    }
    bail!("SignerInfo signature algorithm not found")
}

fn parse_oid_bytes(bytes: &[u8]) -> Result<String> {
    let tlv = parse_tlv(bytes, 0).context("invalid OID TLV")?;
    if tlv.tag != 0x06 {
        bail!("expected OID tag, got {}", tlv.tag);
    }
    Ok(parse_oid_raw(
        &bytes[tlv.content_start..tlv.content_start + tlv.content_length],
    ))
}

fn parse_oid_raw(bytes: &[u8]) -> String {
    if bytes.is_empty() {
        return String::new();
    }
    let mut parts = vec![(bytes[0] / 40) as u64, (bytes[0] % 40) as u64];
    let mut value = 0u64;
    for byte in &bytes[1..] {
        value = (value << 7) | (u64::from(*byte) & 0x7f);
        if byte & 0x80 == 0 {
            parts.push(value);
            value = 0;
        }
    }
    parts
        .into_iter()
        .map(|part| part.to_string())
        .collect::<Vec<_>>()
        .join(".")
}

fn extract_country_from_name(name: &str) -> Option<String> {
    for fragment in name.split([',', '/']) {
        let fragment = fragment.trim();
        if let Some(value) = fragment.strip_prefix("countryName=") {
            return Some(value.trim().to_string());
        }
        if let Some(value) = fragment.strip_prefix("C=") {
            return Some(value.trim().to_string());
        }
    }
    None
}

fn hex_lower(bytes: &[u8]) -> String {
    bytes
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect::<String>()
}

#[derive(Debug, Clone, Copy)]
struct Tlv {
    offset: usize,
    tag: u8,
    content_start: usize,
    content_length: usize,
}

fn parse_tlv(buf: &[u8], offset: usize) -> Option<Tlv> {
    if offset >= buf.len() {
        return None;
    }
    let tag = buf[offset];
    let mut pos = offset + 1;
    if pos >= buf.len() {
        return None;
    }

    let mut length = usize::from(buf[pos]);
    pos += 1;
    if length == 0x80 {
        return None;
    }
    if length > 0x80 {
        let num_bytes = length & 0x7f;
        length = 0;
        for _ in 0..num_bytes {
            if pos >= buf.len() {
                return None;
            }
            length = (length << 8) | usize::from(buf[pos]);
            pos += 1;
        }
    }
    Some(Tlv {
        offset,
        tag,
        content_start: pos,
        content_length: length,
    })
}

#[cfg(test)]
mod tests {
    use super::analyze_document;
    use base64::Engine;
    use prover_types::DocumentInput;

    const JOHN_SOD_B64: &str = "MIIFqgYJKoZIhvcNAQcCoIIFmzCCBZcCAQMxDTALBglghkgBZQMEAgEwbgYGZ4EIAQEBoGQEYjBgAgEAMAsGCWCGSAFlAwQCATBOMCUCAQEEINwYjAVVVMdLC2MhUGDqpyWacq6GdVwFRVjlyf20LD48MCUCAQIEIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAoIIDXjCCA1owggJCoAMCAQICAQIwDQYJKoZIhvcNAQELBQAwNDElMCMGA1UEAxMcWmVybyBLbm93bGVkZ2UgUmVwdWJsaWMgQ1NDQTELMAkGA1UEBhMCWkswHhcNMjUwNDI5MTQzOTA4WhcNMzUwNDI3MTQzOTA4WjAzMSQwIgYDVQQDExtaZXJvIEtub3dsZWRnZSBSZXB1YmxpYyBEU0MxCzAJBgNVBAYTAlpLMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAt/Luj2Qk1gfZAdi+2IJ+4rIZ+A2sQADkFUCHn3tEmcEi6Y9v4vKVPJVzZYGkiOyzGvXcaD57Z4aFIaKpd+3hbrxmU6XwZmPplOw/23eYSIcagu8T+Ha0+wyvNGft/x2ulwva2PnXnjltjw+hvsgKpQ6VIGiuGCyjhM/46qzOGcipHxWHVKaoxio5kp2EntFIhPOHCfv9Vr0DM6qZ/KrohiDkJPOPqNN/QwElRT/FEaf2vNYUUDUVgVQAbIvnG/kWIxvjJXDblJ6zMyUtkUV05KEFDSp6jg4EXRZGq85FoaE+8KCMhZpA9Pzt+qgCZ8X7mqy9dV2n7TPZSuGy1Lzb5wIDAQABo3gwdjAMBgNVHRMBAf8EAjAAMA4GA1UdDwEB/wQEAwIHgDApBgNVHQ4EIgQg20wj6jFXBKwGcf1ZWqHyo/4ZqpcojRktjk07TfkFCzIwKwYDVR0jBCQwIoAgVAb5SxCtfn9vlu/RlDRXTHI1FT5VnX7t3vamnvum77AwDQYJKoZIhvcNAQELBQADggEBAGWAC32nLWbsZg/bajGat4APluOMIw5O6AOyGJ6e4XL+0XCjtOQg0u4c93lMxvBc3RwD+8dMIjxQhhaCqTSy3jSyZE35Iw1Vj6/bdXdBctWD0wN5w22dtzdttXyfYoqgipT10bvIX1nzje0JROQniRo7sZqRGy6NLFH9LG/UbIb776thIpiBzwZh65t5gNpbuITvvoy6h1sD0Ud6+uNykHC02212L6eSZVJ4pbfQUQ/BthNpS1GftdvTU0oulPvMTDDLPhnloQdOjEUDMPNpl62+LjqDwXyNofUunpTLSG4ipjUn93XmOB5o1nWNArtyOrCym+NW3LMRWRTTc/cjLJAxggGvMIIBqwIBAYAgAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAwCwYJYIZIAWUDBAIBoGYwFQYJKoZIhvcNAQkDMQgGBmeBCAEBATAcBgkqhkiG9w0BCQUxDxcNMjQwNTAxMDAwMDAwWjAvBgkqhkiG9w0BCQQxIgQgIgDt2Ta5MvMS040cP4+zc7dkfvebBND7ZOJ9HQY9Wy4wCwYJKoZIhvcNAQELBIIBAKpNU6LTvFPCluRMeHx0ufPHaOyNgdbsOOfVBMWmSdklKpO6cSJSblMt5Pl912gre+XWH1z3mrmRykaf5zY7QhaUv2TODG5RRhXtFrCTCSamuj5pUB7gVrFPpHcSDfpFN2qDBHGcTS+YlLt+g0vAqG5I63P1fxhhrRMFeXiMOniDU8iobSUqYabVt8GQcVhHHz2H9I6YJNkn9BFOo/lGj0kPKTVwgtBaQcD0FQu7oQibuSES03OulpBoWUd94UIoYO3iBu++NTGoyBPeTdsK5DZdtIWjCkhVBLCov2v5By8GsZXLgyGRWkdbdy6IhKv9oOY367bLhBnt4IW0CC4lf/Y=";
    const JOHN_MRZ: &str =
        "P<ZKRSMITH<<JOHN<MILLER<<<<<<<<<<<<<<<<<<<<<ZP1111111_ZKR951112_M350101_<<<<<<<<<<<<<<<<";

    #[test]
    fn analyzes_debug_fixture_document() {
        let dg1_bytes = [
            vec![0x61, 0x5b, 0x5f, 0x1f, 0x58],
            JOHN_MRZ.as_bytes().to_vec(),
        ]
        .concat();
        let document = DocumentInput {
            dg1_base64: base64::engine::general_purpose::STANDARD.encode(dg1_bytes),
            sod_der_base64: JOHN_SOD_B64.to_string(),
            dg2_base64: None,
            document_type: Some("passport".to_string()),
        };
        let analysis = analyze_document(&document).expect("analysis");
        assert_eq!(analysis.document_type, "passport");
        assert_eq!(analysis.mrz_fields.nationality, "ZKR");
        assert_eq!(analysis.tbs_bucket, 700);
        assert_eq!(analysis.dsc_parent_signature_algorithm, "RSA");
    }
}
