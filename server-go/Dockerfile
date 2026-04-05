# syntax=docker/dockerfile:1.7
#
# Vocdoni Passport Server - Production Image
#
# This image contains everything needed to run the Vocdoni Passport server:
#   - Go HTTP server for API and operator dashboard
#   - prover-cli for zero-knowledge proof generation
#   - Barretenberg (bb) prover binary
#   - zkPassport circuit artifacts
#
# Usage:
#   docker build -f server-go/Dockerfile -t vocdoni-passport-server .
#   docker run -p 8080:8080 vocdoni-passport-server
#
# Or with Docker Compose:
#   cd server-go && docker compose up -d --build
#
# Copyright (c) 2024 Vocdoni Association
# SPDX-License-Identifier: AGPL-3.0-or-later

# =============================================================================
# Stage 1: Build Barretenberg (bb)
# =============================================================================

FROM ubuntu:22.04 AS bb-builder

ARG DEBIAN_FRONTEND=noninteractive
ARG AZTEC_PACKAGES_REF=a4f7c39e15e7835c1f5f491168afa4aaac286894

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
    rm -rf /tmp/cmake-3.29.2-linux-x86_64*

RUN cd /root && wget -q https://apt.llvm.org/llvm.sh && chmod +x llvm.sh && ./llvm.sh 20 && rm llvm.sh

COPY docker/download_bb_crs.sh /tmp/download_bb_crs.sh
RUN chmod +x /tmp/download_bb_crs.sh && /tmp/download_bb_crs.sh 23 && rm /tmp/download_bb_crs.sh

WORKDIR /src
RUN git clone https://github.com/zkpassport/aztec-packages /src/aztec-packages \
    && cd /src/aztec-packages \
    && git checkout ${AZTEC_PACKAGES_REF}

# Patch: The pinned msgpack commit is no longer fetchable
RUN sed -i 's/5ee9a1c8c325658b29867829677c7eb79c433a98/c0334576ed657fb3b3c49e8e61402989fb84146d/' \
    /src/aztec-packages/barretenberg/cpp/cmake/msgpack.cmake

# Build with AVX-512 for maximum performance on modern CPUs.
# Change to x86-64-v3 for older CPUs without AVX-512.
RUN cd /src/aztec-packages/barretenberg/cpp && \
    cmake --preset clang20 \
      -DCMAKE_BUILD_TYPE=Release \
      -DTARGET_ARCH=x86-64-v4 \
      -DENABLE_PAR_ALGOS=ON \
      -DMULTITHREADING=ON \
      -DDISABLE_AZTEC_VM=ON \
      -DCMAKE_CXX_FLAGS="-O3 -march=x86-64-v4 -mtune=generic" && \
    cmake --build build --target bb

# =============================================================================
# Stage 2: Build zkpassport-utils runtime
# =============================================================================

FROM node:20-bookworm AS zkp-builder

ARG ZKPASSPORT_PACKAGES_REF=efb013e15c798a8cd36b92ec17585b391731199b

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

# =============================================================================
# Stage 3: Build prover-cli (Rust)
# =============================================================================

FROM rust:1.89-bookworm AS prover-builder

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

# =============================================================================
# Stage 4: Build Go server
# =============================================================================

FROM golang:1.26-bookworm AS server-builder

WORKDIR /src/server-go
COPY server-go/go.mod /src/server-go/go.mod
COPY server-go/go.sum /src/server-go/go.sum
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download

COPY server-go /src/server-go
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/vocdoni-passport-server ./cmd

# =============================================================================
# Stage 5: Final runtime image
# =============================================================================

FROM debian:bookworm-slim

ARG DEBIAN_FRONTEND=noninteractive
ARG NODE_MAJOR=20

# Install runtime dependencies including tcmalloc for improved performance
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    gnupg \
    jq \
    libc++1 \
    libstdc++6 \
    libtcmalloc-minimal4 \
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

# Copy binaries
COPY --from=bb-builder /src/aztec-packages/barretenberg/cpp/build/bin/bb /usr/local/bin/bb
COPY --from=bb-builder /root/.bb-crs /opt/bb-crs
COPY --from=prover-builder /src/vocdoni-passport-prover/target/release/prover-cli /opt/vocdoni/bin/prover-cli
COPY --from=server-builder /out/vocdoni-passport-server /opt/vocdoni/bin/server

# Create directories
RUN mkdir -p /opt/vocdoni/downloads

# Copy prover workspace files
COPY config /opt/vocdoni/repos/vocdoni-passport-prover/config
COPY artifacts /opt/vocdoni/repos/vocdoni-passport-prover/artifacts
COPY scripts /opt/vocdoni/repos/vocdoni-passport-prover/scripts

# Copy zkpassport-utils runtime
COPY --from=zkp-builder /src/zkpassport-packages/node_modules /opt/vocdoni/repos/zkpassport-packages/node_modules
COPY --from=zkp-builder /src/zkpassport-packages/packages/zkpassport-utils/node_modules /opt/vocdoni/repos/zkpassport-packages/packages/zkpassport-utils/node_modules
COPY --from=zkp-builder /src/zkpassport-packages/packages/zkpassport-utils/dist /opt/vocdoni/repos/zkpassport-packages/packages/zkpassport-utils/dist

# Environment configuration
ENV BB_BINARY_PATH=/usr/local/bin/bb
ENV CRS_PATH=/opt/bb-crs
ENV VOCDONI_APK_PATH=/opt/vocdoni/downloads/app-release.apk
ENV VOCDONI_PROVER_BINARY_PATH=/opt/vocdoni/bin/prover-cli
ENV VOCDONI_WORKSPACE_ROOT=/opt/vocdoni/repos/vocdoni-passport-prover
ENV VOCDONI_ARTIFACTS_DIR=/opt/vocdoni/repos/vocdoni-passport-prover/artifacts/registry/minimal-default-0.16.0
ENV PATH=/opt/vocdoni/bin:/usr/local/bin:/usr/bin:/bin

# Use tcmalloc for improved memory allocation in multi-threaded proving
# Provides 10-60% speedup depending on core count
# Use tcmalloc for improved memory allocation in multi-threaded proving
# The exact path may vary by Debian version
ENV LD_PRELOAD=/usr/lib/x86_64-linux-gnu/libtcmalloc_minimal.so.4

EXPOSE 8080

ENTRYPOINT ["/opt/vocdoni/bin/server"]
