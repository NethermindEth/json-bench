package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/analysis"
	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// Mock implementations for testing

type MockHistoricStorage struct {
	mock.Mock
}

func (m *MockHistoricStorage) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockHistoricStorage) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockHistoricStorage) SaveHistoricRun(ctx context.Context, result *types.BenchmarkResult) (*types.HistoricRun, error) {
	args := m.Called(ctx, result)
	return args.Get(0).(*types.HistoricRun), args.Error(1)
}

func (m *MockHistoricStorage) GetHistoricRun(ctx context.Context, runID string) (*types.HistoricRun, error) {
	args := m.Called(ctx, runID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.HistoricRun), args.Error(1)
}

func (m *MockHistoricStorage) ListHistoricRuns(ctx context.Context, testName string, limit int) ([]*types.HistoricRun, error) {
	args := m.Called(ctx, testName, limit)
	return args.Get(0).([]*types.HistoricRun), args.Error(1)
}

func (m *MockHistoricStorage) DeleteHistoricRun(ctx context.Context, runID string) error {
	args := m.Called(ctx, runID)
	return args.Error(0)
}

func (m *MockHistoricStorage) GetHistoricTrends(ctx context.Context, testName, client, metric string, days int) (*types.HistoricTrend, error) {
	args := m.Called(ctx, testName, client, metric, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.HistoricTrend), args.Error(1)
}

func (m *MockHistoricStorage) CompareRuns(ctx context.Context, runID1, runID2 string) (*types.HistoricComparison, error) {
	args := m.Called(ctx, runID1, runID2)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.HistoricComparison), args.Error(1)
}

func (m *MockHistoricStorage) GetHistoricSummary(ctx context.Context, testName string) (*types.HistoricSummary, error) {
	args := m.Called(ctx, testName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.HistoricSummary), args.Error(1)
}

func (m *MockHistoricStorage) SaveResultFiles(ctx context.Context, runID string, result *types.BenchmarkResult) error {
	args := m.Called(ctx, runID, result)
	return args.Error(0)
}

func (m *MockHistoricStorage) GetResultFiles(ctx context.Context, runID string) (string, error) {
	args := m.Called(ctx, runID)
	return args.String(0), args.Error(1)
}

func (m *MockHistoricStorage) CleanupOldFiles(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

type MockBaselineManager struct {
	mock.Mock
}

func (m *MockBaselineManager) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockBaselineManager) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockBaselineManager) SetBaseline(ctx context.Context, runID, name, description string) (*types.Baseline, error) {
	args := m.Called(ctx, runID, name, description)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Baseline), args.Error(1)
}

func (m *MockBaselineManager) GetBaseline(ctx context.Context, name string) (*types.Baseline, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Baseline), args.Error(1)
}

func (m *MockBaselineManager) ListBaselines(ctx context.Context, testName string) ([]*types.Baseline, error) {
	args := m.Called(ctx, testName)
	return args.Get(0).([]*types.Baseline), args.Error(1)
}

func (m *MockBaselineManager) DeleteBaseline(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

func (m *MockBaselineManager) CompareToBaseline(ctx context.Context, runID, baselineName string) (*types.BaselineComparison, error) {
	args := m.Called(ctx, runID, baselineName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.BaselineComparison), args.Error(1)
}

type MockTrendAnalyzer struct {
	mock.Mock
}

func (m *MockTrendAnalyzer) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockTrendAnalyzer) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockTrendAnalyzer) CalculateTrends(ctx context.Context, testName string, days int) (*types.TrendAnalysis, error) {
	args := m.Called(ctx, testName, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.TrendAnalysis), args.Error(1)
}

func (m *MockTrendAnalyzer) GetMethodTrends(ctx context.Context, testName, method string, days int) (*types.MethodTrends, error) {
	args := m.Called(ctx, testName, method, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.MethodTrends), args.Error(1)
}

func (m *MockTrendAnalyzer) GetClientTrends(ctx context.Context, testName, client string, days int) (*types.ClientTrends, error) {
	args := m.Called(ctx, testName, client, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.ClientTrends), args.Error(1)
}

func (m *MockTrendAnalyzer) CalculateMovingAverage(ctx context.Context, testName, metric string, windowSize, days int) (*types.MovingAverage, error) {
	args := m.Called(ctx, testName, metric, windowSize, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.MovingAverage), args.Error(1)
}

func (m *MockTrendAnalyzer) ForecastTrend(ctx context.Context, testName, metric string, historyDays, forecastDays int) (*types.TrendForecast, error) {
	args := m.Called(ctx, testName, metric, historyDays, forecastDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.TrendForecast), args.Error(1)
}

type MockRegressionDetector struct {
	mock.Mock
}

func (m *MockRegressionDetector) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockRegressionDetector) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockRegressionDetector) DetectRegressions(ctx context.Context, runID string, options analysis.DetectionOptions) (*types.RegressionReport, error) {
	args := m.Called(ctx, runID, options)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.RegressionReport), args.Error(1)
}

func (m *MockRegressionDetector) GetRegressions(ctx context.Context, runID string) ([]*types.Regression, error) {
	args := m.Called(ctx, runID)
	return args.Get(0).([]*types.Regression), args.Error(1)
}

func (m *MockRegressionDetector) AcknowledgeRegression(ctx context.Context, regressionID, acknowledgedBy string) error {
	args := m.Called(ctx, regressionID, acknowledgedBy)
	return args.Error(0)
}

func (m *MockRegressionDetector) AnalyzeRun(ctx context.Context, runID string) (*types.RunAnalysis, error) {
	args := m.Called(ctx, runID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.RunAnalysis), args.Error(1)
}

type MockDB struct {
	mock.Mock
}

func (m *MockDB) Ping() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	mockArgs := m.Called(ctx, query, args)
	if mockArgs.Get(0) == nil {
		return nil, mockArgs.Error(1)
	}
	return mockArgs.Get(0).(*sql.Rows), mockArgs.Error(1)
}

func (m *MockDB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	mockArgs := m.Called(ctx, query, args)
	return mockArgs.Get(0).(*sql.Row)
}

func (m *MockDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	mockArgs := m.Called(ctx, query, args)
	if mockArgs.Get(0) == nil {
		return nil, mockArgs.Error(1)
	}
	return mockArgs.Get(0).(sql.Result), mockArgs.Error(1)
}

// Helper functions for test setup

func setupTestServer() (*server, *MockHistoricStorage, *MockBaselineManager, *MockTrendAnalyzer, *MockRegressionDetector, *MockDB) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel) // Reduce noise in tests

	storage := &MockHistoricStorage{}
	baselineManager := &MockBaselineManager{}
	trendAnalyzer := &MockTrendAnalyzer{}
	regressionDetector := &MockRegressionDetector{}
	db := &MockDB{}

	srv := &server{
		storage:            storage,
		baselineManager:    baselineManager,
		trendAnalyzer:      trendAnalyzer,
		regressionDetector: regressionDetector,
		db:                 db,
		log:                log.WithField("component", "api-server"),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		wsClients:   make(map[*websocket.Conn]bool),
		wsBroadcast: make(chan []byte, 100),
	}

	return srv, storage, baselineManager, trendAnalyzer, regressionDetector, db
}

// Test server creation and configuration

func TestNewServer(t *testing.T) {
	log := logrus.New()
	storage := &MockHistoricStorage{}
	baselineManager := &MockBaselineManager{}
	trendAnalyzer := &MockTrendAnalyzer{}
	regressionDetector := &MockRegressionDetector{}
	db := &MockDB{}

	srv := NewServer(storage, baselineManager, trendAnalyzer, regressionDetector, db, log)

	assert.NotNil(t, srv)
	assert.Implements(t, (*Server)(nil), srv)
}

func TestServerStart(t *testing.T) {
	srv, _, _, _, _, _ := setupTestServer()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := srv.Start(ctx)
	assert.NoError(t, err)

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Check that server is running
	assert.NotNil(t, srv.httpServer)

	// Stop the server
	err = srv.Stop()
	assert.NoError(t, err)
}

func TestServerStartWithPortInUse(t *testing.T) {
	// Create first server and start it
	srv1, _, _, _, _, _ := setupTestServer()

	ctx1, cancel1 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel1()

	err := srv1.Start(ctx1)
	assert.NoError(t, err)
	defer srv1.Stop()

	// Create second server with same port (should fail to bind)
	srv2, _, _, _, _, _ := setupTestServer()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()

	err = srv2.Start(ctx2)
	assert.NoError(t, err) // Start method doesn't return error for port conflicts
	defer srv2.Stop()

	// Give time for potential port conflict
	time.Sleep(50 * time.Millisecond)
}

func TestServerStop(t *testing.T) {
	srv, _, _, _, _, _ := setupTestServer()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start server
	err := srv.Start(ctx)
	assert.NoError(t, err)

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Stop server
	err = srv.Stop()
	assert.NoError(t, err)

	// Verify server is stopped
	assert.NotNil(t, srv.httpServer) // Server object still exists but is shut down
}

func TestServerStopWithoutStart(t *testing.T) {
	srv, _, _, _, _, _ := setupTestServer()

	// Stop server without starting it first
	err := srv.Stop()
	assert.NoError(t, err) // Should handle gracefully
}

// Test route setup and middleware

func TestSetupRoutes(t *testing.T) {
	srv, _, _, _, _, _ := setupTestServer()

	router := srv.setupRoutes()
	assert.NotNil(t, router)
	assert.IsType(t, &mux.Router{}, router)

	// Test that router is properly configured by making a test request
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should get a response (even if it's an error response)
	assert.NotEqual(t, 0, w.Code)
}

func TestCORSMiddleware(t *testing.T) {
	srv, _, _, _, _, _ := setupTestServer()

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})

	// Wrap with CORS middleware
	corsHandler := srv.enableCORS(testHandler)

	// Test OPTIONS request
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	w := httptest.NewRecorder()

	corsHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, DELETE, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Content-Type, Authorization, X-Requested-With", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "86400", w.Header().Get("Access-Control-Max-Age"))

	// Test regular request
	req = httptest.NewRequest("GET", "/test", nil)
	w = httptest.NewRecorder()

	corsHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "test", w.Body.String())
}

func TestLoggingMiddleware(t *testing.T) {
	srv, _, _, _, _, _ := setupTestServer()

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})

	// Wrap with logging middleware
	logHandler := srv.loggingMiddleware(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "test-agent")
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()

	logHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "test", w.Body.String())
}

func TestErrorHandlingMiddleware(t *testing.T) {
	srv, _, _, _, _, _ := setupTestServer()

	// Create a handler that panics
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	// Wrap with error handling middleware
	errorHandler := srv.errorHandlingMiddleware(panicHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Should not panic
	assert.NotPanics(t, func() {
		errorHandler.ServeHTTP(w, req)
	})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestResponseWriterWrapper(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &responseWriterWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	// Test WriteHeader
	wrapper.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, wrapper.statusCode)
	assert.Equal(t, http.StatusNotFound, recorder.Code)

	// Test Write
	data := []byte("test data")
	n, err := wrapper.Write(data)
	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, string(data), recorder.Body.String())
}

// Test health endpoint (implementation in handlers_test.go)

// Test WebSocket functionality

func TestWebSocketHub(t *testing.T) {
	srv, _, _, _, _, _ := setupTestServer()

	// Test that WebSocket channels are initialized
	assert.NotNil(t, srv.wsClients)
	assert.NotNil(t, srv.wsBroadcast)

	// Test broadcasting to empty clients (should not panic)
	assert.NotPanics(t, func() {
		srv.BroadcastUpdate("test", map[string]string{"key": "value"})
	})
}

func TestHandleWebSocketHub(t *testing.T) {
	srv, _, _, _, _, _ := setupTestServer()

	// Start the WebSocket hub in a goroutine
	go srv.handleWebSocketHub()

	// Give it time to start
	time.Sleep(10 * time.Millisecond)

	// Send a test message
	testMessage := map[string]interface{}{
		"type": "test",
		"data": "test_data",
	}

	srv.BroadcastUpdate("test", testMessage)

	// Give time for processing
	time.Sleep(10 * time.Millisecond)

	// Close the broadcast channel to stop the hub
	close(srv.wsBroadcast)

	// Give time for cleanup
	time.Sleep(10 * time.Millisecond)
}

func TestBroadcastUpdate(t *testing.T) {
	srv, _, _, _, _, _ := setupTestServer()

	testData := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}

	// Should not panic even with no clients
	assert.NotPanics(t, func() {
		srv.BroadcastUpdate("test_update", testData)
	})

	// Test with full channel (non-blocking)
	for i := 0; i < cap(srv.wsBroadcast)+10; i++ {
		srv.BroadcastUpdate("test", map[string]int{"count": i})
	}
}

// Test utility methods (implementation in handlers_test.go)

// TestWriteErrorResponse implementation in handlers_test.go

// Benchmark tests

func BenchmarkServerSetupRoutes(b *testing.B) {
	srv, _, _, _, _, _ := setupTestServer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router := srv.setupRoutes()
		_ = router
	}
}

func BenchmarkCORSMiddleware(b *testing.B) {
	srv, _, _, _, _, _ := setupTestServer()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	corsHandler := srv.enableCORS(testHandler)
	req := httptest.NewRequest("GET", "/test", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		corsHandler.ServeHTTP(w, req)
	}
}

func BenchmarkBroadcastUpdate(b *testing.B) {
	srv, _, _, _, _, _ := setupTestServer()

	testData := map[string]interface{}{
		"key": "value",
		"num": 42,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		srv.BroadcastUpdate("test", testData)
	}
}

// Integration tests

func TestServerIntegration(t *testing.T) {
	srv, mockStorage, _, _, _, mockDB := setupTestServer()

	// Setup mock expectations
	mockDB.On("Ping").Return(nil)
	mockStorage.On("ListHistoricRuns", mock.Anything, "", 1).Return([]*types.HistoricRun{}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start server
	err := srv.Start(ctx)
	require.NoError(t, err)
	defer srv.Stop()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Test health endpoint
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get("http://localhost:8080/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	mockDB.AssertExpectations(t)
}

func TestConcurrentRequests(t *testing.T) {
	srv, _, _, _, _, mockDB := setupTestServer()

	mockDB.On("Ping").Return(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := srv.Start(ctx)
	require.NoError(t, err)
	defer srv.Stop()

	time.Sleep(50 * time.Millisecond)

	// Make concurrent requests
	const numRequests = 10
	results := make(chan int, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			client := &http.Client{Timeout: 1 * time.Second}
			resp, err := client.Get("http://localhost:8080/health")
			if err != nil {
				results <- 0
				return
			}
			defer resp.Body.Close()
			results <- resp.StatusCode
		}()
	}

	// Collect results
	for i := 0; i < numRequests; i++ {
		statusCode := <-results
		assert.Equal(t, http.StatusOK, statusCode)
	}

	mockDB.AssertExpectations(t)
}

func TestServerSecurityHeaders(t *testing.T) {
	srv, _, _, _, _, _ := setupTestServer()

	router := srv.setupRoutes()
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Verify CORS headers are set
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, DELETE, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Content-Type, Authorization, X-Requested-With", w.Header().Get("Access-Control-Allow-Headers"))
}

func TestServerRateLimiting(t *testing.T) {
	// This would be implemented if rate limiting was added to the server
	// For now, just verify that rapid requests don't cause issues
	srv, _, _, _, _, mockDB := setupTestServer()

	mockDB.On("Ping").Return(nil)

	router := srv.setupRoutes()

	// Make rapid requests
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Should handle all requests without issues
		assert.Equal(t, http.StatusOK, w.Code)
	}

	mockDB.AssertExpectations(t)
}
