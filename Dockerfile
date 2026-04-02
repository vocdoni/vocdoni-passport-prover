# syntax=docker/dockerfile:1.7

# Canonical proving image for vocdoni-passport-prover.
#
# This image is the source of truth for the zk proving stack:
# - prover-cli
# - zkPassport-compatible bb
# - proving CRS
# - packaged circuit artifacts
# - helper scripts and minimal zkpassport-utils runtime
#
# The server should be treated as an orchestrator that consumes this stack, not as a second
# place where proving versions are decided independently.

FROM ubuntu:22.04 AS bb-builder

ARG DEBIAN_FRONTEND=noninteractive
ARG AZTEC_PACKAGES_REF=a4f7c39e15e7835c1f5f491168afa4aaac286894

# Build bb from the zkPassport-compatible aztec-packages line.
# Do not replace this casually with an upstream prebuilt binary. The registry artifacts and
# recursive proof path are sensitive to bb provenance.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
    software-properties-common \
    gnupg \
    gpg-agent \
    dirmngr \
    && add-apt-repository -y ppa:ubuntu-toolchain-r/test \
    && apt-get update && apt-get install -y --no-install-recommends \
    gcc-13 \
    g++-13 \
    git \
    jq \
    curl \
    wget \
    time \
    file \
    ca-certificates \
    libc++1 \
    zlib1g \
    coreutils \
    build-essential \
    ninja-build \
    parallel \
    libssl-dev \
    gawk \
    bison \
    libgmp-dev \
    libmpfr-dev \
    libmpc-dev \
    && rm -rf /var/lib/apt/lists/*

RUN cd /tmp && \
    wget -q https://github.com/Kitware/CMake/releases/download/v3.29.2/cmake-3.29.2-linux-x86_64.tar.gz && \
    tar -xzf cmake-3.29.2-linux-x86_64.tar.gz && \
    mv cmake-3.29.2-linux-x86_64 /opt/cmake-3.29.2 && \
    ln -sf /opt/cmake-3.29.2/bin/cmake /usr/local/bin/cmake && \
    ln -sf /opt/cmake-3.29.2/bin/ctest /usr/local/bin/ctest && \
    ln -sf /opt/cmake-3.29.2/bin/cpack /usr/local/bin/cpack && \
    rm -rf /tmp/cmake-3.29.2-linux-x86_64 /tmp/cmake-3.29.2-linux-x86_64.tar.gz

RUN cd /root && wget -q https://apt.llvm.org/llvm.sh && chmod +x llvm.sh && ./llvm.sh 20 && rm llvm.sh

COPY docker/download_bb_crs.sh /tmp/download_bb_crs.sh
RUN chmod +x /tmp/download_bb_crs.sh && /tmp/download_bb_crs.sh 23 && rm /tmp/download_bb_crs.sh

WORKDIR /src
RUN git clone https://github.com/zkpassport/aztec-packages /src/aztec-packages \
    && cd /src/aztec-packages \
    && git checkout ${AZTEC_PACKAGES_REF}

# Keep this patch documented. The pinned msgpack commit in this aztec line is no longer fetchable.
# If a future aztec/zkpassport revision removes this need, delete the patch instead of carrying it forever.
RUN sed -i 's/5ee9a1c8c325658b29867829677c7eb79c433a98/c0334576ed657fb3b3c49e8e61402989fb84146d/' \
    /src/aztec-packages/barretenberg/cpp/cmake/msgpack.cmake

RUN cd /src/aztec-packages/barretenberg/cpp && \
    cmake --preset clang20 \
      -DCMAKE_BUILD_TYPE=Release \
      -DTARGET_ARCH=native \
      -DENABLE_PAR_ALGOS=ON \
      -DMULTITHREADING=ON \
      -DDISABLE_AZTEC_VM=ON \
      -DCMAKE_CXX_FLAGS="-O3 -march=native -mtune=native" && \
    cmake --build build --target bb

FROM node:20-bookworm AS zkp-builder

ARG ZKPASSPORT_PACKAGES_REF=efb013e15c798a8cd36b92ec17585b391731199b

# Only the built runtime pieces of zkpassport-utils are copied into the final image.
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl unzip ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

ENV BUN_INSTALL=/opt/bun
ENV PATH="/opt/bun/bin:${PATH}"

RUN curl -fsSL https://bun.sh/install | bash -s -- bun-v1.3.1

WORKDIR /src
RUN git clone https://github.com/zkpassport/zkpassport-packages /src/zkpassport-packages \
    && cd /src/zkpassport-packages \
    && git checkout ${ZKPASSPORT_PACKAGES_REF}

WORKDIR /src/zkpassport-packages
RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile

RUN cd packages/zkpassport-utils && bun run build

FROM rust:1.89-trixie

ARG NODE_MAJOR=20

ENV DEBIAN_FRONTEND=noninteractive

# Runtime image for prover-cli plus the helper Node runtime needed by zkpassport-utils scripts.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    clang \
    cmake \
    curl \
    git \
    gnupg \
    libgmp-dev \
    libssl-dev \
    jq \
    lld \
    make \
    ninja-build \
    perl \
    pkg-config \
    protobuf-compiler \
    python3 \
    unzip \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key \
        | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_${NODE_MAJOR}.x nodistro main" \
        > /etc/apt/sources.list.d/nodesource.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

COPY --from=bb-builder /src/aztec-packages/barretenberg/cpp/build/bin/bb /usr/local/bin/bb
COPY --from=bb-builder /root/.bb-crs /opt/bb-crs

ENV BB_BINARY_PATH=/usr/local/bin/bb
ENV CRS_PATH=/opt/bb-crs
ENV PATH=/usr/local/cargo/bin:/usr/local/bin:/usr/bin:/bin

WORKDIR /opt/vocdoni/repos/vocdoni-passport-prover
COPY . /opt/vocdoni/repos/vocdoni-passport-prover

RUN --mount=type=cache,target=/usr/local/cargo/registry,sharing=locked \
    --mount=type=cache,target=/usr/local/cargo/git,sharing=locked \
    cargo build --release -p prover-cli --features native-prover

COPY --from=zkp-builder /src/zkpassport-packages/node_modules /opt/vocdoni/repos/zkpassport-packages/node_modules
COPY --from=zkp-builder /src/zkpassport-packages/packages/zkpassport-utils/node_modules /opt/vocdoni/repos/zkpassport-packages/packages/zkpassport-utils/node_modules
COPY --from=zkp-builder /src/zkpassport-packages/packages/zkpassport-utils/dist /opt/vocdoni/repos/zkpassport-packages/packages/zkpassport-utils/dist

ENV VOCDONI_WORKSPACE_ROOT=/opt/vocdoni/repos/vocdoni-passport-prover
ENV VOCDONI_ARTIFACTS_DIR=/opt/vocdoni/repos/vocdoni-passport-prover/artifacts/registry/minimal-default-0.16.0

WORKDIR /opt/vocdoni/repos/vocdoni-passport-prover

ENTRYPOINT ["/opt/vocdoni/repos/vocdoni-passport-prover/target/release/prover-cli"]
