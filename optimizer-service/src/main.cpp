/*
 * main.cpp — Optimizer gRPC server (C++ implementation).
 *
 * This is a standalone C++ gRPC microservice that implements the
 * OptimizerService defined in proto/optimizer.proto. The controller
 * calls this service over gRPC to get scheduling decisions.
 *
 * Environment Variables:
 *   GRPC_PORT — Port to listen on (default: 50052)
 *
 * Copyright 2026. Apache License 2.0.
 */

#include <csignal>
#include <cstdlib>
#include <iostream>
#include <memory>
#include <string>

#include <grpcpp/grpcpp.h>
#include <grpcpp/health_check_service_interface.h>
#include <grpcpp/ext/proto_server_reflection_plugin.h>

#include "optimizer.grpc.pb.h"
#include "optimizer.pb.h"
#include "greedy_optimizer.h"

using grpc::Server;
using grpc::ServerBuilder;
using grpc::ServerContext;
using grpc::Status;

namespace pb = adaptive::optimizer::v1;

// ─────────────────────────────────────────────────────────────
// Proto ↔ Internal type conversion helpers
// ─────────────────────────────────────────────────────────────

adaptive::PlanRequest ProtoToPlanRequest(const pb::PlanRequest& proto_req) {
    adaptive::PlanRequest req;
    req.workflow_name = proto_req.workflow_name();
    req.optimization_goal = proto_req.optimization_goal();

    // Convert tasks.
    for (const auto& t : proto_req.tasks()) {
        adaptive::DAGTask task;
        task.name = t.name();
        task.image = t.image();
        for (const auto& cmd : t.command()) {
            task.command.push_back(cmd);
        }
        for (const auto& dep : t.dependencies()) {
            task.dependencies.push_back(dep);
        }
        if (t.has_base_resources()) {
            task.has_base_resources = true;
            task.base_resources.cpu_millicores =
                t.base_resources().cpu_millicores();
            task.base_resources.memory_mib = t.base_resources().memory_mib();
        }
        req.tasks.push_back(task);
    }

    // Convert task statuses.
    for (const auto& [name, s] : proto_req.task_statuses()) {
        adaptive::TaskStatus status;
        status.phase = s.phase();
        status.pod_name = s.pod_name();
        status.allocated_cpu_millicores = s.allocated_cpu_millicores();
        status.allocated_memory_mib = s.allocated_memory_mib();
        req.task_statuses[name] = status;
    }

    // Convert predictions.
    for (const auto& [name, p] : proto_req.predictions()) {
        adaptive::TaskPrediction pred;
        pred.cpu_millicores = p.cpu_millicores();
        pred.memory_mib = p.memory_mib();
        pred.estimated_duration_s = p.estimated_duration_s();
        pred.confidence = p.confidence();
        req.predictions[name] = pred;
    }

    // Convert max resources.
    if (proto_req.has_max_resources()) {
        req.has_max_resources = true;
        req.max_resources.cpu_millicores =
            proto_req.max_resources().cpu_millicores();
        req.max_resources.memory_mib =
            proto_req.max_resources().memory_mib();
    }

    return req;
}

pb::PlanResponse PlanResponseToProto(
    const adaptive::PlanResponse& response) {
    pb::PlanResponse proto_resp;
    for (const auto& alloc : response.tasks_to_start) {
        auto* ta = proto_resp.add_tasks_to_start();
        ta->set_task_name(alloc.task_name);
        ta->set_cpu_request_millicores(alloc.cpu_request_millicores);
        ta->set_memory_request_mib(alloc.memory_request_mib);
        ta->set_cpu_limit_millicores(alloc.cpu_limit_millicores);
        ta->set_memory_limit_mib(alloc.memory_limit_mib);
    }
    return proto_resp;
}

// ─────────────────────────────────────────────────────────────
// gRPC Service Implementation
// ─────────────────────────────────────────────────────────────

class OptimizerServiceImpl final
    : public pb::OptimizerService::Service {
public:
    OptimizerServiceImpl() : optimizer_() {}

    Status Plan(ServerContext* context,
                const pb::PlanRequest* request,
                pb::PlanResponse* response) override {
        std::cout << "[optimizer] Plan called for workflow '"
                  << request->workflow_name() << "', goal="
                  << request->optimization_goal() << ", "
                  << request->tasks_size() << " tasks" << std::endl;

        // Convert proto → internal types.
        adaptive::PlanRequest internal_req = ProtoToPlanRequest(*request);

        // Run the greedy optimizer.
        adaptive::PlanResponse internal_resp = optimizer_.Plan(internal_req);

        // Convert internal → proto response.
        *response = PlanResponseToProto(internal_resp);

        std::cout << "[optimizer] Plan result: "
                  << response->tasks_to_start_size()
                  << " tasks to start" << std::endl;

        return Status::OK;
    }

private:
    adaptive::GreedyOptimizer optimizer_;
};

// ─────────────────────────────────────────────────────────────
// Server startup
// ─────────────────────────────────────────────────────────────

std::unique_ptr<Server> g_server;

void SignalHandler(int signum) {
    std::cout << "\n[optimizer] Shutting down (signal " << signum
              << ")..." << std::endl;
    if (g_server) {
        g_server->Shutdown();
    }
}

int main(int argc, char* argv[]) {
    // Read port from environment.
    const char* port_env = std::getenv("GRPC_PORT");
    std::string port = port_env ? port_env : "50052";
    std::string server_address = "0.0.0.0:" + port;

    // Enable gRPC reflection for debugging with grpcurl.
    grpc::EnableDefaultHealthCheckService(true);
    grpc::reflection::InitProtoReflectionServerBuilderPlugin();

    OptimizerServiceImpl service;

    ServerBuilder builder;
    builder.AddListeningPort(server_address,
                             grpc::InsecureServerCredentials());
    builder.RegisterService(&service);

    g_server = builder.BuildAndStart();
    if (!g_server) {
        std::cerr << "[optimizer] Failed to start server on "
                  << server_address << std::endl;
        return 1;
    }

    // Register signal handlers for graceful shutdown.
    std::signal(SIGINT, SignalHandler);
    std::signal(SIGTERM, SignalHandler);

    std::cout << "[optimizer] C++ Optimizer gRPC server listening on "
              << server_address << std::endl;

    g_server->Wait();
    return 0;
}
