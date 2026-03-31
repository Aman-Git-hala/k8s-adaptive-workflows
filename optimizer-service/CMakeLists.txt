cmake_minimum_required(VERSION 3.20)
project(optimizer_service VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# ───────────────────────────────────────────────────────────────
# Find gRPC and Protobuf (installed via system package manager)
# ───────────────────────────────────────────────────────────────
find_package(Threads REQUIRED)
find_package(Protobuf REQUIRED)
find_package(gRPC CONFIG QUIET)

if(gRPC_FOUND)
    message(STATUS "Found gRPC via CMake config")
    set(_GRPC_GRPCPP gRPC::grpc++)
    set(_GRPC_CPP_PLUGIN $<TARGET_FILE:gRPC::grpc_cpp_plugin>)
else()
    # Fallback: find via pkg-config
    find_package(PkgConfig REQUIRED)
    pkg_check_modules(GRPC REQUIRED grpc++ grpc)
    set(_GRPC_GRPCPP ${GRPC_LIBRARIES})
    find_program(_GRPC_CPP_PLUGIN grpc_cpp_plugin REQUIRED)
endif()

find_program(_PROTOBUF_PROTOC protoc REQUIRED)

# ───────────────────────────────────────────────────────────────
# Proto file compilation
# ───────────────────────────────────────────────────────────────
set(PROTO_SRC_DIR "${CMAKE_SOURCE_DIR}/../proto")
set(PROTO_GENERATED_DIR "${CMAKE_BINARY_DIR}/generated")
file(MAKE_DIRECTORY ${PROTO_GENERATED_DIR})

set(OPTIMIZER_PROTO "${PROTO_SRC_DIR}/optimizer.proto")

# Generated file paths
set(OPTIMIZER_PROTO_SRCS "${PROTO_GENERATED_DIR}/optimizer.pb.cc")
set(OPTIMIZER_PROTO_HDRS "${PROTO_GENERATED_DIR}/optimizer.pb.h")
set(OPTIMIZER_GRPC_SRCS  "${PROTO_GENERATED_DIR}/optimizer.grpc.pb.cc")
set(OPTIMIZER_GRPC_HDRS  "${PROTO_GENERATED_DIR}/optimizer.grpc.pb.h")

add_custom_command(
    OUTPUT "${OPTIMIZER_PROTO_SRCS}" "${OPTIMIZER_PROTO_HDRS}"
           "${OPTIMIZER_GRPC_SRCS}" "${OPTIMIZER_GRPC_HDRS}"
    COMMAND ${_PROTOBUF_PROTOC}
    ARGS --grpc_out "${PROTO_GENERATED_DIR}"
         --cpp_out "${PROTO_GENERATED_DIR}"
         -I "${PROTO_SRC_DIR}"
         --plugin=protoc-gen-grpc="${_GRPC_CPP_PLUGIN}"
         "${OPTIMIZER_PROTO}"
    DEPENDS "${OPTIMIZER_PROTO}"
    COMMENT "Generating C++ gRPC stubs from optimizer.proto"
)

# ───────────────────────────────────────────────────────────────
# Proto library
# ───────────────────────────────────────────────────────────────
add_library(optimizer_proto
    ${OPTIMIZER_PROTO_SRCS}
    ${OPTIMIZER_GRPC_SRCS}
)
target_include_directories(optimizer_proto PUBLIC ${PROTO_GENERATED_DIR})
target_link_libraries(optimizer_proto PUBLIC
    protobuf::libprotobuf
    ${_GRPC_GRPCPP}
)

# ───────────────────────────────────────────────────────────────
# Optimizer library (the greedy algorithm)
# ───────────────────────────────────────────────────────────────
add_library(greedy_optimizer
    src/greedy_optimizer.cpp
)
target_include_directories(greedy_optimizer PUBLIC
    ${CMAKE_SOURCE_DIR}/src
    ${PROTO_GENERATED_DIR}
)
target_link_libraries(greedy_optimizer PUBLIC optimizer_proto)

# ───────────────────────────────────────────────────────────────
# Main executable (gRPC server)
# ───────────────────────────────────────────────────────────────
add_executable(optimizer_server src/main.cpp)
target_link_libraries(optimizer_server PRIVATE
    greedy_optimizer
    optimizer_proto
    ${_GRPC_GRPCPP}
    protobuf::libprotobuf
    Threads::Threads
)

# ───────────────────────────────────────────────────────────────
# Install
# ───────────────────────────────────────────────────────────────
install(TARGETS optimizer_server DESTINATION bin)
