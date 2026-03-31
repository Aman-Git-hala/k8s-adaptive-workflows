# ─────────────────────────────────────────────────────────────
# Optimizer Service — Multi-stage Docker build (C++)
#
# Stage 1: Build with full toolchain (gRPC, protobuf, CMake)
# Stage 2: Minimal runtime image
# ─────────────────────────────────────────────────────────────

FROM ubuntu:22.04 AS builder

ENV DEBIAN_FRONTEND=noninteractive

# Install build tools, gRPC, and protobuf.
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    cmake \
    pkg-config \
    git \
    ca-certificates \
    libgrpc++-dev \
    libprotobuf-dev \
    protobuf-compiler \
    protobuf-compiler-grpc \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Copy proto files (needed for compilation).
COPY proto/optimizer.proto /build/proto/

# Copy optimizer service source.
COPY optimizer-service/ /build/optimizer-service/

WORKDIR /build/optimizer-service

# Build the optimizer.
RUN mkdir -p build && cd build \
    && cmake .. -DCMAKE_BUILD_TYPE=Release \
    && make -j$(nproc)

# ─────────────────────────────────────────────────────────────
# Runtime image
# ─────────────────────────────────────────────────────────────
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    libgrpc++1 \
    libprotobuf23 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /build/optimizer-service/build/optimizer_server /usr/local/bin/optimizer_server

ENV GRPC_PORT=50052
EXPOSE 50052

ENTRYPOINT ["optimizer_server"]
