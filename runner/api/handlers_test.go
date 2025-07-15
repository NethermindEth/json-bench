package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/types"
)

// Helper function to setup test handlers
func setupTestHandlers() (*apiHandlers, *MockHistoricStorage, *MockBaselineManager, *MockTrendAnalyzer, *MockRegressionDetector, *MockDB) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel) // Reduce noise in tests

	storage := &MockHistoricStorage{}
	baselineManager := &MockBaselineManager{}
	trendAnalyzer := &MockTrendAnalyzer{}
	regressionDetector := &MockRegressionDetector{}
	db := &MockDB{}

	handlers := &apiHandlers{
		storage:            storage,
		baselineManager:    baselineManager,
		trendAnalyzer:      trendAnalyzer,
		regressionDetector: regressionDetector,
		db:                 db,
		log:                log.WithField("component", "api-handlers"),
	}

	return handlers, storage, baselineManager, trendAnalyzer, regressionDetector, db
}

// Helper function to create mock historic run
func createMockHistoricRun(id string) *types.HistoricRun {
	return &types.HistoricRun{
		ID:               id,
		TestName:         "test-benchmark",
		Description:      "Test benchmark description",
		GitCommit:        "abc123def456",
		GitBranch:        "main",
		Timestamp:        time.Now(),
		StartTime:        time.Now().Add(-5 * time.Minute),
		EndTime:          time.Now(),
		Duration:         "5m0s",
		ClientsCount:     3,
		EndpointsCount:   10,
		TargetRPS:        100,
		TotalRequests:    30000,
		TotalErrors:      150,
		OverallErrorRate: 0.005,
		AvgLatencyMs:     45.5,
		P95LatencyMs:     89.2,
		P99LatencyMs:     125.8,
		MaxLatencyMs:     250.0,
		BestClient:       "geth",
		PerformanceScores: map[string]float64{
			"geth":       92.5,
			"besu":       88.3,
			"nethermind": 85.1,
		},
		FullResults: json.RawMessage(`{"test": "data"}`),
		Notes:       "Test run notes",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// Test APIHandlers creation and lifecycle

func TestNewAPIHandlers(t *testing.T) {
	handlers, storage, baselineManager, trendAnalyzer, regressionDetector, db := setupTestHandlers()

	assert.NotNil(t, handlers)
	assert.Equal(t, storage, handlers.storage)
	assert.Equal(t, baselineManager, handlers.baselineManager)
	assert.Equal(t, trendAnalyzer, handlers.trendAnalyzer)
	assert.Equal(t, regressionDetector, handlers.regressionDetector)
	assert.Equal(t, db, handlers.db)
	assert.NotNil(t, handlers.log)
}

func TestAPIHandlersStartStop(t *testing.T) {
	handlers, _, _, _, _, _ := setupTestHandlers()

	ctx := context.Background()

	// Test Start
	err := handlers.Start(ctx)
	assert.NoError(t, err)

	// Test Stop
	err = handlers.Stop()
	assert.NoError(t, err)
}

// Test Historic Runs Handlers

func TestHandleListRuns(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		mockRuns       []*types.HistoricRun
		mockError      error
		expectedStatus int
		expectedCount  int
	}{
		{
			name:        "successful list with no filters",
			queryParams: "",
			mockRuns: []*types.HistoricRun{
				createMockHistoricRun("run1"),
				createMockHistoricRun("run2"),
			},
			mockError:      nil,
			expectedStatus: http.StatusOK,
			expectedCount:  2,
		},
		{
			name:        "successful list with test filter",
			queryParams: "?test=specific-test&limit=10",
			mockRuns: []*types.HistoricRun{
				createMockHistoricRun("run1"),
			},
			mockError:      nil,
			expectedStatus: http.StatusOK,
			expectedCount:  1,
		},
		{
			name:           "storage error",
			queryParams:    "",
			mockRuns:       nil,
			mockError:      fmt.Errorf("storage error"),
			expectedStatus: http.StatusInternalServerError,
			expectedCount:  0,
		},
		{
			name:           "empty result",
			queryParams:    "",
			mockRuns:       []*types.HistoricRun{},
			mockError:      nil,
			expectedStatus: http.StatusOK,
			expectedCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, mockStorage, _, _, _, mockDB := setupTestHandlers()

			// Setup expectations
			mockStorage.On("ListHistoricRuns", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("int")).Return(tt.mockRuns, tt.mockError)
			if tt.mockError == nil {
				mockDB.On("QueryRowContext", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return(&sql.Row{})
			}

			req := httptest.NewRequest("GET", "/api/runs"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			handlers.HandleListRuns(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)

				runs, ok := response["runs"].([]interface{})
				require.True(t, ok)
				assert.Len(t, runs, tt.expectedCount)

				assert.Contains(t, response, "count")
				assert.Contains(t, response, "limit")
				assert.Contains(t, response, "offset")
			}

			mockStorage.AssertExpectations(t)
		})
	}
}

func TestHandleGetRun(t *testing.T) {
	tests := []struct {
		name           string
		runID          string
		mockRun        *types.HistoricRun
		mockError      error
		expectedStatus int
	}{
		{
			name:           "successful get",
			runID:          "test-run-1",
			mockRun:        createMockHistoricRun("test-run-1"),
			mockError:      nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "run not found",
			runID:          "nonexistent",
			mockRun:        nil,
			mockError:      fmt.Errorf("run not found"),
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "storage error",
			runID:          "test-run-1",
			mockRun:        nil,
			mockError:      fmt.Errorf("database connection failed"),
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "empty run ID",
			runID:          "",
			mockRun:        nil,
			mockError:      nil,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, mockStorage, _, _, _, _ := setupTestHandlers()

			if tt.runID != "" {
				mockStorage.On("GetHistoricRun", mock.Anything, tt.runID).Return(tt.mockRun, tt.mockError)
			}

			req := httptest.NewRequest("GET", "/api/runs/"+tt.runID, nil)
			req = mux.SetURLVars(req, map[string]string{"runId": tt.runID})
			w := httptest.NewRecorder()

			handlers.HandleGetRun(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var run types.HistoricRun
				err := json.Unmarshal(w.Body.Bytes(), &run)
				require.NoError(t, err)
				assert.Equal(t, tt.mockRun.ID, run.ID)
				assert.Equal(t, tt.mockRun.TestName, run.TestName)
			}

			mockStorage.AssertExpectations(t)
		})
	}
}

func TestHandleGetRunWithClientFilter(t *testing.T) {
	handlers, mockStorage, _, _, _, _ := setupTestHandlers()

	mockRun := createMockHistoricRun("test-run-1")
	mockStorage.On("GetHistoricRun", mock.Anything, "test-run-1").Return(mockRun, nil)

	req := httptest.NewRequest("GET", "/api/runs/test-run-1?client=geth", nil)
	req = mux.SetURLVars(req, map[string]string{"runId": "test-run-1"})
	w := httptest.NewRecorder()

	handlers.HandleGetRun(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var run types.HistoricRun
	err := json.Unmarshal(w.Body.Bytes(), &run)
	require.NoError(t, err)
	assert.Equal(t, "test-run-1", run.ID)

	mockStorage.AssertExpectations(t)
}

func TestHandleDeleteRun(t *testing.T) {
	tests := []struct {
		name           string
		runID          string
		mockError      error
		expectedStatus int
	}{
		{
			name:           "successful delete",
			runID:          "test-run-1",
			mockError:      nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "run not found",
			runID:          "nonexistent",
			mockError:      fmt.Errorf("run not found"),
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "storage error",
			runID:          "test-run-1",
			mockError:      fmt.Errorf("database error"),
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "empty run ID",
			runID:          "",
			mockError:      nil,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, mockStorage, _, _, _, _ := setupTestHandlers()

			if tt.runID != "" {
				mockStorage.On("DeleteHistoricRun", mock.Anything, tt.runID).Return(tt.mockError)
			}

			req := httptest.NewRequest("DELETE", "/api/runs/"+tt.runID, nil)
			req = mux.SetURLVars(req, map[string]string{"runId": tt.runID})
			w := httptest.NewRecorder()

			handlers.HandleDeleteRun(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Equal(t, "success", response["status"])
				assert.Equal(t, tt.runID, response["run_id"])
			}

			mockStorage.AssertExpectations(t)
		})
	}
}

func TestHandleCompareRuns(t *testing.T) {
	tests := []struct {
		name           string
		runID1         string
		runID2         string
		mockComparison *types.HistoricComparison
		mockError      error
		expectedStatus int
	}{
		{
			name:   "successful comparison",
			runID1: "run1",
			runID2: "run2",
			mockComparison: &types.HistoricComparison{
				RunID1:     "run1",
				RunID2:     "run2",
				Summary:    "Run 2 performed 5% better than run 1",
				Timestamp1: time.Now().Add(-1 * time.Hour),
				Timestamp2: time.Now(),
			},
			mockError:      nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "storage error",
			runID1:         "run1",
			runID2:         "run2",
			mockComparison: nil,
			mockError:      fmt.Errorf("comparison failed"),
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "missing run ID",
			runID1:         "run1",
			runID2:         "",
			mockComparison: nil,
			mockError:      nil,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, mockStorage, _, _, _, _ := setupTestHandlers()

			if tt.runID1 != "" && tt.runID2 != "" {
				mockStorage.On("CompareRuns", mock.Anything, tt.runID1, tt.runID2).Return(tt.mockComparison, tt.mockError)
			}

			req := httptest.NewRequest("GET", "/api/runs/"+tt.runID1+"/compare/"+tt.runID2, nil)
			req = mux.SetURLVars(req, map[string]string{"runId1": tt.runID1, "runId2": tt.runID2})
			w := httptest.NewRecorder()

			handlers.HandleCompareRuns(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var comparison types.HistoricComparison
				err := json.Unmarshal(w.Body.Bytes(), &comparison)
				require.NoError(t, err)
				assert.Equal(t, tt.mockComparison.RunID1, comparison.RunID1)
				assert.Equal(t, tt.mockComparison.RunID2, comparison.RunID2)
			}

			mockStorage.AssertExpectations(t)
		})
	}
}

// Test Baseline Management Handlers

func TestHandleListBaselines(t *testing.T) {
	tests := []struct {
		name           string
		testName       string
		mockBaselines  []*types.Baseline
		mockError      error
		expectedStatus int
		expectedCount  int
	}{
		{
			name:     "successful list",
			testName: "test-benchmark",
			mockBaselines: []*types.Baseline{
				{Name: "baseline1", TestName: "test-benchmark"},
				{Name: "baseline2", TestName: "test-benchmark"},
			},
			mockError:      nil,
			expectedStatus: http.StatusOK,
			expectedCount:  2,
		},
		{
			name:           "baseline manager error",
			testName:       "test-benchmark",
			mockBaselines:  nil,
			mockError:      fmt.Errorf("baseline error"),
			expectedStatus: http.StatusInternalServerError,
			expectedCount:  0,
		},
		{
			name:           "empty result",
			testName:       "test-benchmark",
			mockBaselines:  []*types.Baseline{},
			mockError:      nil,
			expectedStatus: http.StatusOK,
			expectedCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, _, mockBaselineManager, _, _, _ := setupTestHandlers()

			mockBaselineManager.On("ListBaselines", mock.Anything, tt.testName).Return(tt.mockBaselines, tt.mockError)

			req := httptest.NewRequest("GET", "/api/baselines?test="+tt.testName, nil)
			w := httptest.NewRecorder()

			handlers.HandleListBaselines(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)

				baselines, ok := response["baselines"].([]interface{})
				require.True(t, ok)
				assert.Len(t, baselines, tt.expectedCount)
			}

			mockBaselineManager.AssertExpectations(t)
		})
	}
}

func TestHandleCreateBaseline(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    interface{}
		mockBaseline   *types.Baseline
		mockError      error
		expectedStatus int
	}{
		{
			name: "successful creation",
			requestBody: CreateBaselineRequest{
				RunID:       "test-run-1",
				Name:        "test-baseline",
				Description: "Test baseline description",
			},
			mockBaseline: &types.Baseline{
				Name:        "test-baseline",
				RunID:       "test-run-1",
				Description: "Test baseline description",
				TestName:    "test-benchmark",
			},
			mockError:      nil,
			expectedStatus: http.StatusCreated,
		},
		{
			name: "missing required fields",
			requestBody: CreateBaselineRequest{
				RunID: "test-run-1",
				// Missing Name
			},
			mockBaseline:   nil,
			mockError:      nil,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "baseline manager error",
			requestBody: CreateBaselineRequest{
				RunID:       "test-run-1",
				Name:        "test-baseline",
				Description: "Test baseline description",
			},
			mockBaseline:   nil,
			mockError:      fmt.Errorf("baseline creation failed"),
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "invalid JSON",
			requestBody:    "invalid json",
			mockBaseline:   nil,
			mockError:      nil,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, _, mockBaselineManager, _, _, _ := setupTestHandlers()

			var body []byte
			if req, ok := tt.requestBody.(CreateBaselineRequest); ok {
				body, _ = json.Marshal(req)
				if req.RunID != "" && req.Name != "" {
					mockBaselineManager.On("SetBaseline", mock.Anything, req.RunID, req.Name, req.Description).Return(tt.mockBaseline, tt.mockError)
				}
			} else {
				body = []byte(tt.requestBody.(string))
			}

			req := httptest.NewRequest("POST", "/api/baselines", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleCreateBaseline(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusCreated {
				var baseline types.Baseline
				err := json.Unmarshal(w.Body.Bytes(), &baseline)
				require.NoError(t, err)
				assert.Equal(t, tt.mockBaseline.Name, baseline.Name)
			}

			mockBaselineManager.AssertExpectations(t)
		})
	}
}

func TestHandleGetBaseline(t *testing.T) {
	tests := []struct {
		name           string
		baselineName   string
		mockBaseline   *types.Baseline
		mockError      error
		expectedStatus int
	}{
		{
			name:         "successful get",
			baselineName: "test-baseline",
			mockBaseline: &types.Baseline{
				Name:        "test-baseline",
				RunID:       "test-run-1",
				Description: "Test baseline",
				TestName:    "test-benchmark",
			},
			mockError:      nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "baseline not found",
			baselineName:   "nonexistent",
			mockBaseline:   nil,
			mockError:      fmt.Errorf("baseline not found"),
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "empty baseline name",
			baselineName:   "",
			mockBaseline:   nil,
			mockError:      nil,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, _, mockBaselineManager, _, _, _ := setupTestHandlers()

			if tt.baselineName != "" {
				mockBaselineManager.On("GetBaseline", mock.Anything, tt.baselineName).Return(tt.mockBaseline, tt.mockError)
			}

			req := httptest.NewRequest("GET", "/api/baselines/"+tt.baselineName, nil)
			req = mux.SetURLVars(req, map[string]string{"baselineName": tt.baselineName})
			w := httptest.NewRecorder()

			handlers.HandleGetBaseline(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var baseline types.Baseline
				err := json.Unmarshal(w.Body.Bytes(), &baseline)
				require.NoError(t, err)
				assert.Equal(t, tt.mockBaseline.Name, baseline.Name)
			}

			mockBaselineManager.AssertExpectations(t)
		})
	}
}

func TestHandleDeleteBaseline(t *testing.T) {
	tests := []struct {
		name           string
		baselineName   string
		mockError      error
		expectedStatus int
	}{
		{
			name:           "successful delete",
			baselineName:   "test-baseline",
			mockError:      nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "baseline not found",
			baselineName:   "nonexistent",
			mockError:      fmt.Errorf("baseline not found"),
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "empty baseline name",
			baselineName:   "",
			mockError:      nil,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, _, mockBaselineManager, _, _, _ := setupTestHandlers()

			if tt.baselineName != "" {
				mockBaselineManager.On("DeleteBaseline", mock.Anything, tt.baselineName).Return(tt.mockError)
			}

			req := httptest.NewRequest("DELETE", "/api/baselines/"+tt.baselineName, nil)
			req = mux.SetURLVars(req, map[string]string{"baselineName": tt.baselineName})
			w := httptest.NewRecorder()

			handlers.HandleDeleteBaseline(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Equal(t, "success", response["status"])
			}

			mockBaselineManager.AssertExpectations(t)
		})
	}
}

// Test Trend Analysis Handlers

func TestHandleGetTrends(t *testing.T) {
	tests := []struct {
		name           string
		testName       string
		days           string
		mockTrends     *types.TrendAnalysis
		mockError      error
		expectedStatus int
	}{
		{
			name:     "successful trends",
			testName: "test-benchmark",
			days:     "30",
			mockTrends: &types.TrendAnalysis{
				TestName: "test-benchmark",
				Days:     30,
				Trends:   map[string]*types.HistoricTrend{},
			},
			mockError:      nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing test name",
			testName:       "",
			days:           "30",
			mockTrends:     nil,
			mockError:      nil,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "trend analyzer error",
			testName:       "test-benchmark",
			days:           "30",
			mockTrends:     nil,
			mockError:      fmt.Errorf("trend calculation failed"),
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, _, _, mockTrendAnalyzer, _, _ := setupTestHandlers()

			if tt.testName != "" {
				expectedDays := 30
				if tt.days != "" {
					expectedDays = 30 // Default value used in handler
				}
				mockTrendAnalyzer.On("CalculateTrends", mock.Anything, tt.testName, expectedDays).Return(tt.mockTrends, tt.mockError)
			}

			url := "/api/trends?test=" + tt.testName
			if tt.days != "" {
				url += "&days=" + tt.days
			}

			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()

			handlers.HandleGetTrends(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var trends types.TrendAnalysis
				err := json.Unmarshal(w.Body.Bytes(), &trends)
				require.NoError(t, err)
				assert.Equal(t, tt.mockTrends.TestName, trends.TestName)
			}

			mockTrendAnalyzer.AssertExpectations(t)
		})
	}
}

// Test Regression Detection Handlers

func TestHandleDetectRegressions(t *testing.T) {
	tests := []struct {
		name           string
		runID          string
		requestBody    interface{}
		mockReport     *types.RegressionReport
		mockError      error
		expectedStatus int
	}{
		{
			name:  "successful detection",
			runID: "test-run-1",
			requestBody: RegressionDetectionRequest{
				ComparisonMode: "sequential",
				LookbackCount:  1,
				WindowSize:     5,
			},
			mockReport: &types.RegressionReport{
				RunID:       "test-run-1",
				Regressions: []*types.Regression{},
				Summary:     "No regressions detected",
			},
			mockError:      nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing run ID",
			runID:          "",
			requestBody:    RegressionDetectionRequest{},
			mockReport:     nil,
			mockError:      nil,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:  "detection error",
			runID: "test-run-1",
			requestBody: RegressionDetectionRequest{
				ComparisonMode: "sequential",
			},
			mockReport:     nil,
			mockError:      fmt.Errorf("detection failed"),
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, _, _, _, mockRegressionDetector, _ := setupTestHandlers()

			var body []byte
			if req, ok := tt.requestBody.(RegressionDetectionRequest); ok {
				body, _ = json.Marshal(req)
				if tt.runID != "" {
					mockRegressionDetector.On("DetectRegressions", mock.Anything, tt.runID, mock.AnythingOfType("analysis.DetectionOptions")).Return(tt.mockReport, tt.mockError)
				}
			}

			req := httptest.NewRequest("POST", "/api/runs/"+tt.runID+"/regressions", bytes.NewReader(body))
			req = mux.SetURLVars(req, map[string]string{"runId": tt.runID})
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleDetectRegressions(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var report types.RegressionReport
				err := json.Unmarshal(w.Body.Bytes(), &report)
				require.NoError(t, err)
				assert.Equal(t, tt.mockReport.RunID, report.RunID)
			}

			mockRegressionDetector.AssertExpectations(t)
		})
	}
}

func TestHandleGetRegressions(t *testing.T) {
	tests := []struct {
		name            string
		runID           string
		severityFilter  string
		mockRegressions []*types.Regression
		mockError       error
		expectedStatus  int
		expectedCount   int
	}{
		{
			name:  "successful get with no filter",
			runID: "test-run-1",
			mockRegressions: []*types.Regression{
				{ID: "reg1", RunID: "test-run-1", Severity: "high"},
				{ID: "reg2", RunID: "test-run-1", Severity: "low"},
			},
			mockError:      nil,
			expectedStatus: http.StatusOK,
			expectedCount:  2,
		},
		{
			name:           "successful get with severity filter",
			runID:          "test-run-1",
			severityFilter: "high",
			mockRegressions: []*types.Regression{
				{ID: "reg1", RunID: "test-run-1", Severity: "high"},
				{ID: "reg2", RunID: "test-run-1", Severity: "low"},
			},
			mockError:      nil,
			expectedStatus: http.StatusOK,
			expectedCount:  1, // Only high severity should be returned
		},
		{
			name:            "missing run ID",
			runID:           "",
			mockRegressions: nil,
			mockError:       nil,
			expectedStatus:  http.StatusBadRequest,
			expectedCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, _, _, _, mockRegressionDetector, _ := setupTestHandlers()

			if tt.runID != "" {
				mockRegressionDetector.On("GetRegressions", mock.Anything, tt.runID).Return(tt.mockRegressions, tt.mockError)
			}

			url := "/api/runs/" + tt.runID + "/regressions"
			if tt.severityFilter != "" {
				url += "?severity=" + tt.severityFilter
			}

			req := httptest.NewRequest("GET", url, nil)
			req = mux.SetURLVars(req, map[string]string{"runId": tt.runID})
			w := httptest.NewRecorder()

			handlers.HandleGetRegressions(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)

				regressions, ok := response["regressions"].([]interface{})
				require.True(t, ok)
				assert.Len(t, regressions, tt.expectedCount)
			}

			mockRegressionDetector.AssertExpectations(t)
		})
	}
}

func TestHandleAcknowledgeRegression(t *testing.T) {
	tests := []struct {
		name           string
		regressionID   string
		requestBody    interface{}
		mockError      error
		expectedStatus int
	}{
		{
			name:         "successful acknowledgment",
			regressionID: "regression-1",
			requestBody: AcknowledgeRegressionRequest{
				AcknowledgedBy: "user@example.com",
				Notes:          "Acknowledged - investigating fix",
			},
			mockError:      nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing regression ID",
			regressionID:   "",
			requestBody:    AcknowledgeRegressionRequest{AcknowledgedBy: "user@example.com"},
			mockError:      nil,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:         "missing acknowledged by",
			regressionID: "regression-1",
			requestBody: AcknowledgeRegressionRequest{
				Notes: "Investigating",
			},
			mockError:      nil,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:         "regression not found",
			regressionID: "nonexistent",
			requestBody: AcknowledgeRegressionRequest{
				AcknowledgedBy: "user@example.com",
			},
			mockError:      fmt.Errorf("regression not found"),
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, _, _, _, mockRegressionDetector, _ := setupTestHandlers()

			var body []byte
			if req, ok := tt.requestBody.(AcknowledgeRegressionRequest); ok {
				body, _ = json.Marshal(req)
				if tt.regressionID != "" && req.AcknowledgedBy != "" {
					mockRegressionDetector.On("AcknowledgeRegression", mock.Anything, tt.regressionID, req.AcknowledgedBy).Return(tt.mockError)
				}
			}

			req := httptest.NewRequest("POST", "/api/regressions/"+tt.regressionID+"/acknowledge", bytes.NewReader(body))
			req = mux.SetURLVars(req, map[string]string{"regressionId": tt.regressionID})
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleAcknowledgeRegression(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Equal(t, "success", response["status"])
			}

			mockRegressionDetector.AssertExpectations(t)
		})
	}
}

// Test Health and Status Handlers

func TestHandleHealth(t *testing.T) {
	tests := []struct {
		name           string
		dbPingError    error
		storageError   error
		expectedStatus int
		expectedHealth string
	}{
		{
			name:           "healthy system",
			dbPingError:    nil,
			storageError:   nil,
			expectedStatus: http.StatusOK,
			expectedHealth: "healthy",
		},
		{
			name:           "unhealthy database",
			dbPingError:    fmt.Errorf("db connection failed"),
			storageError:   nil,
			expectedStatus: http.StatusServiceUnavailable,
			expectedHealth: "unhealthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, mockStorage, _, _, _, mockDB := setupTestHandlers()

			mockDB.On("Ping").Return(tt.dbPingError)
			if tt.dbPingError == nil {
				mockStorage.On("ListHistoricRuns", mock.Anything, "", 1).Return([]*types.HistoricRun{}, tt.storageError)
			}

			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()

			handlers.HandleHealth(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Contains(t, response["status"], tt.expectedHealth)

			mockDB.AssertExpectations(t)
			if tt.dbPingError == nil {
				mockStorage.AssertExpectations(t)
			}
		})
	}
}

// Test JSON utility methods

func TestWriteJSONResponse(t *testing.T) {
	handlers, _, _, _, _, _ := setupTestHandlers()

	testData := map[string]interface{}{
		"test": "data",
		"num":  42,
	}

	w := httptest.NewRecorder()
	handlers.writeJSONResponse(w, http.StatusOK, testData)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "data", response["test"])
	assert.Equal(t, float64(42), response["num"])
}

func TestWriteErrorResponse(t *testing.T) {
	handlers, _, _, _, _, _ := setupTestHandlers()

	w := httptest.NewRecorder()
	handlers.writeErrorResponse(w, http.StatusBadRequest, "Test error message")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, true, response["error"])
	assert.Equal(t, "Test error message", response["message"])
	assert.Equal(t, float64(400), response["status"])
	assert.Contains(t, response, "timestamp")
}

// Benchmark tests

func BenchmarkHandleListRuns(b *testing.B) {
	handlers, mockStorage, _, _, _, _ := setupTestHandlers()

	mockRuns := []*types.HistoricRun{
		createMockHistoricRun("run1"),
		createMockHistoricRun("run2"),
	}
	mockStorage.On("ListHistoricRuns", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("int")).Return(mockRuns, nil)

	req := httptest.NewRequest("GET", "/api/runs", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handlers.HandleListRuns(w, req)
	}
}

func BenchmarkHandleGetRun(b *testing.B) {
	handlers, mockStorage, _, _, _, _ := setupTestHandlers()

	mockRun := createMockHistoricRun("test-run-1")
	mockStorage.On("GetHistoricRun", mock.Anything, "test-run-1").Return(mockRun, nil)

	req := httptest.NewRequest("GET", "/api/runs/test-run-1", nil)
	req = mux.SetURLVars(req, map[string]string{"runId": "test-run-1"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handlers.HandleGetRun(w, req)
	}
}

// Integration tests

func TestHandlerIntegration(t *testing.T) {
	handlers, mockStorage, mockBaselineManager, mockTrendAnalyzer, mockRegressionDetector, mockDB := setupTestHandlers()

	// Test lifecycle
	ctx := context.Background()
	err := handlers.Start(ctx)
	require.NoError(t, err)

	// Test typical workflow: list runs, get specific run, create baseline
	mockRuns := []*types.HistoricRun{createMockHistoricRun("run1")}
	mockStorage.On("ListHistoricRuns", mock.Anything, "", 50).Return(mockRuns, nil)

	mockRun := createMockHistoricRun("run1")
	mockStorage.On("GetHistoricRun", mock.Anything, "run1").Return(mockRun, nil)

	mockBaseline := &types.Baseline{Name: "test-baseline", RunID: "run1"}
	mockBaselineManager.On("SetBaseline", mock.Anything, "run1", "test-baseline", "Test baseline").Return(mockBaseline, nil)

	// Test sequence
	// 1. List runs
	req1 := httptest.NewRequest("GET", "/api/runs", nil)
	w1 := httptest.NewRecorder()
	handlers.HandleListRuns(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	// 2. Get specific run
	req2 := httptest.NewRequest("GET", "/api/runs/run1", nil)
	req2 = mux.SetURLVars(req2, map[string]string{"runId": "run1"})
	w2 := httptest.NewRecorder()
	handlers.HandleGetRun(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	// 3. Create baseline from run
	baselineReq := CreateBaselineRequest{
		RunID:       "run1",
		Name:        "test-baseline",
		Description: "Test baseline",
	}
	body, _ := json.Marshal(baselineReq)
	req3 := httptest.NewRequest("POST", "/api/baselines", bytes.NewReader(body))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	handlers.HandleCreateBaseline(w3, req3)
	assert.Equal(t, http.StatusCreated, w3.Code)

	// Test cleanup
	err = handlers.Stop()
	require.NoError(t, err)

	mockStorage.AssertExpectations(t)
	mockBaselineManager.AssertExpectations(t)
}

func TestErrorHandling(t *testing.T) {
	handlers, _, _, _, _, _ := setupTestHandlers()

	// Test handling of malformed JSON
	req := httptest.NewRequest("POST", "/api/baselines", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandleCreateBaseline(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Test handling of missing content type
	req = httptest.NewRequest("POST", "/api/baselines", strings.NewReader("{}"))
	w = httptest.NewRecorder()

	handlers.HandleCreateBaseline(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
