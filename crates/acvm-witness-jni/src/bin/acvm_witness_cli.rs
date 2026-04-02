use std::{env, fs};

fn main() {
    let args: Vec<String> = env::args().collect();
    if args.len() != 2 {
        eprintln!("usage: acvm_witness_cli <payload.json>");
        std::process::exit(2);
    }
    let path = &args[1];
    let json = fs::read_to_string(path).unwrap_or_else(|e| {
        eprintln!("read {}: {}", path, e);
        std::process::exit(2);
    });

    match acvm_witness_jni::solve_compressed_witness_from_json_utf8(&json) {
        Ok(bytes) => {
            println!("ok: witness bytes={}", bytes.len());
        }
        Err(e) => {
            eprintln!("error: {}", e);
            std::process::exit(1);
        }
    }
}

