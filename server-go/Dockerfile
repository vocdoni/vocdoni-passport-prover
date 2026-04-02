# syntax=docker/dockerfile:1.7

# Go server image embedded inside vocdoni-passport-prover.
#
# This image has three responsibilities at runtime:
# 1. serve the HTTP API and operator page,
# 2. run the zkPassport-compatible outer prover,
# 3. distribute the latest Android APK.
#
# We keep those pieces in one image on purpose to avoid host-side dependency drift.

FROM ubuntu:22.04 AS bb-builder

ARG DEBIAN_FRONTEND=noninteractive
ARG AZTEC_PACKAGES_REF=a4f7c39e15e7835c1f5f491168afa4aaac286894

# Build `bb` from a pinned zkPassport-compatible upstream checkout.
# This avoids relying on an upstream prebuilt bb that might not match the packaged registry VKs.
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

# Keep this patch documented: the pinned msgpack commit in this aztec/zkpassport line is no longer fetchable.
# If a future aztec-packages revision removes this need, delete this rewrite rather than carrying it forward.
RUN sed -i 's/5ee9a1c8c325658b29867829677c7eb79c433a98/c0334576ed657fb3b3c49e8e61402989fb84146d/' \
    /src/aztec-packages/barretenberg/cpp/cmake/msgpack.cmake

# These flags are part of the pinned proving recipe for this repository.
# Treat bb upgrades as compatibility upgrades: validate against the prover artifacts before changing them.
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

# We only need the runtime pieces of zkpassport-utils used by prover helper scripts.
# Do not grow this stage casually: every extra package increases image size and upgrade surface.
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

FROM rust:1.89-trixie AS prover-builder

# Build the canonical prover binary from the local workspace, not from the server repo.
# The server is orchestration only; the proving source of truth is this repository.
ENV DEBIAN_FRONTEND=noninteractive

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    clang \
    cmake \
    git \
    libgmp-dev \
    libssl-dev \
    lld \
    make \
    ninja-build \
    pkg-config \
    protobuf-compiler \
    python3 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src/vocdoni-passport-prover
COPY Cargo.toml /src/vocdoni-passport-prover/Cargo.toml
COPY Cargo.lock /src/vocdoni-passport-prover/Cargo.lock
COPY crates /src/vocdoni-passport-prover/crates

RUN --mount=type=cache,target=/usr/local/cargo/registry,sharing=locked \
    --mount=type=cache,target=/usr/local/cargo/git,sharing=locked \
    cargo build --release -p prover-cli --features native-prover

FROM golang:1.24-trixie AS server-builder

# Build the thin HTTP/orchestration layer separately from the proving stack.
WORKDIR /src/server-go
COPY server-go/go.mod /src/server-go/go.mod
COPY server-go/go.sum /src/server-go/go.sum
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download

COPY server-go /src/server-go
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/vocdoni-passport-server ./cmd

FROM debian:trixie-slim

ARG DEBIAN_FRONTEND=noninteractive
ARG NODE_MAJOR=20

# Runtime base image.
# We install Node because the prover helper path still uses zkpassport-utils scripts at runtime.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    gnupg \
    jq \
    libc++1 \
    libstdc++6 \
    zlib1g \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key \
        | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_${NODE_MAJOR}.x nodistro main" \
        > /etc/apt/sources.list.d/nodesource.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/vocdoni

# Bundle the exact bb, prover binary, artifacts, and APK that correspond to the current tested stack.
# Future upgrades should update these pieces together, not one by one in isolation.
COPY --from=bb-builder /src/aztec-packages/barretenberg/cpp/build/bin/bb /usr/local/bin/bb
COPY --from=bb-builder /root/.bb-crs /opt/bb-crs
COPY --from=prover-builder /src/vocdoni-passport-prover/target/release/prover-cli /opt/vocdoni/bin/prover-cli
COPY --from=server-builder /out/vocdoni-passport-server /opt/vocdoni/bin/server
RUN mkdir -p /opt/vocdoni/downloads

# Copy the prover workspace pieces needed at runtime.
COPY config /opt/vocdoni/repos/vocdoni-passport-prover/config
COPY artifacts /opt/vocdoni/repos/vocdoni-passport-prover/artifacts
COPY scripts /opt/vocdoni/repos/vocdoni-passport-prover/scripts
# Only the built runtime dependencies of zkpassport-utils are needed here.
COPY --from=zkp-builder /src/zkpassport-packages/node_modules /opt/vocdoni/repos/zkpassport-packages/node_modules
COPY --from=zkp-builder /src/zkpassport-packages/packages/zkpassport-utils/node_modules /opt/vocdoni/repos/zkpassport-packages/packages/zkpassport-utils/node_modules
COPY --from=zkp-builder /src/zkpassport-packages/packages/zkpassport-utils/dist /opt/vocdoni/repos/zkpassport-packages/packages/zkpassport-utils/dist

# Runtime contract used by the Go server.
ENV BB_BINARY_PATH=/usr/local/bin/bb
ENV CRS_PATH=/opt/bb-crs
ENV VOCDONI_APK_PATH=/opt/vocdoni/downloads/app-release.apk
ENV VOCDONI_PROVER_BINARY_PATH=/opt/vocdoni/bin/prover-cli
ENV VOCDONI_WORKSPACE_ROOT=/opt/vocdoni/repos/vocdoni-passport-prover
ENV VOCDONI_ARTIFACTS_DIR=/opt/vocdoni/repos/vocdoni-passport-prover/artifacts/registry/minimal-default-0.16.0
ENV PATH=/opt/vocdoni/bin:/usr/local/bin:/usr/bin:/bin

EXPOSE 8080

ENTRYPOINT ["/opt/vocdoni/bin/server"]
