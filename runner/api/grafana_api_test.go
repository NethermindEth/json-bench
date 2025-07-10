package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// Helper function to setup Grafana API for testing
func setupGrafanaAPI() (*grafanaAPI, *MockHistoricStorage, *MockDB) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel) // Reduce noise in tests

	storage := &MockHistoricStorage{}
	db := &MockDB{}

	api := &grafanaAPI{
		storage: storage,
		db:      db,
		log:     log.WithField("component", "grafana-api"),
	}

	return api, storage, db
}

// Test Grafana API creation and lifecycle

func TestNewGrafanaAPI(t *testing.T) {
	log := logrus.New()
	storage := &MockHistoricStorage{}
	db := &MockDB{}

	api := NewGrafanaAPI(storage, db, log)

	assert.NotNil(t, api)
	assert.Implements(t, (*GrafanaAPI)(nil), api)
}

func TestGrafanaAPIStartStop(t *testing.T) {
	api, _, _ := setupGrafanaAPI()

	ctx := context.Background()

	// Test Start
	err := api.Start(ctx)
	assert.NoError(t, err)

	// Test Stop
	err = api.Stop()
	assert.NoError(t, err)
}

// Test connection endpoint

func TestHandleGrafanaTestConnection(t *testing.T) {
	tests := []struct {
		name           string
		dbPingError    error
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "successful connection",
			dbPingError:    nil,
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name:           "database connection failed",
			dbPingError:    fmt.Errorf("connection failed"),
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   "Database connection failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, _, mockDB := setupGrafanaAPI()

			mockDB.On("Ping").Return(tt.dbPingError)

			req := httptest.NewRequest("GET", "/grafana/", nil)
			w := httptest.NewRecorder()

			api.HandleGrafanaTestConnection(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			assert.Contains(t, w.Body.String(), tt.expectedBody)

			mockDB.AssertExpectations(t)
		})
	}
}

// Test search endpoint

func TestHandleGrafanaSearch(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    interface{}
		mockRows       []string
		dbError        error
		expectedStatus int
		expectedCount  int
	}{
		{
			name: "successful search",
			requestBody: GrafanaSearchRequest{
				Target: "test",
			},
			mockRows:       []string{"test-benchmark", "another-test"},
			dbError:        nil,
			expectedStatus: http.StatusOK,
			expectedCount:  8, // 2 tests * 4 metric types
		},
		{
			name: "empty search",
			requestBody: GrafanaSearchRequest{
				Target: "",
			},
			mockRows:       []string{"test-benchmark"},
			dbError:        nil,
			expectedStatus: http.StatusOK,
			expectedCount:  4, // 1 test * 4 metric types
		},
		{
			name:           "invalid JSON",
			requestBody:    "invalid json",
			mockRows:       nil,
			dbError:        nil,
			expectedStatus: http.StatusBadRequest,
			expectedCount:  0,
		},
		{
			name: "database error",
			requestBody: GrafanaSearchRequest{
				Target: "test",
			},
			mockRows:       nil,
			dbError:        fmt.Errorf("database error"),
			expectedStatus: http.StatusInternalServerError,
			expectedCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, _, mockDB := setupGrafanaAPI()

			// Setup database mock expectations
			if tt.dbError == nil && tt.mockRows != nil {
				// Mock test names query
				rows := &MockRows{
					data: make([][]interface{}, len(tt.mockRows)),
				}
				for i, testName := range tt.mockRows {
					rows.data[i] = []interface{}{testName}
				}
				mockDB.On("QueryContext", mock.Anything, mock.MatchedBy(func(query string) bool {
					return query == "SELECT DISTINCT test_name FROM historic_runs ORDER BY test_name"
				})).Return(rows, nil)

				// Mock client names query
				clientRows := &MockRows{
					data: [][]interface{}{
						{"geth"},
						{"besu"},
					},
				}
				mockDB.On("QueryContext", mock.Anything, mock.MatchedBy(func(query string) bool {
					return query != "SELECT DISTINCT test_name FROM historic_runs ORDER BY test_name"
				}), mock.Anything).Return(clientRows, nil)
			} else if tt.dbError != nil {
				mockDB.On("QueryContext", mock.Anything, mock.AnythingOfType("string")).Return(nil, tt.dbError)
			}

			var body []byte
			if req, ok := tt.requestBody.(GrafanaSearchRequest); ok {
				body, _ = json.Marshal(req)
			} else {
				body = []byte(tt.requestBody.(string))
			}

			req := httptest.NewRequest("POST", "/grafana/search", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			api.HandleGrafanaSearch(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var metrics []string
				err := json.Unmarshal(w.Body.Bytes(), &metrics)
				require.NoError(t, err)
				assert.Len(t, metrics, tt.expectedCount)
			}

			mockDB.AssertExpectations(t)
		})
	}
}

// Test query endpoint

func TestHandleGrafanaQuery(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    interface{}
		mockData       [][]interface{}
		dbError        error
		expectedStatus int
		expectedCount  int
	}{
		{
			name: "successful time series query",
			requestBody: GrafanaQueryRequest{
				Range: GrafanaRange{
					From: "2023-01-01T00:00:00Z",
					To:   "2023-01-02T00:00:00Z",
				},
				Targets: []GrafanaTarget{
					{
						Target: "test-benchmark.overall.avg_latency",
						Type:   "timeserie",
					},
				},
			},
			mockData: [][]interface{}{
				{time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC), 45.5},
				{time.Date(2023, 1, 1, 13, 0, 0, 0, time.UTC), 47.2},
			},
			dbError:        nil,
			expectedStatus: http.StatusOK,
			expectedCount:  1, // 1 target
		},
		{
			name: "successful table query",
			requestBody: GrafanaQueryRequest{
				Range: GrafanaRange{
					From: "2023-01-01T00:00:00Z",
					To:   "2023-01-02T00:00:00Z",
				},
				Targets: []GrafanaTarget{
					{
						Target: "test-benchmark.overall.avg_latency",
						Type:   "table",
					},
				},
			},
			mockData: [][]interface{}{
				{
					time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
					"test-benchmark", "abc123", "main",
					int64(1000), int64(10), 0.01,
					45.5, 89.2, 125.8, "geth",
				},
			},
			dbError:        nil,
			expectedStatus: http.StatusOK,
			expectedCount:  1,
		},
		{
			name:           "invalid JSON",
			requestBody:    "invalid json",
			mockData:       nil,
			dbError:        nil,
			expectedStatus: http.StatusBadRequest,
			expectedCount:  0,
		},
		{
			name: "invalid time format",
			requestBody: GrafanaQueryRequest{
				Range: GrafanaRange{
					From: "invalid-time",
					To:   "2023-01-02T00:00:00Z",
				},
				Targets: []GrafanaTarget{
					{Target: "test.overall.avg_latency"},
				},
			},
			mockData:       nil,
			dbError:        nil,
			expectedStatus: http.StatusBadRequest,
			expectedCount:  0,
		},
		{
			name: "database error",
			requestBody: GrafanaQueryRequest{
				Range: GrafanaRange{
					From: "2023-01-01T00:00:00Z",
					To:   "2023-01-02T00:00:00Z",
				},
				Targets: []GrafanaTarget{
					{Target: "test.overall.avg_latency"},
				},
			},
			mockData:       nil,
			dbError:        fmt.Errorf("database error"),
			expectedStatus: http.StatusOK, // Query endpoint handles individual target errors gracefully
			expectedCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, _, mockDB := setupGrafanaAPI()

			// Setup database mock expectations
			if tt.dbError == nil && tt.mockData != nil {
				rows := &MockRows{data: tt.mockData}
				mockDB.On("QueryContext", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return(rows, nil)
			} else if tt.dbError != nil {
				mockDB.On("QueryContext", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return(nil, tt.dbError)
			}

			var body []byte
			if req, ok := tt.requestBody.(GrafanaQueryRequest); ok {
				body, _ = json.Marshal(req)
			} else {
				body = []byte(tt.requestBody.(string))
			}

			req := httptest.NewRequest("POST", "/grafana/query", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			api.HandleGrafanaQuery(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var response []interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Len(t, response, tt.expectedCount)
			}

			if tt.dbError != nil || (tt.dbError == nil && tt.mockData != nil) {
				mockDB.AssertExpectations(t)
			}
		})
	}
}

// Test annotations endpoint

func TestHandleGrafanaAnnotations(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    interface{}
		mockData       map[string][][]interface{}
		dbError        error
		expectedStatus int
		minAnnotations int
	}{
		{
			name: "successful annotations query",
			requestBody: GrafanaAnnotationRequest{
				Range: GrafanaRange{
					From: "2023-01-01T00:00:00Z",
					To:   "2023-01-02T00:00:00Z",
				},
				Annotation: GrafanaAnnotationQuery{
					Name: "regressions",
				},
			},
			mockData: map[string][][]interface{}{
				"regressions": {
					{"reg1", "run1", "geth", "p95_latency", "high", time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC), "test-benchmark"},
				},
				"baselines": {
					{"baseline1", "run1", "test-benchmark", time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC), "abc123"},
				},
				"deployments": {
					{"run1", "test-benchmark", "abc123", "main", time.Date(2023, 1, 1, 9, 0, 0, 0, time.UTC), "geth"},
				},
			},
			dbError:        nil,
			expectedStatus: http.StatusOK,
			minAnnotations: 1,
		},
		{
			name: "all annotations types",
			requestBody: GrafanaAnnotationRequest{
				Range: GrafanaRange{
					From: "2023-01-01T00:00:00Z",
					To:   "2023-01-02T00:00:00Z",
				},
				Annotation: GrafanaAnnotationQuery{
					Name: "", // Empty name should return all types
				},
			},
			mockData: map[string][][]interface{}{
				"regressions": {
					{"reg1", "run1", "geth", "p95_latency", "high", time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC), "test-benchmark"},
				},
				"baselines": {
					{"baseline1", "run1", "test-benchmark", time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC), "abc123"},
				},
				"deployments": {
					{"run1", "test-benchmark", "abc123", "main", time.Date(2023, 1, 1, 9, 0, 0, 0, time.UTC), "geth"},
				},
			},
			dbError:        nil,
			expectedStatus: http.StatusOK,
			minAnnotations: 3,
		},
		{
			name:           "invalid JSON",
			requestBody:    "invalid json",
			mockData:       nil,
			dbError:        nil,
			expectedStatus: http.StatusBadRequest,
			minAnnotations: 0,
		},
		{
			name: "invalid time format",
			requestBody: GrafanaAnnotationRequest{
				Range: GrafanaRange{
					From: "invalid-time",
					To:   "2023-01-02T00:00:00Z",
				},
				Annotation: GrafanaAnnotationQuery{
					Name: "regressions",
				},
			},
			mockData:       nil,
			dbError:        nil,
			expectedStatus: http.StatusBadRequest,
			minAnnotations: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, _, mockDB := setupGrafanaAPI()

			// Setup database mock expectations
			if tt.dbError == nil && tt.mockData != nil {
				// Mock regressions query
				if data, exists := tt.mockData["regressions"]; exists {
					rows := &MockRows{data: data}
					mockDB.On("QueryContext", mock.Anything, mock.MatchedBy(func(query string) bool {
						return contains(query, "regressions")
					}), mock.Anything).Return(rows, nil)
				}

				// Mock baselines query
				if data, exists := tt.mockData["baselines"]; exists {
					rows := &MockRows{data: data}
					mockDB.On("QueryContext", mock.Anything, mock.MatchedBy(func(query string) bool {
						return contains(query, "baselines")
					}), mock.Anything).Return(rows, nil)
				}

				// Mock deployments query
				if data, exists := tt.mockData["deployments"]; exists {
					rows := &MockRows{data: data}
					mockDB.On("QueryContext", mock.Anything, mock.MatchedBy(func(query string) bool {
						return contains(query, "historic_runs") && contains(query, "git_commit")
					}), mock.Anything).Return(rows, nil)
				}
			} else if tt.dbError != nil {
				mockDB.On("QueryContext", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return(nil, tt.dbError)
			}

			var body []byte
			if req, ok := tt.requestBody.(GrafanaAnnotationRequest); ok {
				body, _ = json.Marshal(req)
			} else {
				body = []byte(tt.requestBody.(string))
			}

			req := httptest.NewRequest("POST", "/grafana/annotations", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			api.HandleGrafanaAnnotations(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var annotations []GrafanaAnnotation
				err := json.Unmarshal(w.Body.Bytes(), &annotations)
				require.NoError(t, err)
				assert.GreaterOrEqual(t, len(annotations), tt.minAnnotations)

				// Verify annotation structure
				for _, annotation := range annotations {
					assert.NotEmpty(t, annotation.Title)
					assert.NotZero(t, annotation.Time)
					assert.NotNil(t, annotation.Annotation)
				}
			}

			if tt.mockData != nil || tt.dbError != nil {
				mockDB.AssertExpectations(t)
			}
		})
	}
}

// Test metrics metadata endpoint

func TestHandleGrafanaMetrics(t *testing.T) {
	api, _, _ := setupGrafanaAPI()

	req := httptest.NewRequest("GET", "/grafana/metrics", nil)
	w := httptest.NewRecorder()

	api.HandleGrafanaMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var metadata []MetricMetadata
	err := json.Unmarshal(w.Body.Bytes(), &metadata)
	require.NoError(t, err)

	assert.NotEmpty(t, metadata)

	// Verify base metrics are present
	baseMetrics := []string{"avg_latency", "p95_latency", "p99_latency", "error_rate", "throughput"}
	foundMetrics := make(map[string]bool)
	for _, meta := range metadata {
		for _, base := range baseMetrics {
			if meta.Name == base {
				foundMetrics[base] = true
				assert.NotEmpty(t, meta.Type)
				assert.NotEmpty(t, meta.Help)
				assert.NotEmpty(t, meta.Unit)
				assert.NotEmpty(t, meta.Labels)
			}
		}
	}

	for _, base := range baseMetrics {
		assert.True(t, foundMetrics[base], "Base metric %s should be present", base)
	}
}

// Test data formatting methods

func TestFormatGrafanaTimeSeries(t *testing.T) {
	api, _, _ := setupGrafanaAPI()

	data := []TimeSeriesDataPoint{
		{
			Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
			Value:     45.5,
		},
		{
			Timestamp: time.Date(2023, 1, 1, 13, 0, 0, 0, time.UTC),
			Value:     47.2,
		},
	}

	result := api.FormatGrafanaTimeSeries(data, "test.metric")

	assert.Equal(t, "test.metric", result.Target)
	assert.Len(t, result.DataPoints, 2)

	// Verify data point format [value, timestamp_ms]
	assert.Equal(t, 45.5, result.DataPoints[0][0])
	assert.Equal(t, int64(1672574400000), result.DataPoints[0][1]) // Unix timestamp in ms

	assert.Equal(t, 47.2, result.DataPoints[1][0])
	assert.Equal(t, int64(1672578000000), result.DataPoints[1][1])
}

func TestFormatGrafanaTable(t *testing.T) {
	api, _, _ := setupGrafanaAPI()

	columns := []TableColumn{
		{Text: "Time", Type: "time"},
		{Text: "Value", Type: "number"},
		{Text: "Client", Type: "string"},
	}

	data := []TableRow{
		{Values: []interface{}{1672574400000, 45.5, "geth"}},
		{Values: []interface{}{1672578000000, 47.2, "besu"}},
	}

	result := api.FormatGrafanaTable(data, columns)

	assert.Equal(t, "table", result.Type)
	assert.Len(t, result.Columns, 3)
	assert.Len(t, result.Rows, 2)

	assert.Equal(t, "Time", result.Columns[0].Text)
	assert.Equal(t, "time", result.Columns[0].Type)

	assert.Equal(t, []interface{}{1672574400000, 45.5, "geth"}, result.Rows[0])
	assert.Equal(t, []interface{}{1672578000000, 47.2, "besu"}, result.Rows[1])
}

// Test helper methods

func TestParseMetricTarget(t *testing.T) {
	api, _, _ := setupGrafanaAPI()

	tests := []struct {
		name     string
		target   string
		expected *MetricInfo
	}{
		{
			name:   "basic metric",
			target: "test-benchmark.geth.avg_latency",
			expected: &MetricInfo{
				OriginalTarget: "test-benchmark.geth.avg_latency",
				TestName:       "test-benchmark",
				Client:         "geth",
				MetricType:     "avg_latency",
			},
		},
		{
			name:   "metric with aggregation",
			target: "rate(test-benchmark.overall.error_rate)",
			expected: &MetricInfo{
				OriginalTarget: "test-benchmark.overall.error_rate",
				TestName:       "test-benchmark",
				Client:         "overall",
				MetricType:     "error_rate",
				Aggregation:    "rate",
			},
		},
		{
			name:     "invalid format",
			target:   "invalid.format",
			expected: nil,
		},
		{
			name:     "empty target",
			target:   "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := api.parseMetricTarget(tt.target)

			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expected.OriginalTarget, result.OriginalTarget)
				assert.Equal(t, tt.expected.TestName, result.TestName)
				assert.Equal(t, tt.expected.Client, result.Client)
				assert.Equal(t, tt.expected.MetricType, result.MetricType)
				assert.Equal(t, tt.expected.Aggregation, result.Aggregation)
			}
		})
	}
}

func TestParseGrafanaTime(t *testing.T) {
	api, _, _ := setupGrafanaAPI()

	tests := []struct {
		name        string
		timeStr     string
		expectError bool
		expected    time.Time
	}{
		{
			name:        "RFC3339 format",
			timeStr:     "2023-01-01T12:00:00Z",
			expectError: false,
			expected:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name:        "Unix timestamp seconds",
			timeStr:     "1672574400",
			expectError: false,
			expected:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name:        "Unix timestamp milliseconds",
			timeStr:     "1672574400000",
			expectError: false,
			expected:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name:        "invalid format",
			timeStr:     "invalid-time",
			expectError: true,
		},
		{
			name:        "empty string",
			timeStr:     "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := api.parseGrafanaTime(tt.timeStr)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected.Unix(), result.Unix())
			}
		})
	}
}

func TestMatchesSearch(t *testing.T) {
	api, _, _ := setupGrafanaAPI()

	tests := []struct {
		name     string
		metric   string
		search   string
		expected bool
	}{
		{
			name:     "exact match",
			metric:   "test.geth.avg_latency",
			search:   "test.geth.avg_latency",
			expected: true,
		},
		{
			name:     "partial match",
			metric:   "test.geth.avg_latency",
			search:   "geth",
			expected: true,
		},
		{
			name:     "case insensitive",
			metric:   "test.geth.avg_latency",
			search:   "GETH",
			expected: true,
		},
		{
			name:     "wildcard match",
			metric:   "test.geth.avg_latency",
			search:   "test.*latency",
			expected: true,
		},
		{
			name:     "no match",
			metric:   "test.geth.avg_latency",
			search:   "besu",
			expected: false,
		},
		{
			name:     "empty search matches all",
			metric:   "test.geth.avg_latency",
			search:   "",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := api.matchesSearch(tt.metric, tt.search)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApplyAggregation(t *testing.T) {
	api, _, _ := setupGrafanaAPI()

	tests := []struct {
		name           string
		aggregation    string
		value          float64
		existingPoints [][]interface{}
		expected       float64
	}{
		{
			name:           "rate calculation",
			aggregation:    "rate",
			value:          100.0,
			existingPoints: [][]interface{}{{50.0, int64(1672574400000)}},
			expected:       50.0,
		},
		{
			name:           "delta calculation",
			aggregation:    "delta",
			value:          75.0,
			existingPoints: [][]interface{}{{50.0, int64(1672574400000)}},
			expected:       25.0,
		},
		{
			name:           "count aggregation",
			aggregation:    "count",
			value:          123.45,
			existingPoints: [][]interface{}{},
			expected:       1.0,
		},
		{
			name:           "no aggregation",
			aggregation:    "",
			value:          42.5,
			existingPoints: [][]interface{}{},
			expected:       42.5,
		},
		{
			name:           "unknown aggregation",
			aggregation:    "unknown",
			value:          42.5,
			existingPoints: [][]interface{}{},
			expected:       42.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := api.applyAggregation(tt.aggregation, tt.value, tt.existingPoints)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetSeverityColor(t *testing.T) {
	api, _, _ := setupGrafanaAPI()

	tests := []struct {
		severity string
		expected string
	}{
		{"critical", "red"},
		{"major", "orange"},
		{"minor", "yellow"},
		{"unknown", "blue"},
		{"", "blue"},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			result := api.getSeverityColor(tt.severity)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test response writing methods

func TestWriteGrafanaResponse(t *testing.T) {
	api, _, _ := setupGrafanaAPI()

	testData := map[string]interface{}{
		"test": "data",
		"num":  42,
	}

	w := httptest.NewRecorder()
	api.writeGrafanaResponse(w, http.StatusOK, testData)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Content-Type, Authorization", w.Header().Get("Access-Control-Allow-Headers"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "data", response["test"])
	assert.Equal(t, float64(42), response["num"])
}

func TestWriteGrafanaErrorResponse(t *testing.T) {
	api, _, _ := setupGrafanaAPI()

	w := httptest.NewRecorder()
	api.writeGrafanaErrorResponse(w, http.StatusBadRequest, "Test error message")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "Test error message", response["error"])
	assert.Equal(t, "Test error message", response["message"])
	assert.Equal(t, float64(400), response["status"])
}

// Mock implementation for sql.Rows
type MockRows struct {
	data    [][]interface{}
	index   int
	columns []string
	err     error
}

func (m *MockRows) Close() error {
	return nil
}

func (m *MockRows) Columns() ([]string, error) {
	if m.columns != nil {
		return m.columns, nil
	}
	return []string{"col1", "col2", "col3", "col4", "col5", "col6", "col7"}, nil
}

func (m *MockRows) Err() error {
	return m.err
}

func (m *MockRows) Next() bool {
	return m.index < len(m.data)
}

func (m *MockRows) Scan(dest ...interface{}) error {
	if m.index >= len(m.data) {
		return fmt.Errorf("no more rows")
	}

	row := m.data[m.index]
	m.index++

	for i, val := range dest {
		if i < len(row) {
			switch v := dest[i].(type) {
			case *string:
				if str, ok := row[i].(string); ok {
					*v = str
				} else {
					*v = fmt.Sprintf("%v", row[i])
				}
			case *time.Time:
				if t, ok := row[i].(time.Time); ok {
					*v = t
				}
			case *float64:
				if f, ok := row[i].(float64); ok {
					*v = f
				} else if i, ok := row[i].(int); ok {
					*v = float64(i)
				}
			case *int64:
				if i, ok := row[i].(int64); ok {
					*v = i
				} else if i, ok := row[i].(int); ok {
					*v = int64(i)
				}
			case *int:
				if i, ok := row[i].(int); ok {
					*v = i
				} else if i, ok := row[i].(int64); ok {
					*v = int(i)
				}
			}
		}
	}

	return nil
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(substr) > 0 && len(s) > 0 &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				(len(s) > len(substr) && findInString(s, substr)))))
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Benchmark tests

func BenchmarkHandleGrafanaQuery(b *testing.B) {
	api, _, mockDB := setupGrafanaAPI()

	mockData := [][]interface{}{
		{time.Now(), 45.5},
		{time.Now().Add(time.Minute), 47.2},
	}
	rows := &MockRows{data: mockData}
	mockDB.On("QueryContext", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return(rows, nil)

	requestBody := GrafanaQueryRequest{
		Range: GrafanaRange{
			From: "2023-01-01T00:00:00Z",
			To:   "2023-01-02T00:00:00Z",
		},
		Targets: []GrafanaTarget{
			{Target: "test.overall.avg_latency"},
		},
	}
	body, _ := json.Marshal(requestBody)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/grafana/query", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		api.HandleGrafanaQuery(w, req)
	}
}

func BenchmarkParseMetricTarget(b *testing.B) {
	api, _, _ := setupGrafanaAPI()
	target := "test-benchmark.geth.avg_latency"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = api.parseMetricTarget(target)
	}
}

func BenchmarkFormatGrafanaTimeSeries(b *testing.B) {
	api, _, _ := setupGrafanaAPI()

	data := []TimeSeriesDataPoint{
		{Timestamp: time.Now(), Value: 45.5},
		{Timestamp: time.Now().Add(time.Minute), Value: 47.2},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = api.FormatGrafanaTimeSeries(data, "test.metric")
	}
}

// Integration tests

func TestGrafanaAPIIntegration(b *testing.T) {
	api, mockStorage, mockDB := setupGrafanaAPI()

	ctx := context.Background()

	// Test lifecycle
	err := api.Start(ctx)
	require.NoError(b, err)

	// Test connection endpoint
	mockDB.On("Ping").Return(nil)

	req := httptest.NewRequest("GET", "/grafana/", nil)
	w := httptest.NewRecorder()

	api.HandleGrafanaTestConnection(w, req)
	assert.Equal(b, http.StatusOK, w.Code)

	// Test search endpoint
	rows := &MockRows{
		data: [][]interface{}{
			{"test-benchmark"},
			{"another-test"},
		},
	}
	mockDB.On("QueryContext", mock.Anything, mock.AnythingOfType("string")).Return(rows, nil)
	mockDB.On("QueryContext", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return(&MockRows{data: [][]interface{}{{"geth"}}}, nil)

	searchReq := GrafanaSearchRequest{Target: "test"}
	body, _ := json.Marshal(searchReq)

	req = httptest.NewRequest("POST", "/grafana/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()

	api.HandleGrafanaSearch(w, req)
	assert.Equal(b, http.StatusOK, w.Code)

	// Test cleanup
	err = api.Stop()
	require.NoError(b, err)

	mockDB.AssertExpectations(b)
}
