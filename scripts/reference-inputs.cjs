#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const crypto = require('crypto');

const ROOT = path.resolve(__dirname, '..', '..');
const UTILS = require(path.join(
  ROOT,
  'VocdoniPassport',
  'node_modules',
  '@zkpassport',
  'utils',
  'dist',
  'cjs',
  'index.cjs',
));

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

function hexBigint(value) {
  return `0x${BigInt(value).toString(16)}`;
}

function extractMrzFromDG1(dg1Bytes) {
  for (let i = 0; i < dg1Bytes.length - 2; i += 1) {
    if (dg1Bytes[i] === 0x5f && dg1Bytes[i + 1] === 0x1f) {
      const len = dg1Bytes[i + 2];
      const start = i + 3;
      if (start + len <= dg1Bytes.length) {
        return Buffer.from(dg1Bytes.slice(start, start + len)).toString('ascii');
      }
    }
  }
  return Buffer.from(dg1Bytes).toString('ascii');
}

function parseMrz(mrz) {
  const clean = String(mrz || '').replace(/\n/g, '').replace(/ /g, '');
  if (clean.length >= 88 && clean[0] === 'P') {
    const names = clean.slice(5, 44).split('<<');
    return {
      issuingCountry: clean.slice(2, 5).replace(/</g, ''),
      documentNumber: clean.slice(44, 53).replace(/</g, ''),
      nationality: clean.slice(54, 57).replace(/</g, ''),
      dateOfBirth: clean.slice(57, 63),
      gender: clean.slice(64, 65).replace(/</g, ''),
      expiryDate: clean.slice(65, 71),
      lastName: (names[0] || '').replace(/</g, ' ').trim(),
      firstName: (names[1] || '').replace(/</g, ' ').trim(),
    };
  }
  if (clean.length >= 90) {
    const line1 = clean.slice(0, 30);
    const line2 = clean.slice(30, 60);
    const line3 = clean.slice(60, 90);
    const names = line3.split('<<');
    return {
      issuingCountry: line1.slice(2, 5).replace(/</g, ''),
      documentNumber: line1.slice(5, 14).replace(/</g, ''),
      nationality: line2.slice(15, 18).replace(/</g, ''),
      dateOfBirth: line2.slice(0, 6),
      gender: line2.slice(7, 8).replace(/</g, ''),
      expiryDate: line2.slice(8, 14),
      lastName: (names[0] || '').replace(/</g, ' ').trim(),
      firstName: (names[1] || '').replace(/</g, ' ').trim(),
    };
  }
  return {
    issuingCountry: '',
    documentNumber: '',
    nationality: '',
    dateOfBirth: '',
    gender: '',
    expiryDate: '',
    lastName: '',
    firstName: '',
  };
}

function inferDocumentType(mrz) {
  const clean = String(mrz || '').replace(/\s+/g, '');
  return clean.length >= 88 && clean.startsWith('P') ? 'passport' : 'id_card';
}

function buildPassportViewModel(document) {
  const dg1Buffer = Buffer.from(document.dg1_base64, 'base64');
  const dg1 = UTILS.Binary.fromBase64(document.dg1_base64);
  const sod = UTILS.SOD.fromDER(UTILS.Binary.fromBase64(document.sod_der_base64));
  const mrz = extractMrzFromDG1(dg1Buffer);
  const mrzFields = parseMrz(mrz);

  const dataGroupHashes = sod.encapContentInfo.eContent.dataGroupHashValues.values;
  const ldsHashAlgo = String(sod.encapContentInfo.eContent.hashAlgorithm);
  const dg1HashFromSod = dataGroupHashes[1];
  const dg2HashFromSod = dataGroupHashes[2];
  const dg1HashArray = dg1HashFromSod
    ? dg1HashFromSod.toNumberArray()
    : Array.from(crypto.createHash('sha256').update(dg1Buffer).digest());

  const dg2Group = document.dg2_base64
    ? (() => {
        const raw = UTILS.Binary.fromBase64(document.dg2_base64);
        return {
          groupNumber: 2,
          name: 'DG2',
          hash: dg2HashFromSod
            ? dg2HashFromSod.toNumberArray()
            : Array.from(crypto.createHash('sha256').update(Buffer.from(document.dg2_base64, 'base64')).digest()),
          value: raw.toNumberArray(),
        };
      })()
    : {
        groupNumber: 2,
        name: 'DG2',
        hash: dg2HashFromSod ? dg2HashFromSod.toNumberArray() : Array(32).fill(0),
        value: [],
      };

  return {
    dateOfIssue: '',
    appVersion: '',
    mrz,
    name: `${mrzFields.firstName} ${mrzFields.lastName}`.trim(),
    dateOfBirth: mrzFields.dateOfBirth,
    nationality: mrzFields.nationality,
    gender: mrzFields.gender,
    passportNumber: mrzFields.documentNumber,
    passportExpiry: mrzFields.expiryDate,
    firstName: mrzFields.firstName,
    lastName: mrzFields.lastName,
    fullName: `${mrzFields.firstName} ${mrzFields.lastName}`.trim(),
    photo: '',
    originalPhoto: '',
    chipAuthSupported: false,
    chipAuthSuccess: false,
    chipAuthFailed: false,
    LDSVersion: '',
    documentType: document.document_type || inferDocumentType(mrz),
    dataGroupsHashAlgorithm: ldsHashAlgo,
    dataGroups: [
      {
        groupNumber: 1,
        name: 'DG1',
        hash: dg1HashArray,
        value: dg1.toNumberArray(),
      },
      dg2Group,
    ],
    sod,
  };
}

function normalizeQuery(query) {
  if (!query || typeof query !== 'object' || Array.isArray(query)) {
    return { nationality: { disclose: true } };
  }
  return Object.keys(query).length === 0 ? { nationality: { disclose: true } } : query;
}

function buildDisclosurePlans(query) {
  const plans = [];
  const hasDiscloseBytes = Object.entries(query).some(([, cfg]) => cfg && (cfg.disclose || cfg.eq));
  if (hasDiscloseBytes) {
    plans.push({
      circuit_name: 'disclose_bytes_evm',
      build: (passport, saltsOut, nullifierSecret, serviceScope, serviceSubscope, currentDate) =>
        UTILS.getDiscloseCircuitInputs(
          passport,
          query,
          saltsOut,
          nullifierSecret,
          serviceScope,
          serviceSubscope,
          currentDate,
        ),
    });
  }
  if (query.age && (query.age.gte != null || query.age.gt != null || query.age.lte != null || query.age.lt != null || query.age.range != null || query.age.eq != null || query.age.disclose)) {
    plans.push({
      circuit_name: 'compare_age_evm',
      build: (passport, saltsOut, nullifierSecret, serviceScope, serviceSubscope, currentDate) =>
        UTILS.getAgeCircuitInputs(
          passport,
          query,
          saltsOut,
          nullifierSecret,
          serviceScope,
          serviceSubscope,
          currentDate,
        ),
    });
  }
  if (query.nationality && query.nationality.in) {
    plans.push({
      circuit_name: 'inclusion_check_nationality_evm',
      build: (passport, saltsOut, nullifierSecret, serviceScope, serviceSubscope, currentDate) =>
        UTILS.getNationalityInclusionCircuitInputs(
          passport,
          query,
          saltsOut,
          nullifierSecret,
          serviceScope,
          serviceSubscope,
          currentDate,
        ),
    });
  }
  if (query.nationality && query.nationality.out) {
    plans.push({
      circuit_name: 'exclusion_check_nationality_evm',
      build: (passport, saltsOut, nullifierSecret, serviceScope, serviceSubscope, currentDate) =>
        UTILS.getNationalityExclusionCircuitInputs(
          passport,
          query,
          saltsOut,
          nullifierSecret,
          serviceScope,
          serviceSubscope,
          currentDate,
        ),
    });
  }
  if (query.issuing_country && query.issuing_country.in) {
    plans.push({
      circuit_name: 'inclusion_check_issuing_country_evm',
      build: (passport, saltsOut, nullifierSecret, serviceScope, serviceSubscope, currentDate) =>
        UTILS.getIssuingCountryInclusionCircuitInputs(
          passport,
          query,
          saltsOut,
          nullifierSecret,
          serviceScope,
          serviceSubscope,
          currentDate,
        ),
    });
  }
  if (query.issuing_country && query.issuing_country.out) {
    plans.push({
      circuit_name: 'exclusion_check_issuing_country_evm',
      build: (passport, saltsOut, nullifierSecret, serviceScope, serviceSubscope, currentDate) =>
        UTILS.getIssuingCountryExclusionCircuitInputs(
          passport,
          query,
          saltsOut,
          nullifierSecret,
          serviceScope,
          serviceSubscope,
          currentDate,
        ),
    });
  }
  if (plans.length === 0) {
    throw new Error('query produced no supported disclosure circuits');
  }
  return plans;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (!args['fixture-dir']) throw new Error('missing --fixture-dir');
  if (!args['certificates']) throw new Error('missing --certificates');

  const fixtureDir = path.resolve(args['fixture-dir']);
  const certificatesPath = path.resolve(args['certificates']);
  const outPath = args.out ? path.resolve(args.out) : null;

  const document = readJson(path.join(fixtureDir, 'document.json'));
  const request = readJson(path.join(fixtureDir, 'request.json'));
  const packagedCerts = readJson(certificatesPath);
  const passport = buildPassportViewModel(document);
  const query = normalizeQuery(request.query);

  const saltDscIn = 1n;
  const saltIdOut = 0n;
  const saltsOut = {
    dg1Salt: 1n,
    expiryDateSalt: 1n,
    dg2HashSalt: 1n,
    privateNullifierSalt: 1n,
  };
  const nullifierSecret = 0n;
  const serviceScope = UTILS.getServiceScopeHash(request.service_scope || 'vocdoni-passport');
  const serviceSubscope = UTILS.getServiceSubscopeHash(request.service_subscope || 'petition');
  const currentDate = Number(request.current_date);

  const dscInputs = await UTILS.getDSCCircuitInputs(passport, saltDscIn, packagedCerts);
  const idDataInputs = await UTILS.getIDDataCircuitInputs(passport, saltDscIn, saltIdOut);
  const integrityInputs = await UTILS.getIntegrityCheckCircuitInputs(passport, saltIdOut, saltsOut);
  const disclosurePlans = buildDisclosurePlans(query);
  const disclosures = [];
  for (const plan of disclosurePlans) {
    disclosures.push({
      circuit_name: plan.circuit_name,
      inputs: await plan.build(
        passport,
        saltsOut,
        nullifierSecret,
        serviceScope,
        serviceSubscope,
        currentDate,
      ),
    });
  }

  const output = {
    fixture_dir: fixtureDir,
    current_date: currentDate,
    service_scope: hexBigint(serviceScope),
    service_subscope: hexBigint(serviceSubscope),
    salts: {
      dsc_in: hexBigint(saltDscIn),
      id_out: hexBigint(saltIdOut),
      dg1: hexBigint(saltsOut.dg1Salt),
      expiry_date: hexBigint(saltsOut.expiryDateSalt),
      dg2_hash: hexBigint(saltsOut.dg2HashSalt),
      private_nullifier: hexBigint(saltsOut.privateNullifierSalt),
      nullifier_secret: hexBigint(nullifierSecret),
    },
    passport_meta: {
      mrz: passport.mrz,
      document_type: passport.documentType,
      tbs_bucket: UTILS.getTBSMaxLen(passport),
    },
    dsc_inputs: dscInputs,
    id_data_inputs: idDataInputs,
    integrity_inputs: integrityInputs,
    disclosures,
  };

  const rendered = JSON.stringify(output, null, 2);
  if (outPath) {
    fs.writeFileSync(outPath, rendered);
  } else {
    process.stdout.write(rendered);
  }
}

main().catch((error) => {
  console.error(error && error.stack ? error.stack : String(error));
  process.exit(1);
});
