use crate::registry::PackagedCircuit;
use acvm::acir::circuit::Program;
use acvm::acir::native_types::WitnessStack;
use acvm::FieldElement;
use anyhow::{Context, Result};
use base64::Engine;
use bn254_blackbox_solver::Bn254BlackBoxSolver;
use nargo::foreign_calls::default::DefaultForeignCallBuilder;
use nargo::ops::execute_program;
use noirc_abi::input_parser::Format;
use noirc_abi::Abi;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WitnessPayload {
    pub bytecode: String,
    pub abi: serde_json::Value,
    #[serde(default)]
    pub inputs: serde_json::Value,
}

fn solve_compressed_witness_with_format(
    payload: &WitnessPayload,
    serialization_format: Option<&str>,
) -> Result<Vec<u8>> {
    let abi: Abi =
        serde_json::from_value(payload.abi.clone()).context("failed to parse Noir ABI")?;
    let program_bytes = base64::engine::general_purpose::STANDARD
        .decode(&payload.bytecode)
        .context("failed to decode circuit bytecode")?;

    let inputs_str =
        serde_json::to_string(&payload.inputs).context("failed to serialize inputs")?;
    let input_map = Format::Json
        .parse(&inputs_str, &abi)
        .map_err(|error| anyhow::anyhow!("input parse failed: {error}"))?;

    let witness_map = abi
        .encode(&input_map, None)
        .map_err(|error| anyhow::anyhow!("ABI encode failed: {error}"))?;

    let program: Program<FieldElement> = Program::deserialize_program(&program_bytes)
        .map_err(|error| anyhow::anyhow!("Program::deserialize_program failed: {error}"))?;

    let blackbox_solver = Bn254BlackBoxSolver::default();
    let mut foreign_call_executor = DefaultForeignCallBuilder::default().build();

    let witness_stack: WitnessStack<FieldElement> = execute_program(
        &program,
        witness_map,
        &blackbox_solver,
        &mut foreign_call_executor,
    )
    .map_err(|error| anyhow::anyhow!("ACVM execution failed: {error}"))?;

    match serialization_format {
        Some(format) => std::env::set_var("NOIR_SERIALIZATION_FORMAT", format),
        None => std::env::remove_var("NOIR_SERIALIZATION_FORMAT"),
    }
    witness_stack
        .serialize()
        .map_err(|error| anyhow::anyhow!("witness serialization failed: {error}"))
}

fn decompress_witness_stack(serialized: &[u8]) -> Result<Vec<u8>> {
    let mut decoder = flate2::read::GzDecoder::new(serialized);
    let mut decompressed = Vec::new();
    std::io::Read::read_to_end(&mut decoder, &mut decompressed)
        .map_err(|error| anyhow::anyhow!("witness gzip decompression failed: {error}"))?;
    Ok(decompressed)
}

pub fn solve_serialized_witness(payload: &WitnessPayload) -> Result<Vec<u8>> {
    solve_compressed_witness_with_format(payload, Some("msgpack-compact"))
}

pub fn solve_serialized_legacy_witness(payload: &WitnessPayload) -> Result<Vec<u8>> {
    solve_compressed_witness_with_format(payload, None)
}

pub fn solve_compressed_witness(payload: &WitnessPayload) -> Result<Vec<u8>> {
    let serialized = solve_serialized_witness(payload)?;
    decompress_witness_stack(&serialized)
}

pub fn solve_legacy_witness(payload: &WitnessPayload) -> Result<Vec<u8>> {
    let serialized = solve_serialized_legacy_witness(payload)?;
    decompress_witness_stack(&serialized)
}

pub fn solve_serialized_witness_for_circuit(
    circuit: &PackagedCircuit,
    inputs: serde_json::Value,
) -> Result<Vec<u8>> {
    let payload = WitnessPayload {
        bytecode: circuit.bytecode.clone(),
        abi: circuit.abi.clone(),
        inputs,
    };
    solve_serialized_witness(&payload)
}

pub fn solve_serialized_legacy_witness_for_circuit(
    circuit: &PackagedCircuit,
    inputs: serde_json::Value,
) -> Result<Vec<u8>> {
    let payload = WitnessPayload {
        bytecode: circuit.bytecode.clone(),
        abi: circuit.abi.clone(),
        inputs,
    };
    solve_serialized_legacy_witness(&payload)
}

pub fn solve_compressed_witness_for_circuit(
    circuit: &PackagedCircuit,
    inputs: serde_json::Value,
) -> Result<Vec<u8>> {
    let payload = WitnessPayload {
        bytecode: circuit.bytecode.clone(),
        abi: circuit.abi.clone(),
        inputs,
    };
    solve_compressed_witness(&payload)
}

pub fn solve_legacy_witness_for_circuit(
    circuit: &PackagedCircuit,
    inputs: serde_json::Value,
) -> Result<Vec<u8>> {
    let payload = WitnessPayload {
        bytecode: circuit.bytecode.clone(),
        abi: circuit.abi.clone(),
        inputs,
    };
    solve_legacy_witness(&payload)
}

#[cfg(test)]
mod tests {
    use super::{solve_compressed_witness, WitnessPayload};

    #[test]
    fn rejects_invalid_circuit_payload() {
        let payload = WitnessPayload {
            bytecode: "not-base64".to_string(),
            abi: serde_json::json!({}),
            inputs: serde_json::json!({}),
        };
        assert!(solve_compressed_witness(&payload).is_err());
    }
}
