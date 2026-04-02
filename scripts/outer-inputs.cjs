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

async function fetchJson(url) {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`failed to fetch ${url}: ${response.status}`);
  }
  return response.json();
}

function toFieldHex(value) {
  return `0x${Buffer.from(value).toString('hex').padStart(64, '0')}`;
}

function splitFieldBuffer(buf) {
  if (buf.length % 32 !== 0) {
    throw new Error(`buffer length ${buf.length} is not a multiple of 32 bytes`);
  }
  const out = [];
  for (let offset = 0; offset < buf.length; offset += 32) {
    out.push(toFieldHex(buf.subarray(offset, offset + 32)));
  }
  return out;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const fixtureDir = args['fixture-dir'];
  const proofDir = args['proof-dir'];
  const artifactsDir = args['artifacts-dir'];
  const outPath = args.out;

  if (!fixtureDir || !proofDir || !artifactsDir || !outPath) {
    throw new Error(
      'usage: outer-inputs.cjs --fixture-dir <dir> --proof-dir <dir> --artifacts-dir <dir> --out <file>',
    );
  }

  const fixture = readJson(path.join(fixtureDir, 'bundle.json'));
  const manifest = readJson(path.join(artifactsDir, 'manifest.json'));
  const summary = readJson(path.join(proofDir, 'summary.json'));
  const byHashBase = 'https://circuits2.zkpassport.id/sepolia/by-hash';

  const circuits = summary.circuits.map((entry) => entry.circuit_name);
  const dscName = circuits.find((name) => name.startsWith('sig_check_dsc_'));
  const idDataName = circuits.find((name) => name.startsWith('sig_check_id_data_'));
  const integrityName = circuits.find((name) => name.startsWith('data_check_integrity_'));
  const disclosureNames = circuits.filter(
    (name) =>
      name !== dscName &&
      name !== idDataName &&
      name !== integrityName &&
      !name.startsWith('outer_'),
  );

  if (!dscName || !idDataName || !integrityName || disclosureNames.length === 0) {
    throw new Error(`could not classify inner proof artifacts from ${path.join(proofDir, 'summary.json')}`);
  }

  async function loadCircuitArtifact(circuitName) {
    const localPath = path.join(artifactsDir, 'circuits', `${circuitName}.json`);
    if (fs.existsSync(localPath)) {
      return readJson(localPath);
    }
    const manifestNode = manifest.circuits?.[circuitName];
    if (!manifestNode?.hash) {
      throw new Error(`circuit ${circuitName} not found in manifest`);
    }
    return fetchJson(`${byHashBase}/${manifestNode.hash}.json`);
  }

  async function buildRecursiveProof(circuitName) {
    const artifact = await loadCircuitArtifact(circuitName);
    const proofBytes = fs.readFileSync(path.join(proofDir, `${circuitName}.proof.bin`));
    const publicInputsBytes = fs.readFileSync(
      path.join(proofDir, `${circuitName}.public_inputs.bin`),
    );
    const vkeyBytes = Buffer.from(artifact.vkey, 'base64');
    const keyHash = artifact.vkey_hash || manifest.circuits?.[circuitName]?.hash;
    const tree = await UTILS.getCircuitMerkleProof(keyHash, manifest);
    return {
      circuit_name: circuitName,
      proof: splitFieldBuffer(proofBytes),
      public_inputs: splitFieldBuffer(publicInputsBytes),
      vkey: UTILS.ultraVkToFields(vkeyBytes),
      key_hash: keyHash,
      tree_hash_path: tree.path.map((value) =>
        typeof value === 'string' ? value : `0x${BigInt(value).toString(16)}`,
      ),
      tree_index: String(tree.index),
    };
  }

  const dsc = await buildRecursiveProof(dscName);
  const idData = await buildRecursiveProof(idDataName);
  const integrity = await buildRecursiveProof(integrityName);
  const disclosures = [];
  for (const name of disclosureNames) {
    disclosures.push(await buildRecursiveProof(name));
  }

  const outerInputs = UTILS.getOuterCircuitInputs(
    {
      proof: dsc.proof,
      publicInputs: dsc.public_inputs,
      vkey: dsc.vkey,
      keyHash: dsc.key_hash,
      treeHashPath: dsc.tree_hash_path,
      treeIndex: dsc.tree_index,
    },
    {
      proof: idData.proof,
      publicInputs: idData.public_inputs,
      vkey: idData.vkey,
      keyHash: idData.key_hash,
      treeHashPath: idData.tree_hash_path,
      treeIndex: idData.tree_index,
    },
    {
      proof: integrity.proof,
      publicInputs: integrity.public_inputs,
      vkey: integrity.vkey,
      keyHash: integrity.key_hash,
      treeHashPath: integrity.tree_hash_path,
      treeIndex: integrity.tree_index,
    },
    disclosures.map((proof) => ({
      proof: proof.proof,
      publicInputs: proof.public_inputs,
      vkey: proof.vkey,
      keyHash: proof.key_hash,
      treeHashPath: proof.tree_hash_path,
      treeIndex: proof.tree_index,
    })),
    manifest.root,
  );

  const result = {
    version: fixture.request?.circuit_version || manifest.version,
    current_date:
      fixture.request?.current_date || Number(BigInt(disclosures[0].public_inputs[1] || '0x0')),
    outer_circuit_name: `outer_evm_count_${3 + disclosures.length}`,
    inner_bundle: {
      version: fixture.request?.circuit_version || manifest.version,
      current_date:
        fixture.request?.current_date || Number(BigInt(disclosures[0].public_inputs[1] || '0x0')),
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
