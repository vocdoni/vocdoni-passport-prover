#!/bin/bash
set -eu

# Download CRS for Barretenberg.
# Accepts an optional subgroup size exponent and stores the files in $HOME/.bb-crs.

subgroup_size_exp="${1:-23}"
crs_path="${HOME}/.bb-crs"

crs_size=$((2**subgroup_size_exp + 1))
crs_size_bytes=$((crs_size * 64))
g1="${crs_path}/bn254_g1.dat"
g2="${crs_path}/bn254_g2.dat"

if [ ! -f "${g1}" ] || [ "$(stat -c%s "${g1}")" -lt "${crs_size_bytes}" ]; then
  echo "Downloading CRS of size: ${crs_size} ($((crs_size_bytes / (1024 * 1024)))MB)"
  mkdir -p "${crs_path}"
  curl -s -H "Range: bytes=0-$((crs_size_bytes - 1))" -o "${g1}" \
    https://aztec-ignition.s3.amazonaws.com/MAIN%20IGNITION/flat/g1.dat
  chmod a-w "${g1}"
fi

if [ ! -f "${g2}" ]; then
  curl -s https://aztec-ignition.s3.amazonaws.com/MAIN%20IGNITION/flat/g2.dat -o "${g2}"
fi

grumpkin_g1="${crs_path}/grumpkin_g1.dat"
grumpkin_num_points=$((2**16 + 1))
grumpkin_start=28
grumpkin_size_bytes=$((grumpkin_num_points * 64))
grumpkin_end=$((grumpkin_start + grumpkin_size_bytes - 1))

if [ ! -f "${grumpkin_g1}" ]; then
  echo "Downloading Grumpkin transcript..."
  curl -s -H "Range: bytes=${grumpkin_start}-${grumpkin_end}" \
    -o "${grumpkin_g1}" \
    "https://aztec-ignition.s3.amazonaws.com/TEST%20GRUMPKIN/monomial/transcript00.dat"
  echo -n "${grumpkin_num_points}" > "${crs_path}/grumpkin_size"
fi
