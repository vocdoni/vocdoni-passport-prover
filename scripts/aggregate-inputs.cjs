#!/usr/bin/env node

const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..', '..');

function loadUtils() {
  const candidates = [
    process.env.VOCDONI_ZKPASSPORT_UTILS_DIST,
    path.join(ROOT, 'zkpassport-packages', 'packages', 'zkpassport-utils', 'dist', 'cjs', 'index.cjs'),
    '/opt/vocdoni/repos/zkpassport-packages/packages/zkpassport-utils/dist/cjs/index.cjs',
  ].filter(Boolean);

  for (const candidate of candidates) {
    if (fs.existsSync(candidate)) {
      return require(candidate);
    }
  }

  throw new Error('zkpassport-utils dist not found; set VOCDONI_ZKPASSPORT_UTILS_DIST');
}

const UTILS = loadUtils();

function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i += 1) {
    const key = argv[i];
    if (!key.startsWith('--')) continue;
    const value = argv[i + 1];
    out[key.slice(2)] = value;
    i += 1;
  }
  return out;
}

function readJson(filePath) {
  return JSON.parse(fs.readFileSync(filePath, 'utf8'));
}

function normalizeProof(proof) {
  return {
    circuit_name: proof.circuitName || proof.circuit_name,
    proof: proof.proof || [],
    public_inputs: proof.publicInputs || proof.public_inputs || [],
    vkey: proof.vkey || [],
    key_hash: proof.keyHash || proof.key_hash || '',
    tree_hash_path: proof.treeHashPath || proof.tree_hash_path || [],
    tree_index: String(proof.treeIndex || proof.tree_index || '0'),
  };
}

function unwrapRecursiveProof(proof) {
  return {
    proof: proof.proof,
    publicInputs: proof.public_inputs,
    vkey: proof.vkey,
    keyHash: proof.key_hash,
    treeHashPath: proof.tree_hash_path,
    treeIndex: proof.tree_index,
  };
}

function normalizeCurrentDate(payload, disclosures) {
  if (payload.currentDate != null) return Number(payload.currentDate);
  if (payload.current_date != null) return Number(payload.current_date);
  if (disclosures.length > 0 && disclosures[0].public_inputs?.length > 1) {
    return Number(BigInt(disclosures[0].public_inputs[1]));
  }
  return 0;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const requestPath = args.request;
  const artifactsDir = args['artifacts-dir'];
  const outPath = args.out;

  if (!requestPath || !artifactsDir || !outPath) {
    throw new Error(
      'usage: aggregate-inputs.cjs --request <file> --artifacts-dir <dir> --out <file>',
    );
  }

  const payload = readJson(requestPath);
  const manifest = readJson(path.join(artifactsDir, 'manifest.json'));

  const dsc = normalizeProof(payload.dsc);
  const idData = normalizeProof(payload.idData || payload.id_data);
  const integrity = normalizeProof(payload.integrity);
  const disclosures = (payload.disclosures || []).map(normalizeProof);

  if (!dsc.circuit_name || !idData.circuit_name || !integrity.circuit_name) {
    throw new Error('aggregate request is missing one or more recursive inner proofs');
  }
  if (disclosures.length === 0) {
    throw new Error('aggregate request must include at least one disclosure proof');
  }

  const outerInputs = UTILS.getOuterCircuitInputs(
    unwrapRecursiveProof(dsc),
    unwrapRecursiveProof(idData),
    unwrapRecursiveProof(integrity),
    disclosures.map(unwrapRecursiveProof),
    manifest.root,
  );

  const result = {
    version: payload.version || manifest.version,
    current_date: normalizeCurrentDate(payload, disclosures),
    outer_circuit_name: `outer_evm_count_${3 + disclosures.length}`,
    inner_bundle: {
      version: payload.version || manifest.version,
      current_date: normalizeCurrentDate(payload, disclosures),
      dsc,
      id_data: idData,
      integrity,
      disclosures,
      metadata: {},
    },
    outer_inputs: outerInputs,
  };

  fs.writeFileSync(outPath, JSON.stringify(result, null, 2));
}

main().catch((error) => {
  console.error(error?.stack || String(error));
  process.exit(1);
});
