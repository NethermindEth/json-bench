package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/storage"
)

// MethodResponse represents the API response for methods
type MethodResponse struct {
	Name       string   `json:"name"`
	P99Latency *float64 `json:"p99_latency"`
	AvgLatency *float64 `json:"avg_latency"`
}

// TestP99DataFlow tests the complete p99 data flow from database to API
func TestP99DataFlow(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup test database connection
	db := setupTestDB(t)
	defer db.Close()

	// Ensure benchmark_metrics table exists
	err := storage.RunMigrations(db)
	require.NoError(t, err, "Failed to run migrations")

	// Create test handler function that uses the database
	handleGetRunMethods := func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		runID := vars["runID"]

		// Query method metrics directly from database
		query := `
			SELECT 
				method,
				MAX(CASE WHEN metric_name = 'latency_p99' THEN value END) as p99_latency,
				MAX(CASE WHEN metric_name = 'latency_avg' THEN value END) as avg_latency
			FROM benchmark_metrics
			WHERE run_id = $1 AND method != 'all'
			GROUP BY method
			ORDER BY method`

		rows, err := db.Query(query, runID)
		if err != nil {
			log.Printf("Failed to query methods: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var methods []MethodResponse
		for rows.Next() {
			var method string
			var p99, avg sql.NullFloat64

			err := rows.Scan(&method, &p99, &avg)
			if err != nil {
				log.Printf("Failed to scan row: %v", err)
				continue
			}

			methodResp := MethodResponse{
				Name: method,
			}

			if p99.Valid {
				methodResp.P99Latency = &p99.Float64
			}
			if avg.Valid {
				methodResp.AvgLatency = &avg.Float64
			}

			methods = append(methods, methodResp)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(methods)
	}

	// Setup router
	router := mux.NewRouter()
	router.HandleFunc("/api/runs/{runID}/methods", handleGetRunMethods).Methods("GET")

	// Create test server
	ts := httptest.NewServer(router)
	defer ts.Close()

	// Test data
	testRunID := fmt.Sprintf("test-run-%d", time.Now().Unix())
	testMethod := "eth_call"
	testP99Value := 100.5

	// Test 1: Insert test data with p99=100.5
	t.Run("Test_P99_With_Value", func(t *testing.T) {
		err := insertTestP99Metric(db, testRunID, testMethod, &testP99Value)
		require.NoError(t, err, "Failed to insert test metric")

		// Call API endpoint
		resp, err := http.Get(fmt.Sprintf("%s/api/runs/%s/methods", ts.URL, testRunID))
		require.NoError(t, err, "Failed to make API request")
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var methods []MethodResponse
		err = json.NewDecoder(resp.Body).Decode(&methods)
		require.NoError(t, err, "Failed to decode response")

		// Verify response contains p99=100.5
		found := false
		for _, method := range methods {
			if method.Name == testMethod {
				found = true
				assert.NotNil(t, method.P99Latency, "P99 latency should not be nil")
				if method.P99Latency != nil {
					assert.Equal(t, testP99Value, *method.P99Latency, "P99 value mismatch")
				}
			}
		}
		assert.True(t, found, "Method not found in response")
	})

	// Test 2: Insert test data with NULL p99
	t.Run("Test_P99_With_NULL", func(t *testing.T) {
		testMethodNull := "eth_getBalance"
		err := insertTestP99Metric(db, testRunID, testMethodNull, nil)
		require.NoError(t, err, "Failed to insert test metric with NULL p99")

		// Call API endpoint again
		resp, err := http.Get(fmt.Sprintf("%s/api/runs/%s/methods", ts.URL, testRunID))
		require.NoError(t, err, "Failed to make API request")
		defer resp.Body.Close()

		var methods []MethodResponse
		err = json.NewDecoder(resp.Body).Decode(&methods)
		require.NoError(t, err, "Failed to decode response")

		// Verify response contains p99=null (not 0)
		found := false
		for _, method := range methods {
			if method.Name == testMethodNull {
				found = true
				assert.Nil(t, method.P99Latency, "P99 latency should be nil for NULL value")
			}
		}
		assert.True(t, found, "Method with NULL p99 not found in response")
	})

	// Test 3: Verify zero values are preserved
	t.Run("Test_P99_With_Zero", func(t *testing.T) {
		testMethodZero := "eth_blockNumber"
		zeroValue := 0.0
		err := insertTestP99Metric(db, testRunID, testMethodZero, &zeroValue)
		require.NoError(t, err, "Failed to insert test metric with zero p99")

		// Call API endpoint
		resp, err := http.Get(fmt.Sprintf("%s/api/runs/%s/methods", ts.URL, testRunID))
		require.NoError(t, err, "Failed to make API request")
		defer resp.Body.Close()

		var methods []MethodResponse
		err = json.NewDecoder(resp.Body).Decode(&methods)
		require.NoError(t, err, "Failed to decode response")

		// Verify response contains p99=0 (not null)
		found := false
		for _, method := range methods {
			if method.Name == testMethodZero {
				found = true
				assert.NotNil(t, method.P99Latency, "P99 latency should not be nil for zero value")
				if method.P99Latency != nil {
					assert.Equal(t, zeroValue, *method.P99Latency, "P99 zero value should be preserved")
				}
			}
		}
		assert.True(t, found, "Method with zero p99 not found in response")
	})

	// Cleanup test data
	cleanupTestData(db, testRunID)
}

// setupTestDB creates a test database connection
func setupTestDB(t *testing.T) *sql.DB {
	// Use test database connection string
	// This should be configured in your test environment
	connStr := "postgres://postgres:postgres@localhost:5432/jsonrpc_bench_test?sslmode=disable"

	db, err := sql.Open("postgres", connStr)
	require.NoError(t, err, "Failed to open database connection")

	err = db.Ping()
	require.NoError(t, err, "Failed to ping database")

	return db
}

// insertTestP99Metric inserts a test p99 metric into the database
func insertTestP99Metric(db *sql.DB, runID string, method string, p99 *float64) error {
	// First, ensure the run exists in benchmark_runs table
	runQuery := `
		INSERT INTO benchmark_runs (id, timestamp, test_name, config_hash)
		VALUES ($1, $2, 'integration_test', 'test_hash')
		ON CONFLICT (id) DO NOTHING
	`
	_, err := db.Exec(runQuery, runID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to insert test run: %w", err)
	}

	// Insert the metric
	if p99 == nil {
		// Insert NULL value
		query := `
			INSERT INTO benchmark_metrics (time, run_id, client, method, metric_name, value)
			VALUES ($1, $2, 'test_client', $3, 'latency_p99', NULL)
		`
		_, err = db.Exec(query, time.Now(), runID, method)
	} else {
		// Insert actual value
		query := `
			INSERT INTO benchmark_metrics (time, run_id, client, method, metric_name, value)
			VALUES ($1, $2, 'test_client', $3, 'latency_p99', $4)
		`
		_, err = db.Exec(query, time.Now(), runID, method, *p99)
	}

	if err != nil {
		return fmt.Errorf("failed to insert test metric: %w", err)
	}

	// Also insert avg_latency for completeness
	avgQuery := `
		INSERT INTO benchmark_metrics (time, run_id, client, method, metric_name, value)
		VALUES ($1, $2, 'test_client', $3, 'latency_avg', $4)
	`
	avgValue := 50.0
	if p99 != nil && *p99 > 0 {
		avgValue = *p99 / 2 // Make avg half of p99
	}
	_, err = db.Exec(avgQuery, time.Now(), runID, method, avgValue)

	return err
}

// cleanupTestData removes test data from the database
func cleanupTestData(db *sql.DB, runID string) {
	// Delete metrics
	db.Exec("DELETE FROM benchmark_metrics WHERE run_id = $1", runID)
	// Delete run
	db.Exec("DELETE FROM benchmark_runs WHERE id = $1", runID)
}
