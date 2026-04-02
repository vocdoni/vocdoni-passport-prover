#!/usr/bin/env node

const fs = require('fs');
const path = require('path');

const WORKSPACE_ROOT = path.resolve(__dirname, '..', '..', '..');
const PROVER_ROOT = path.resolve(__dirname, '..');

function loadUtils() {
  const candidates = [
    process.env.VOCDONI_ZKPASSPORT_UTILS_DIST,
    path.join(WORKSPACE_ROOT, 'repos', 'zkpassport-packages', 'packages', 'zkpassport-utils', 'dist', 'cjs', 'index.cjs'),
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
    out[key.slice(2)] = argv[i + 1];
    i += 1;
  }
  return out;
}

function readJson(filePath) {
  return JSON.parse(fs.readFileSync(filePath, 'utf8'));
}

function splitFieldBuffer(buf) {
  if (buf.length % 32 !== 0) {
    throw new Error(`buffer length ${buf.length} is not a multiple of 32 bytes`);
  }
  const out = [];
  for (let offset = 0; offset < buf.length; offset += 32) {
    out.push(`0x${Buffer.from(buf.subarray(offset, offset + 32)).toString('hex')}`);
  }
  return out;
}

function hex32(value) {
  const bigint = typeof value === 'bigint' ? value : BigInt(value);
  return `0x${bigint.toString(16).padStart(64, '0')}`;
}

function normalizeHex(value) {
  if (typeof value !== 'string') {
    return hex32(value);
  }
  return value.startsWith('0x') ? value.toLowerCase() : `0x${value.toLowerCase()}`;
}

function assertEq(label, actual, expected, failures) {
  if (actual !== expected) {
    failures.push({ label, actual, expected });
  }
}

function assertArrayEq(label, actual, expected, failures) {
  if (actual.length !== expected.length) {
    failures.push({
      label: `${label}.length`,
      actual: String(actual.length),
      expected: String(expected.length),
    });
    return;
  }
  for (let i = 0; i < actual.length; i += 1) {
    if (normalizeHex(actual[i]) !== normalizeHex(expected[i])) {
      failures.push({
        label: `${label}[${i}]`,
        actual: normalizeHex(actual[i]),
        expected: normalizeHex(expected[i]),
      });
      return;
    }
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const outerDir = path.resolve(args['outer-dir'] || 'outputs/latest/outer');
  const proofDir = path.resolve(args['proof-dir'] || 'outputs/latest/proofs');
  const artifactsDir = path.resolve(
    args['artifacts-dir'] || path.join(PROVER_ROOT, 'artifacts', 'registry', 'minimal-default-0.16.0'),
  );

  const outerInputs = readJson(path.join(outerDir, 'outer_inputs.json'));
  const outerResult = readJson(path.join(outerDir, 'outer_result.json'));
  const summary = readJson(path.join(proofDir, 'summary.json'));
  const manifest = readJson(path.join(artifactsDir, 'manifest.json'));

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

  const failures = [];
  const dscPublicInputs = splitFieldBuffer(fs.readFileSync(path.join(proofDir, `${dscName}.public_inputs.bin`)));
  const dscProof = splitFieldBuffer(fs.readFileSync(path.join(proofDir, `${dscName}.proof.bin`)));
  const idDataPublicInputs = splitFieldBuffer(
    fs.readFileSync(path.join(proofDir, `${idDataName}.public_inputs.bin`)),
  );
  const idDataProof = splitFieldBuffer(fs.readFileSync(path.join(proofDir, `${idDataName}.proof.bin`)));
  const integrityPublicInputs = splitFieldBuffer(
    fs.readFileSync(path.join(proofDir, `${integrityName}.public_inputs.bin`)),
  );
  const integrityProof = splitFieldBuffer(fs.readFileSync(path.join(proofDir, `${integrityName}.proof.bin`)));

  async function expectedTree(circuitName) {
    const keyHash = manifest.circuits?.[circuitName]?.hash;
    if (!keyHash) {
      failures.push({
        label: `${circuitName}.manifest`,
        actual: 'missing',
        expected: 'present',
      });
      return null;
    }
    const tree = await UTILS.getCircuitMerkleProof(keyHash, manifest);
    return {
      keyHash,
      path: tree.path.map((value) =>
        typeof value === 'string' ? value : `0x${BigInt(value).toString(16).padStart(64, '0')}`,
      ),
      index: String(tree.index),
    };
  }

  async function assertRecursiveProof(label, actual, circuitName, proofFields, publicInputs) {
    const vkBytes = fs.readFileSync(path.join(proofDir, `${circuitName}.vk.bin`));
    const tree = await expectedTree(circuitName);
    assertArrayEq(`${label}.proof`, actual.proof, proofFields, failures);
    assertArrayEq(`${label}.public_inputs`, actual.public_inputs, publicInputs, failures);
    assertArrayEq(
      `${label}.vkey`,
      actual.vkey,
      UTILS.ultraVkToFields(vkBytes),
      failures,
    );
    assertEq(`${label}.key_hash`, normalizeHex(actual.key_hash), normalizeHex(manifest.circuits?.[circuitName]?.hash), failures);
    if (tree) {
      assertEq(`${label}.tree_index`, String(actual.tree_index), tree.index, failures);
      assertArrayEq(`${label}.tree_hash_path`, actual.tree_hash_path, tree.path, failures);
      assertEq(`${label}.manifest_hash`, normalizeHex(actual.key_hash), normalizeHex(tree.keyHash), failures);
    }
  }

  await assertRecursiveProof(
    'csc_to_dsc_proof',
    outerInputs.csc_to_dsc_proof,
    dscName,
    dscProof,
    dscPublicInputs.slice(1),
  );
  assertArrayEq(
    'csc_to_dsc_proof.public_inputs',
    outerInputs.csc_to_dsc_proof.public_inputs,
    dscPublicInputs.slice(1),
    failures,
  );
  await assertRecursiveProof(
    'dsc_to_id_data_proof',
    outerInputs.dsc_to_id_data_proof,
    idDataName,
    idDataProof,
    idDataPublicInputs,
  );
  assertArrayEq(
    'dsc_to_id_data_proof.public_inputs',
    outerInputs.dsc_to_id_data_proof.public_inputs,
    idDataPublicInputs,
    failures,
  );
  await assertRecursiveProof(
    'integrity_check_proof',
    outerInputs.integrity_check_proof,
    integrityName,
    integrityProof,
    integrityPublicInputs,
  );
  assertArrayEq(
    'integrity_check_proof.public_inputs',
    outerInputs.integrity_check_proof.public_inputs,
    integrityPublicInputs,
    failures,
  );

  for (const [index, name] of disclosureNames.entries()) {
    const disclosurePublicInputs = splitFieldBuffer(
      fs.readFileSync(path.join(proofDir, `${name}.public_inputs.bin`)),
    );
    const disclosureProof = splitFieldBuffer(fs.readFileSync(path.join(proofDir, `${name}.proof.bin`)));
    await assertRecursiveProof(
      `disclosure_proofs[${index}]`,
      outerInputs.disclosure_proofs[index],
      name,
      disclosureProof,
      [disclosurePublicInputs[0], disclosurePublicInputs[6]],
    );
    assertArrayEq(
      `disclosure_proofs[${index}].public_inputs`,
      outerInputs.disclosure_proofs[index].public_inputs,
      [disclosurePublicInputs[0], disclosurePublicInputs[6]],
      failures,
    );
  }

  const expectedOuterPublicInputs = [
    outerInputs.certificate_registry_root,
    outerInputs.circuit_registry_root,
    hex32(outerInputs.current_date),
    outerInputs.service_scope,
    outerInputs.service_subscope,
    outerInputs.param_commitments[0],
    outerInputs.nullifier_type,
    outerInputs.scoped_nullifier,
  ];
  assertArrayEq('outer_result.public_inputs', outerResult.public_inputs, expectedOuterPublicInputs, failures);

  const disclosurePublicInputs = splitFieldBuffer(
    fs.readFileSync(path.join(proofDir, `${disclosureNames[0]}.public_inputs.bin`)),
  );
  assertEq('current_date/disclosure', normalizeHex(expectedOuterPublicInputs[2]), normalizeHex(disclosurePublicInputs[1]), failures);
  assertEq('service_scope/disclosure', normalizeHex(expectedOuterPublicInputs[3]), normalizeHex(disclosurePublicInputs[2]), failures);
  assertEq('service_subscope/disclosure', normalizeHex(expectedOuterPublicInputs[4]), normalizeHex(disclosurePublicInputs[3]), failures);
  assertEq('param_commitment/disclosure', normalizeHex(expectedOuterPublicInputs[5]), normalizeHex(disclosurePublicInputs[4]), failures);
  assertEq('nullifier_type/disclosure', normalizeHex(expectedOuterPublicInputs[6]), normalizeHex(disclosurePublicInputs[5]), failures);
  assertEq('scoped_nullifier/disclosure', normalizeHex(expectedOuterPublicInputs[7]), normalizeHex(disclosurePublicInputs[6]), failures);

  const result = {
    ok: failures.length === 0,
    checked: {
      dsc: dscName,
      id_data: idDataName,
      integrity: integrityName,
      disclosures: disclosureNames,
    },
    failures,
  };

  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
  if (failures.length > 0) {
    process.exit(1);
  }
}

main().catch((error) => {
  console.error(error?.stack || String(error));
  process.exit(1);
});
