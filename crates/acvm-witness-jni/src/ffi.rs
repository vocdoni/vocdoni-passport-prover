//! C ABI for iOS (staticlib). Android uses JNI in `lib.rs`.

use std::ffi::{c_char, CString};
use std::slice;

use crate::solve_compressed_witness_from_json_utf8;

/// Must match `acvm_witness_ffi.h`.
#[repr(C)]
pub struct AcvmWitnessBuffer {
    pub ptr: *mut u8,
    pub len: usize,
}

#[repr(C)]
pub struct AcvmWitnessFfiResult {
    /// 0 = success (`data` valid), non-zero = error (`error_utf8` valid).
    pub status: u32,
    pub data: AcvmWitnessBuffer,
    pub error_utf8: *mut c_char,
}

fn ok(bytes: Vec<u8>) -> AcvmWitnessFfiResult {
    let mut buf = bytes.into_boxed_slice();
    let len = buf.len();
    let ptr = buf.as_mut_ptr();
    std::mem::forget(buf);
    AcvmWitnessFfiResult {
        status: 0,
        data: AcvmWitnessBuffer { ptr, len },
        error_utf8: std::ptr::null_mut(),
    }
}

fn err(msg: String) -> AcvmWitnessFfiResult {
    let c = CString::new(msg).unwrap_or_else(|_| CString::new("invalid error string").unwrap());
    AcvmWitnessFfiResult {
        status: 1,
        data: AcvmWitnessBuffer {
            ptr: std::ptr::null_mut(),
            len: 0,
        },
        error_utf8: c.into_raw(),
    }
}

/// Solve witness from UTF-8 JSON payload (same shape as Android JNI). Caller must free with `acvm_witness_free_ffi_result`.
#[no_mangle]
pub unsafe extern "C" fn acvm_witness_solve_json_utf8(
    json_ptr: *const u8,
    json_len: usize,
) -> AcvmWitnessFfiResult {
    if json_ptr.is_null() || json_len == 0 {
        return err("empty or null JSON input".into());
    }
    let json = match std::str::from_utf8(slice::from_raw_parts(json_ptr, json_len)) {
        Ok(s) => s,
        Err(_) => return err("invalid UTF-8".into()),
    };
    match solve_compressed_witness_from_json_utf8(json) {
        Ok(v) => ok(v),
        Err(e) => err(e),
    }
}

/// Releases buffers returned from `acvm_witness_solve_json_utf8`.
#[no_mangle]
pub unsafe extern "C" fn acvm_witness_free_ffi_result(r: AcvmWitnessFfiResult) {
    if !r.data.ptr.is_null() && r.data.len > 0 {
        drop(Vec::from_raw_parts(r.data.ptr, r.data.len, r.data.len));
    }
    if !r.error_utf8.is_null() {
        drop(CString::from_raw(r.error_utf8));
    }
}
