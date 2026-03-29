#!/usr/bin/env python3
"""
train_model.py — Train a simple RandomForestRegressor on synthetic data,
then export it as an ONNX model for the Inference Engine.

Features:
  - image_hash: hash of the container image name (numeric feature)
  - hist_avg_cpu: historical average CPU usage in millicores
  - hist_avg_mem: historical average memory usage in MiB
  - hist_avg_duration: historical average duration in seconds

Targets:
  - predicted_cpu: predicted CPU in millicores
  - predicted_mem: predicted memory in MiB
  - predicted_duration: predicted wall-clock duration in seconds

Usage:
  python train_model.py [--samples N] [--output model.onnx]
"""

import argparse
import hashlib

import numpy as np
from sklearn.ensemble import RandomForestRegressor
from sklearn.model_selection import train_test_split
from skl2onnx import convert_sklearn
from skl2onnx.common.data_types import FloatTensorType


def image_hash(name: str) -> float:
    """Deterministic numeric hash of an image name."""
    h = hashlib.sha256(name.encode()).hexdigest()
    return int(h[:8], 16) / (16**8)  # Normalize to [0, 1]


def generate_synthetic_data(n_samples: int = 5000, seed: int = 42):
    """
    Generate synthetic training data simulating task execution metrics.

    The synthetic data models realistic patterns:
    - Heavy images (tensorflow, pytorch) → high CPU/memory, long duration
    - Light images (alpine, busybox) → low CPU/memory, short duration
    - Noisy variations to model real-world variance
    """
    rng = np.random.default_rng(seed)

    images = [
        "alpine:latest", "busybox:latest", "python:3.12-slim",
        "node:20-slim", "golang:1.22", "tensorflow/tensorflow:latest",
        "pytorch/pytorch:latest", "redis:7-alpine", "nginx:latest",
        "ubuntu:22.04", "gcc:latest", "openjdk:21-slim",
    ]

    # Base profiles (cpu_m, mem_mib, duration_s)
    profiles = {
        "alpine:latest": (50, 32, 5),
        "busybox:latest": (30, 16, 3),
        "python:3.12-slim": (200, 256, 30),
        "node:20-slim": (150, 192, 25),
        "golang:1.22": (300, 512, 45),
        "tensorflow/tensorflow:latest": (1000, 2048, 120),
        "pytorch/pytorch:latest": (800, 1536, 100),
        "redis:7-alpine": (100, 64, 10),
        "nginx:latest": (80, 48, 8),
        "ubuntu:22.04": (120, 128, 15),
        "gcc:latest": (400, 384, 60),
        "openjdk:21-slim": (250, 320, 35),
    }

    X = []
    y = []

    for _ in range(n_samples):
        img = rng.choice(images)
        base_cpu, base_mem, base_dur = profiles[img]

        # Simulate historical averages (noisy version of true profile)
        hist_cpu = max(10, base_cpu + rng.normal(0, base_cpu * 0.2))
        hist_mem = max(8, base_mem + rng.normal(0, base_mem * 0.2))
        hist_dur = max(1, base_dur + rng.normal(0, base_dur * 0.2))

        # Features
        features = [
            image_hash(img),
            hist_cpu,
            hist_mem,
            hist_dur,
        ]
        X.append(features)

        # Actual observed values (targets) — slightly different from historical
        actual_cpu = max(10, base_cpu + rng.normal(0, base_cpu * 0.15))
        actual_mem = max(8, base_mem + rng.normal(0, base_mem * 0.15))
        actual_dur = max(1, base_dur + rng.normal(0, base_dur * 0.15))
        y.append([actual_cpu, actual_mem, actual_dur])

    return np.array(X, dtype=np.float32), np.array(y, dtype=np.float32)


def main():
    parser = argparse.ArgumentParser(description="Train inference model")
    parser.add_argument("--samples", type=int, default=5000, help="Number of training samples")
    parser.add_argument("--output", type=str, default="model.onnx", help="Output ONNX file path")
    args = parser.parse_args()

    print(f"Generating {args.samples} synthetic samples...")
    X, y = generate_synthetic_data(args.samples)

    X_train, X_test, y_train, y_test = train_test_split(X, y, test_size=0.2, random_state=42)

    print("Training RandomForestRegressor...")
    model = RandomForestRegressor(
        n_estimators=100,
        max_depth=10,
        random_state=42,
        n_jobs=-1,
    )
    model.fit(X_train, y_train)

    # Evaluate
    score = model.score(X_test, y_test)
    print(f"R² score on test set: {score:.4f}")

    # Export to ONNX
    print(f"Exporting to {args.output}...")
    initial_type = [("float_input", FloatTensorType([None, 4]))]
    onnx_model = convert_sklearn(model, initial_types=initial_type)

    with open(args.output, "wb") as f:
        f.write(onnx_model.SerializeToString())

    print(f"Model saved to {args.output}")
    print(f"  Input shape:  (batch, 4) — [image_hash, hist_cpu, hist_mem, hist_duration]")
    print(f"  Output shape: (batch, 3) — [pred_cpu, pred_mem, pred_duration]")


if __name__ == "__main__":
    main()
