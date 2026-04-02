use acvm::acir::{circuit::Program, FieldElement};
use anyhow::{Context, Result};
use base64::engine::{general_purpose, Engine};
use flate2::bufread::GzDecoder;
use std::io::Read;

pub fn get_acir_buffer(circuit_bytecode: &str) -> Result<Vec<u8>> {
    general_purpose::STANDARD
        .decode(circuit_bytecode)
        .context("failed to decode circuit bytecode from base64")
}

pub fn uncompress_acir_buffer(acir_buffer: Vec<u8>) -> Result<Vec<u8>> {
    let mut decoder = GzDecoder::new(acir_buffer.as_slice());
    let mut acir_buffer_uncompressed = Vec::<u8>::new();
    decoder
        .read_to_end(&mut acir_buffer_uncompressed)
        .context("failed to decompress ACIR buffer")?;
    Ok(acir_buffer_uncompressed)
}

pub fn get_acir_buffer_uncompressed(circuit_bytecode: &str) -> Result<Vec<u8>> {
    let acir_buffer = get_acir_buffer(circuit_bytecode)?;
    let program: Program<FieldElement> = Program::deserialize_program(&acir_buffer)
        .map_err(|error| anyhow::anyhow!("failed to deserialize program: {error}"))?;
    // Barretenberg expects Noir's msgpack-compact serialization marker.
    std::env::set_var("NOIR_SERIALIZATION_FORMAT", "msgpack-compact");
    let reserialized = Program::serialize_program(&program);
    uncompress_acir_buffer(reserialized)
}
