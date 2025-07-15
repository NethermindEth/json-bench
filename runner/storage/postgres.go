package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

// Database handles PostgreSQL operations for historic storage
type Database struct {
	db  *sql.DB
	cfg *config.PostgreSQLConfig
	log logrus.FieldLogger
}

// NewDatabase creates a new database connection
func NewDatabase(cfg *config.PostgreSQLConfig) (*Database, error) {
	db := &Database{
		cfg: cfg,
		log: logrus.WithField("component", "postgres"),
	}
	return db, nil
}

// Connect establishes database connection
func (d *Database) Connect() error {
	connStr := d.cfg.ConnectionString()

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(d.cfg.MaxOpenConns)
	db.SetMaxIdleConns(d.cfg.MaxIdleConns)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	d.db = db
	d.log.Info("Connected to PostgreSQL database")
	return nil
}

// InsertRun inserts a historic run into the database
func (d *Database) InsertRun(run *types.HistoricRun) error {
	query := `
		INSERT INTO benchmark_runs (
			id, timestamp, git_commit, git_branch, test_name, description,
			config_hash, result_path, duration, total_requests, success_rate,
			avg_latency, p95_latency, clients, methods, tags, is_baseline, baseline_name
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (id) DO UPDATE SET
			success_rate = EXCLUDED.success_rate,
			avg_latency = EXCLUDED.avg_latency,
			p95_latency = EXCLUDED.p95_latency`

	clientsJSON, _ := json.Marshal(run.Clients)
	methodsJSON, _ := json.Marshal(run.Methods)
	tagsJSON, _ := json.Marshal(run.Tags)

	_, err := d.db.Exec(query,
		run.ID, run.Timestamp, run.GitCommit, run.GitBranch, run.TestName,
		run.Description, run.ConfigHash, run.ResultPath, run.Duration,
		run.TotalRequests, run.SuccessRate, run.AvgLatency, run.P95Latency,
		clientsJSON, methodsJSON, tagsJSON, run.IsBaseline, run.BaselineName,
	)

	if err != nil {
		return fmt.Errorf("failed to insert run: %w", err)
	}

	d.log.WithField("run_id", run.ID).Debug("Inserted historic run")
	return nil
}

// InsertMetrics inserts time-series metrics into the database
func (d *Database) InsertMetrics(metrics []types.TimeSeriesMetric) error {
	if len(metrics) == 0 {
		return nil
	}

	query := `
		INSERT INTO benchmark_metrics (
			time, run_id, client, method, metric_name, value, tags
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`

	stmt, err := d.db.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, metric := range metrics {
		tagsJSON, _ := json.Marshal(metric.Tags)

		_, err := stmt.Exec(
			metric.Time, metric.RunID, metric.Client,
			metric.Method, metric.MetricName, metric.Value, tagsJSON,
		)
		if err != nil {
			return fmt.Errorf("failed to insert metric: %w", err)
		}
	}

	d.log.WithField("count", len(metrics)).Debug("Inserted metrics")
	return nil
}

// GetRun retrieves a run by ID
func (d *Database) GetRun(id string) (*types.HistoricRun, error) {
	query := `
		SELECT id, timestamp, git_commit, git_branch, test_name, description,
			config_hash, result_path, duration, total_requests, success_rate,
			avg_latency, p95_latency, clients, methods, tags, is_baseline, baseline_name
		FROM benchmark_runs WHERE id = $1`

	var run types.HistoricRun
	var clientsJSON, methodsJSON, tagsJSON []byte

	err := d.db.QueryRow(query, id).Scan(
		&run.ID, &run.Timestamp, &run.GitCommit, &run.GitBranch,
		&run.TestName, &run.Description, &run.ConfigHash, &run.ResultPath,
		&run.Duration, &run.TotalRequests, &run.SuccessRate,
		&run.AvgLatency, &run.P95Latency, &clientsJSON, &methodsJSON,
		&tagsJSON, &run.IsBaseline, &run.BaselineName,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("run not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get run: %w", err)
	}

	json.Unmarshal(clientsJSON, &run.Clients)
	json.Unmarshal(methodsJSON, &run.Methods)
	json.Unmarshal(tagsJSON, &run.Tags)

	// Populate the millisecond fields from the base fields
	// since the database only stores avg_latency, not avg_latency_ms
	run.AvgLatencyMs = run.AvgLatency
	run.P95LatencyMs = run.P95Latency
	run.P99LatencyMs = run.P95Latency // Using P95 as approximation since P99 is not stored
	run.MaxLatencyMs = run.P95Latency // Using P95 as approximation since Max is not stored
	run.OverallErrorRate = 100.0 - run.SuccessRate
	run.TotalErrors = int64(float64(run.TotalRequests) * (100.0 - run.SuccessRate) / 100.0)

	return &run, nil
}

// ListRuns lists runs with filtering
func (d *Database) ListRuns(filter types.RunFilter) ([]*types.HistoricRun, error) {
	query := `SELECT id, timestamp, git_commit, git_branch, test_name, description,
		total_requests, success_rate, avg_latency, p95_latency, is_baseline, baseline_name
		FROM benchmark_runs WHERE 1=1`

	args := []interface{}{}
	argCount := 1

	if filter.GitBranch != "" {
		query += fmt.Sprintf(" AND git_branch = $%d", argCount)
		args = append(args, filter.GitBranch)
		argCount++
	}

	if filter.TestName != "" {
		query += fmt.Sprintf(" AND test_name = $%d", argCount)
		args = append(args, filter.TestName)
		argCount++
	}

	if filter.IsBaseline != nil {
		query += fmt.Sprintf(" AND is_baseline = $%d", argCount)
		args = append(args, *filter.IsBaseline)
		argCount++
	}

	if !filter.Since.IsZero() {
		query += fmt.Sprintf(" AND timestamp >= $%d", argCount)
		args = append(args, filter.Since)
		argCount++
	}

	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list runs: %w", err)
	}
	defer rows.Close()

	var runs []*types.HistoricRun
	for rows.Next() {
		run := &types.HistoricRun{}
		err := rows.Scan(
			&run.ID, &run.Timestamp, &run.GitCommit, &run.GitBranch,
			&run.TestName, &run.Description, &run.TotalRequests,
			&run.SuccessRate, &run.AvgLatency, &run.P95Latency,
			&run.IsBaseline, &run.BaselineName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan run: %w", err)
		}

		// Populate the millisecond fields from the base fields
		// since the database only stores avg_latency, not avg_latency_ms
		run.AvgLatencyMs = run.AvgLatency
		run.P95LatencyMs = run.P95Latency
		run.P99LatencyMs = run.P95Latency // Using P95 as approximation since P99 is not stored
		run.MaxLatencyMs = run.P95Latency // Using P95 as approximation since Max is not stored
		run.OverallErrorRate = 100.0 - run.SuccessRate
		run.TotalErrors = int64(float64(run.TotalRequests) * (100.0 - run.SuccessRate) / 100.0)

		runs = append(runs, run)
	}

	return runs, rows.Err()
}

// UpdateBaseline updates baseline status of a run
func (d *Database) UpdateBaseline(runID, baselineName string) error {
	query := `UPDATE benchmark_runs SET is_baseline = true, baseline_name = $1 WHERE id = $2`
	_, err := d.db.Exec(query, baselineName, runID)
	if err != nil {
		return fmt.Errorf("failed to update baseline: %w", err)
	}
	return nil
}

// GetBaselines retrieves all baseline runs
func (d *Database) GetBaselines() ([]*types.HistoricRun, error) {
	filter := types.RunFilter{IsBaseline: &[]bool{true}[0]}
	return d.ListRuns(filter)
}

// DeleteOldRuns deletes runs older than the specified time
func (d *Database) DeleteOldRuns(before time.Time) error {
	query := `DELETE FROM benchmark_runs WHERE timestamp < $1 AND is_baseline = false`
	result, err := d.db.Exec(query, before)
	if err != nil {
		return fmt.Errorf("failed to delete old runs: %w", err)
	}

	count, _ := result.RowsAffected()
	d.log.WithField("deleted_count", count).Info("Deleted old runs")
	return nil
}

// QueryMetrics queries time-series metrics
func (d *Database) QueryMetrics(query types.MetricQuery) ([]types.TimeSeriesMetric, error) {
	sqlQuery := `SELECT time, run_id, client, method, metric_name, value, tags
		FROM benchmark_metrics WHERE 1=1`

	args := []interface{}{}
	argCount := 1

	if len(query.MetricNames) > 0 {
		sqlQuery += fmt.Sprintf(" AND metric_name = ANY($%d)", argCount)
		args = append(args, query.MetricNames)
		argCount++
	}

	if query.Client != "" {
		sqlQuery += fmt.Sprintf(" AND client = $%d", argCount)
		args = append(args, query.Client)
		argCount++
	}

	if query.Method != "" {
		sqlQuery += fmt.Sprintf(" AND method = $%d", argCount)
		args = append(args, query.Method)
		argCount++
	}

	if !query.Since.IsZero() {
		sqlQuery += fmt.Sprintf(" AND time >= $%d", argCount)
		args = append(args, query.Since)
		argCount++
	}

	sqlQuery += " ORDER BY time DESC"

	if query.Limit > 0 {
		sqlQuery += fmt.Sprintf(" LIMIT %d", query.Limit)
	}

	rows, err := d.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query metrics: %w", err)
	}
	defer rows.Close()

	var metrics []types.TimeSeriesMetric
	for rows.Next() {
		var metric types.TimeSeriesMetric
		var tagsJSON []byte

		err := rows.Scan(
			&metric.Time, &metric.RunID, &metric.Client,
			&metric.Method, &metric.MetricName, &metric.Value, &tagsJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan metric: %w", err)
		}

		if tagsJSON != nil {
			json.Unmarshal(tagsJSON, &metric.Tags)
		}

		metrics = append(metrics, metric)
	}

	return metrics, rows.Err()
}
