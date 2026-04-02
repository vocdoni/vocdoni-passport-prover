//! Native witness generation using Noir v1.0.0-beta.14 (bincode format).
//! Compatible with zkpassport circuits v0.16.0.

mod ffi;

use acvm::acir::circuit::Program;
use acvm::acir::native_types::WitnessStack;
use acvm::FieldElement;
use base64::Engine;
use bn254_blackbox_solver::Bn254BlackBoxSolver;
use jni::objects::JClass;
use jni::objects::JString;
use jni::sys::jbyteArray;
use jni::JNIEnv;
use nargo::foreign_calls::default::DefaultForeignCallBuilder;
use nargo::ops::execute_program;
use noirc_abi::input_parser::Format;
use noirc_abi::Abi;
use serde::Deserialize;
use thiserror::Error;

#[derive(Debug, Deserialize)]
struct WitnessPayload {
    bytecode: String,
    abi: Abi,
    #[serde(default)]
    inputs: serde_json::Value,
}

#[derive(Debug, Error)]
enum SolveError {
    #[error("JSON: {0}")]
    Json(#[from] serde_json::Error),
    #[error("Base64: {0}")]
    Base64(#[from] base64::DecodeError),
    #[error("Input parse: {0}")]
    InputParse(String),
    #[error("ABI encode: {0}")]
    AbiEncode(String),
    #[error("Program deserialize: {0}")]
    Deserialize(String),
    #[error("ACVM: {0}")]
    Acvm(String),
    #[error("Witness serialize: {0}")]
    Serialize(String),
}

/// Compressed witness bytes, suitable for Barretenberg.
pub fn solve_compressed_witness_from_json_utf8(json: &str) -> Result<Vec<u8>, String> {
    solve_inner(json).map_err(|e| e.to_string())
}

fn solve_inner(json: &str) -> Result<Vec<u8>, SolveError> {
    let payload: WitnessPayload = serde_json::from_str(json)?;
    let program_bytes = base64::engine::general_purpose::STANDARD.decode(&payload.bytecode)?;

    let inputs_str = serde_json::to_string(&payload.inputs)?;
    let input_map = Format::Json
        .parse(&inputs_str, &payload.abi)
        .map_err(|e| SolveError::InputParse(e.to_string()))?;

    let witness_map = payload
        .abi
        .encode(&input_map, None)
        .map_err(|e| SolveError::AbiEncode(e.to_string()))?;

    let program: Program<FieldElement> = Program::deserialize_program(&program_bytes)
        .map_err(|e| SolveError::Deserialize(format!("Program::deserialize_program failed: {}", e)))?;

    #[cfg(debug_assertions)]
    {
        eprintln!(
            "[acvm] program: {} ACIR functions, {} unconstrained functions",
            program.functions.len(),
            program.unconstrained_functions.len()
        );
        if !program.functions.is_empty() {
            eprintln!(
                "[acvm] main circuit: {} opcodes",
                program.functions[0].opcodes.len()
            );
        }
    }

    // Execute the program using nargo's execute_program (same as noir_rs)
    let blackbox_solver = Bn254BlackBoxSolver::default();
    let mut foreign_call_executor = DefaultForeignCallBuilder::default().build();

    let witness_stack: WitnessStack<FieldElement> =
        execute_program(&program, witness_map, &blackbox_solver, &mut foreign_call_executor)
            .map_err(|e| SolveError::Acvm(e.to_string()))?;

    // Serialize witness stack and decompress for Barretenberg
    // WitnessStack::serialize() returns gzip-compressed data, but bb expects raw bytes
    let compressed = witness_stack
        .serialize()
        .map_err(|e| SolveError::Serialize(e.to_string()))?;
    
    // Decompress to get raw bytes for barretenberg (same as noir_rs)
    use flate2::read::GzDecoder;
    use std::io::Read;
    let mut decoder = GzDecoder::new(compressed.as_slice());
    let mut decompressed = Vec::new();
    decoder.read_to_end(&mut decompressed)
        .map_err(|e| SolveError::Serialize(format!("gzip decompress: {}", e)))?;

    Ok(decompressed)
}

/// JNI: `byte[] nativeSolveFromJsonUtf8(String jsonUtf8)` — returns compressed witness bytes.
#[no_mangle]
pub extern "system" fn Java_com_vocdonipassport_acvm_AcvmWitnessNative_nativeSolveFromJsonUtf8(
    mut env: JNIEnv,
    _class: JClass,
    json: JString,
) -> jbyteArray {
    let json_str: String = match env.get_string(&json) {
        Ok(s) => s.into(),
        Err(e) => {
            env.throw_new("java/lang/RuntimeException", format!("JNI get_string: {e}"))
                .ok();
            return std::ptr::null_mut();
        }
    };

    let bytes = match solve_inner(&json_str) {
        Ok(b) => b,
        Err(e) => {
            env.throw_new("java/lang/RuntimeException", e.to_string()).ok();
            return std::ptr::null_mut();
        }
    };

    let out = match env.byte_array_from_slice(&bytes) {
        Ok(a) => a,
        Err(e) => {
            env.throw_new("java/lang/RuntimeException", format!("byte_array_from_slice: {e}"))
                .ok();
            return std::ptr::null_mut();
        }
    };
    out.into_raw()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rejects_invalid_json() {
        let r = solve_compressed_witness_from_json_utf8("not json");
        assert!(r.is_err());
    }

    #[test]
    fn rejects_empty_object() {
        let r = solve_compressed_witness_from_json_utf8("{}");
        assert!(r.is_err());
    }
}
