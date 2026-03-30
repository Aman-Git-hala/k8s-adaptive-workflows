/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package statedb provides a client for the PostgreSQL State DB
// that stores historical execution metrics for workflow tasks.
// The Inference Engine uses this data to improve future predictions.
package statedb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// TaskExecution represents the observed metrics from a single task run.
type TaskExecution struct {
	WorkflowName string
	TaskName     string
	Image        string
	ActualCPU    int64   // millicores
	ActualMemMiB int64   // MiB
	DurationS    float64 // seconds
	Succeeded    bool
	CompletedAt  time.Time
}

// HistoricalStats holds aggregated historical metrics for an image.
type HistoricalStats struct {
	Image       string
	AvgCPU      float64
	AvgMemMiB   float64
	AvgDuration float64
	SampleCount int
}

// StateDB defines the interface for recording and querying execution metrics.
type StateDB interface {
	// RecordExecution writes a completed task's metrics to the database.
	RecordExecution(ctx context.Context, exec TaskExecution) error

	// GetHistoryByImage returns aggregated stats for a given container image.
	GetHistoryByImage(ctx context.Context, image string) (*HistoricalStats, error)

	// Close closes the database connection.
	Close() error
}

// PostgresStateDB is a PostgreSQL-backed StateDB implementation.
type PostgresStateDB struct {
	db *sql.DB
}

// NewPostgresStateDB creates a new PostgresStateDB from a connection string.
// Example connStr: "postgres://user:pass@localhost:5432/adaptiveworkflows?sslmode=disable"
func NewPostgresStateDB(connStr string) (*PostgresStateDB, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test the connection.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresStateDB{db: db}, nil
}

// RecordExecution writes a completed task's metrics to PostgreSQL.
func (p *PostgresStateDB) RecordExecution(ctx context.Context, exec TaskExecution) error {
	query := `
		INSERT INTO task_executions (workflow_name, task_name, image, actual_cpu_m, actual_mem_mib, duration_s, succeeded, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := p.db.ExecContext(ctx, query,
		exec.WorkflowName,
		exec.TaskName,
		exec.Image,
		exec.ActualCPU,
		exec.ActualMemMiB,
		exec.DurationS,
		exec.Succeeded,
		exec.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert task execution: %w", err)
	}
	return nil
}

// GetHistoryByImage returns aggregated stats for a given container image.
// Only considers successful executions from the last 30 days.
func (p *PostgresStateDB) GetHistoryByImage(ctx context.Context, image string) (*HistoricalStats, error) {
	query := `
		SELECT
			COALESCE(AVG(actual_cpu_m), 0),
			COALESCE(AVG(actual_mem_mib), 0),
			COALESCE(AVG(duration_s), 0),
			COUNT(*)
		FROM task_executions
		WHERE image = $1
		  AND succeeded = TRUE
		  AND completed_at > NOW() - INTERVAL '30 days'
	`

	stats := &HistoricalStats{Image: image}
	err := p.db.QueryRowContext(ctx, query, image).Scan(
		&stats.AvgCPU,
		&stats.AvgMemMiB,
		&stats.AvgDuration,
		&stats.SampleCount,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query history for image %q: %w", image, err)
	}

	return stats, nil
}

// Close closes the database connection.
func (p *PostgresStateDB) Close() error {
	return p.db.Close()
}

// NoOpStateDB is a no-op implementation used when PostgreSQL is not configured.
type NoOpStateDB struct{}

// NewNoOpStateDB creates a no-op StateDB that silently discards metrics.
func NewNoOpStateDB() *NoOpStateDB {
	return &NoOpStateDB{}
}

// RecordExecution is a no-op.
func (n *NoOpStateDB) RecordExecution(_ context.Context, _ TaskExecution) error {
	return nil
}

// GetHistoryByImage always returns zero stats with the no-op DB.
func (n *NoOpStateDB) GetHistoryByImage(_ context.Context, image string) (*HistoricalStats, error) {
	return &HistoricalStats{Image: image}, nil
}

// Close is a no-op.
func (n *NoOpStateDB) Close() error {
	return nil
}
