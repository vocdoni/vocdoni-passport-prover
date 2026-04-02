use anyhow::{bail, Result};
use prover_types::FixtureBundle;
use serde::{Deserialize, Serialize};
use serde_json::{json, Map, Value};

const DEFAULT_QUERY_FIELD: &str = "nationality";
const MAX_DISCLOSURE_PROOFS: usize = 10;
const SUPPORTED_QUERY_KEYS: &[&str] = &[
    "age",
    "nationality",
    "issuing_country",
    "firstname",
    "lastname",
    "fullname",
    "birthdate",
    "expiry_date",
    "document_number",
    "document_type",
    "gender",
];

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct PlannedCircuit {
    pub name: String,
    pub purpose: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ProofPlan {
    pub circuit_version: String,
    pub normalized_query: Value,
    pub disclosure_count: usize,
    pub outer_circuit: String,
    pub resolved_circuits: Vec<PlannedCircuit>,
    pub unresolved_requirements: Vec<String>,
}

impl ProofPlan {
    pub fn required_circuit_names(&self) -> Vec<String> {
        self.resolved_circuits
            .iter()
            .map(|circuit| circuit.name.clone())
            .collect()
    }
}

pub fn build_proof_plan(fixture: &FixtureBundle) -> Result<ProofPlan> {
    fixture.validate()?;

    let normalized_query = normalize_query(&fixture.request.query)?;
    let mut resolved_circuits = Vec::new();

    if has_disclose_bytes_requirement(&normalized_query) {
        resolved_circuits.push(PlannedCircuit {
            name: "disclose_bytes_evm".to_string(),
            purpose: "selective disclosure and parameter commitment".to_string(),
        });
    }

    if has_age_requirement(&normalized_query) {
        resolved_circuits.push(PlannedCircuit {
            name: "compare_age_evm".to_string(),
            purpose: "age comparison".to_string(),
        });
    }

    if has_nested_key(&normalized_query, "nationality", "in") {
        resolved_circuits.push(PlannedCircuit {
            name: "inclusion_check_nationality_evm".to_string(),
            purpose: "nationality inclusion set membership".to_string(),
        });
    }

    if has_nested_key(&normalized_query, "nationality", "out") {
        resolved_circuits.push(PlannedCircuit {
            name: "exclusion_check_nationality_evm".to_string(),
            purpose: "nationality exclusion set membership".to_string(),
        });
    }

    if has_nested_key(&normalized_query, "issuing_country", "in") {
        resolved_circuits.push(PlannedCircuit {
            name: "inclusion_check_issuing_country_evm".to_string(),
            purpose: "issuing country inclusion set membership".to_string(),
        });
    }

    if has_nested_key(&normalized_query, "issuing_country", "out") {
        resolved_circuits.push(PlannedCircuit {
            name: "exclusion_check_issuing_country_evm".to_string(),
            purpose: "issuing country exclusion set membership".to_string(),
        });
    }

    if resolved_circuits.is_empty() {
        bail!("request query produced no supported disclosure circuits");
    }
    if resolved_circuits.len() > MAX_DISCLOSURE_PROOFS {
        bail!(
            "too many disclosure proofs requested: {} (max {})",
            resolved_circuits.len(),
            MAX_DISCLOSURE_PROOFS
        );
    }

    let outer_circuit = format!("outer_evm_count_{}", 3 + resolved_circuits.len());
    resolved_circuits.push(PlannedCircuit {
        name: outer_circuit.clone(),
        purpose: "outer recursive aggregation and EVM verification".to_string(),
    });

    Ok(ProofPlan {
        circuit_version: fixture.request.circuit_version.clone(),
        normalized_query,
        disclosure_count: resolved_circuits.len() - 1,
        outer_circuit,
        resolved_circuits,
        unresolved_requirements: vec![
            "dsc_signature_circuit (requires SOD certificate analysis)".to_string(),
            "id_data_signature_circuit (requires SOD signature algorithm analysis)".to_string(),
            "integrity_circuit (requires LDS hash algorithm analysis)".to_string(),
        ],
    })
}

fn normalize_query(query: &Value) -> Result<Value> {
    match query {
        Value::Null => Ok(json!({
            DEFAULT_QUERY_FIELD: { "disclose": true }
        })),
        Value::Object(map) if map.is_empty() => Ok(json!({
            DEFAULT_QUERY_FIELD: { "disclose": true }
        })),
        Value::Object(map) => {
            validate_supported_query_keys(map)?;
            Ok(Value::Object(map.clone()))
        }
        _ => bail!("query must be a JSON object"),
    }
}

fn validate_supported_query_keys(query: &Map<String, Value>) -> Result<()> {
    for key in query.keys() {
        if !SUPPORTED_QUERY_KEYS.contains(&key.as_str()) {
            bail!("unsupported request query key: {key}");
        }
    }
    Ok(())
}

fn has_disclose_bytes_requirement(query: &Value) -> bool {
    query
        .as_object()
        .map(|fields| {
            fields.values().any(|cfg| {
                cfg.as_object().is_some_and(|cfg| {
                    cfg.get("disclose")
                        .and_then(Value::as_bool)
                        .unwrap_or(false)
                        || cfg.contains_key("eq")
                })
            })
        })
        .unwrap_or(false)
}

fn has_age_requirement(query: &Value) -> bool {
    query
        .as_object()
        .and_then(|fields| fields.get("age"))
        .and_then(Value::as_object)
        .is_some_and(|cfg| {
            ["gte", "gt", "lte", "lt", "range", "eq", "disclose"]
                .iter()
                .any(|key| cfg.contains_key(*key))
        })
}

fn has_nested_key(query: &Value, field: &str, key: &str) -> bool {
    query
        .as_object()
        .and_then(|fields| fields.get(field))
        .and_then(Value::as_object)
        .is_some_and(|cfg| cfg.contains_key(key))
}

#[cfg(test)]
mod tests {
    use super::build_proof_plan;
    use prover_types::{DocumentInput, FixtureBundle, FixtureMetadata, ProofRequest};
    use serde_json::json;

    fn fixture(query: serde_json::Value) -> FixtureBundle {
        FixtureBundle {
            document: DocumentInput {
                dg1_base64: "ZGc=".to_string(),
                sod_der_base64: "c29k".to_string(),
                dg2_base64: None,
                document_type: Some("passport".to_string()),
            },
            request: ProofRequest {
                circuit_version: "0.16.0".to_string(),
                chain_id: 11155111,
                service_scope: "vocdoni-passport".to_string(),
                service_subscope: "petition".to_string(),
                current_date: 1,
                query,
                petition_id: None,
                aggregate_mode: None,
            },
            expected_inner: None,
            expected_outer: None,
            metadata: FixtureMetadata::default(),
        }
    }

    #[test]
    fn defaults_to_nationality_disclosure() {
        let plan = build_proof_plan(&fixture(json!({}))).expect("plan");
        assert_eq!(plan.disclosure_count, 1);
        assert_eq!(plan.outer_circuit, "outer_evm_count_4");
        assert_eq!(plan.resolved_circuits[0].name, "disclose_bytes_evm");
    }

    #[test]
    fn builds_multiple_disclosure_circuits() {
        let plan = build_proof_plan(&fixture(json!({
            "age": { "gte": 18 },
            "nationality": { "in": ["ESP", "FRA"] },
            "issuing_country": { "out": ["RUS"] },
            "firstname": { "disclose": true }
        })))
        .expect("plan");
        let names = plan.required_circuit_names();
        assert_eq!(
            names,
            vec![
                "disclose_bytes_evm",
                "compare_age_evm",
                "inclusion_check_nationality_evm",
                "exclusion_check_issuing_country_evm",
                "outer_evm_count_7"
            ]
        );
    }

    #[test]
    fn rejects_unsupported_query_key() {
        let error = build_proof_plan(&fixture(json!({
            "facematch": { "mode": "strict" }
        })))
        .expect_err("unsupported key should fail");
        assert!(error.to_string().contains("unsupported request query key"));
    }
}
