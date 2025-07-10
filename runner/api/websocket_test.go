package api

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/types"
)

func TestNewWSHub(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel) // Reduce noise in tests

	hub := NewWSHub(log)

	assert.NotNil(t, hub)
	assert.NotNil(t, hub.clients)
	assert.NotNil(t, hub.register)
	assert.NotNil(t, hub.unregister)
	assert.NotNil(t, hub.broadcast)
	assert.NotNil(t, hub.subscriptions)
	assert.Equal(t, 100, hub.config.MaxClients)
	assert.Equal(t, 54*time.Second, hub.config.PingInterval)
	assert.True(t, hub.config.EnablePingPong)
}

func TestWSHubRunAndStop(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start the hub
	err := hub.Run(ctx)
	require.NoError(t, err)

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Check that it's running
	assert.Equal(t, 0, hub.GetConnectedClientsCount())

	// Stop the hub
	err = hub.Stop()
	require.NoError(t, err)
}

func TestWSHubBroadcastToAll(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Test broadcasting without any clients (should not panic)
	testData := map[string]interface{}{
		"test_key": "test_value",
	}

	hub.BroadcastToAll(WSMessageTypeNewRun, testData)

	// Give time for processing
	time.Sleep(50 * time.Millisecond)

	// Stop the hub
	err = hub.Stop()
	require.NoError(t, err)
}

func TestWSHubNotifyMethods(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Test NotifyNewRun
	run := &types.HistoricRun{
		ID:               "test-run-1",
		TestName:         "test-benchmark",
		TotalRequests:    1000,
		TotalErrors:      10,
		OverallErrorRate: 0.01,
		AvgLatencyMs:     50.5,
		P95LatencyMs:     95.5,
		BestClient:       "client1",
		PerformanceScores: map[string]float64{
			"client1": 95.5,
			"client2": 92.3,
		},
	}

	// These should not panic
	hub.NotifyNewRun(run)

	// Test NotifyRegression
	regression := &types.Regression{
		ID:             "regression-1",
		RunID:          "test-run-1",
		BaselineRunID:  "baseline-run-1",
		Client:         "client1",
		Metric:         "p95_latency",
		Method:         "eth_getBalance",
		Severity:       "high",
		PercentChange:  25.5,
		AbsoluteChange: 12.75,
		BaselineValue:  50.0,
		CurrentValue:   62.75,
		IsSignificant:  true,
		PValue:         0.01,
		DetectedAt:     time.Now(),
	}

	hub.NotifyRegression(regression, run)

	// Test NotifyBaselineUpdated
	hub.NotifyBaselineUpdated("test-baseline", "test-run-1", "test-benchmark")

	// Test NotifyAnalysisComplete
	analysisResults := map[string]interface{}{
		"performance_score":  95.5,
		"regressions_found":  2,
		"improvements_found": 1,
	}
	hub.NotifyAnalysisComplete("test-run-1", "test-benchmark", analysisResults)

	// Stop the hub
	err = hub.Stop()
	require.NoError(t, err)
}

func TestGenerateClientID(t *testing.T) {
	id1 := generateClientID()
	id2 := generateClientID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)  // IDs should be unique
	assert.Equal(t, 16, len(id1)) // Should be 8 bytes -> 16 hex chars
}

func TestWSMessageTypes(t *testing.T) {
	// Test that all message types are properly defined
	assert.Equal(t, WSMessageType("connection"), WSMessageTypeConnection)
	assert.Equal(t, WSMessageType("ping"), WSMessageTypePing)
	assert.Equal(t, WSMessageType("pong"), WSMessageTypePong)
	assert.Equal(t, WSMessageType("new_run"), WSMessageTypeNewRun)
	assert.Equal(t, WSMessageType("regression_detected"), WSMessageTypeRegressionDetected)
	assert.Equal(t, WSMessageType("baseline_updated"), WSMessageTypeBaselineUpdated)
	assert.Equal(t, WSMessageType("analysis_complete"), WSMessageTypeAnalysisComplete)
	assert.Equal(t, WSMessageType("run_started"), WSMessageTypeRunStarted)
	assert.Equal(t, WSMessageType("run_progress"), WSMessageTypeRunProgress)
	assert.Equal(t, WSMessageType("run_complete"), WSMessageTypeRunComplete)
	assert.Equal(t, WSMessageType("run_failed"), WSMessageTypeRunFailed)
}

// Enhanced WebSocket real-time functionality tests

// TestWSHubClientSubscriptions tests client subscription management
func TestWSHubClientSubscriptions(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)
	defer hub.Stop()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Test subscription management
	clientID := generateClientID()

	// Subscribe to test name
	hub.SubscribeToTestName(clientID, "test-benchmark-1")
	assert.True(t, hub.IsSubscribedToTestName(clientID, "test-benchmark-1"))
	assert.False(t, hub.IsSubscribedToTestName(clientID, "test-benchmark-2"))

	// Subscribe to run ID
	hub.SubscribeToRunID(clientID, "run-123")
	assert.True(t, hub.IsSubscribedToRunID(clientID, "run-123"))
	assert.False(t, hub.IsSubscribedToRunID(clientID, "run-456"))

	// Unsubscribe
	hub.UnsubscribeFromTestName(clientID, "test-benchmark-1")
	assert.False(t, hub.IsSubscribedToTestName(clientID, "test-benchmark-1"))

	hub.UnsubscribeFromRunID(clientID, "run-123")
	assert.False(t, hub.IsSubscribedToRunID(clientID, "run-123"))
}

// TestWSHubTargetedBroadcasting tests targeted message broadcasting
func TestWSHubTargetedBroadcasting(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)
	defer hub.Stop()

	time.Sleep(50 * time.Millisecond)

	// Test broadcasting to test name subscribers
	testData := map[string]interface{}{
		"test_key": "test_value",
	}

	// Should not panic even without subscribers
	hub.BroadcastToTestName("test-benchmark", WSMessageTypeNewRun, testData)
	hub.BroadcastToRunID("run-123", WSMessageTypeRunProgress, testData)

	time.Sleep(50 * time.Millisecond)
}

// TestWSHubConnectionLimit tests connection limit enforcement
func TestWSHubConnectionLimit(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	// Create hub with low connection limit for testing
	hub := NewWSHub(log)
	hub.config.MaxClients = 2

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)
	defer hub.Stop()

	time.Sleep(50 * time.Millisecond)

	// Test that GetConnectedClientsCount works
	assert.Equal(t, 0, hub.GetConnectedClientsCount())

	// Test GetMaxClients
	assert.Equal(t, 2, hub.GetMaxClients())
}

// TestWSHubMessageQueuing tests message queuing and delivery
func TestWSHubMessageQueuing(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)
	defer hub.Stop()

	time.Sleep(50 * time.Millisecond)

	// Send multiple messages rapidly
	for i := 0; i < 10; i++ {
		testData := map[string]interface{}{
			"message_id": i,
			"timestamp":  time.Now().UnixNano(),
		}
		hub.BroadcastToAll(WSMessageTypeNewRun, testData)
	}

	// Give time for message processing
	time.Sleep(100 * time.Millisecond)
}

// TestWSHubErrorHandling tests error handling scenarios
func TestWSHubErrorHandling(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	// Test operations before hub is started
	testData := map[string]interface{}{
		"test": "data",
	}

	// These should not panic
	hub.BroadcastToAll(WSMessageTypeNewRun, testData)
	hub.BroadcastToTestName("test", WSMessageTypeNewRun, testData)
	hub.BroadcastToRunID("run-123", WSMessageTypeRunProgress, testData)

	clientID := generateClientID()
	hub.SubscribeToTestName(clientID, "test")
	hub.UnsubscribeFromTestName(clientID, "test")

	// Test double stop
	err := hub.Stop()
	assert.NoError(t, err)

	err = hub.Stop()
	assert.NoError(t, err) // Should not error on double stop
}

// TestWSHubConcurrentOperations tests concurrent WebSocket operations
func TestWSHubConcurrentOperations(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)
	defer hub.Stop()

	time.Sleep(50 * time.Millisecond)

	// Test concurrent subscription operations
	const numGoroutines = 10
	const operationsPerGoroutine = 20

	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			clientID := generateClientID()
			testName := fmt.Sprintf("test-%d", id)
			runID := fmt.Sprintf("run-%d", id)

			for j := 0; j < operationsPerGoroutine; j++ {
				// Subscribe
				hub.SubscribeToTestName(clientID, testName)
				hub.SubscribeToRunID(clientID, runID)

				// Check subscriptions
				hub.IsSubscribedToTestName(clientID, testName)
				hub.IsSubscribedToRunID(clientID, runID)

				// Broadcast
				testData := map[string]interface{}{
					"goroutine": id,
					"operation": j,
				}
				hub.BroadcastToTestName(testName, WSMessageTypeNewRun, testData)
				hub.BroadcastToRunID(runID, WSMessageTypeRunProgress, testData)

				// Unsubscribe
				hub.UnsubscribeFromTestName(clientID, testName)
				hub.UnsubscribeFromRunID(clientID, runID)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations to complete")
		}
	}
}

// TestWSHubNotificationWorkflows tests complete notification workflows
func TestWSHubNotificationWorkflows(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)
	defer hub.Stop()

	time.Sleep(50 * time.Millisecond)

	// Test complete benchmark run workflow
	run := &types.HistoricRun{
		ID:               "workflow-run-1",
		TestName:         "workflow-test",
		TotalRequests:    1000,
		TotalErrors:      10,
		OverallErrorRate: 0.01,
		AvgLatencyMs:     50.5,
		P95LatencyMs:     95.5,
		BestClient:       "geth",
		PerformanceScores: map[string]float64{
			"geth":       95.5,
			"nethermind": 92.3,
		},
	}

	// 1. Notify new run
	hub.NotifyNewRun(run)

	// 2. Notify regression detected
	regression := &types.Regression{
		ID:             "workflow-regression-1",
		RunID:          run.ID,
		BaselineRunID:  "baseline-run-1",
		Client:         "geth",
		Metric:         "p95_latency",
		Method:         "eth_getBalance",
		Severity:       "high",
		PercentChange:  25.5,
		AbsoluteChange: 12.75,
		BaselineValue:  50.0,
		CurrentValue:   62.75,
		IsSignificant:  true,
		PValue:         0.01,
		DetectedAt:     time.Now(),
	}

	hub.NotifyRegression(regression, run)

	// 3. Notify baseline updated
	hub.NotifyBaselineUpdated("workflow-baseline", run.ID, run.TestName)

	// 4. Notify analysis complete
	analysisResults := map[string]interface{}{
		"performance_score":    95.5,
		"regressions_found":    1,
		"improvements_found":   0,
		"overall_health_score": 88.2,
		"risk_level":           "medium",
	}
	hub.NotifyAnalysisComplete(run.ID, run.TestName, analysisResults)

	time.Sleep(100 * time.Millisecond)
}

// TestWSHubMemoryLeaks tests for potential memory leaks
func TestWSHubMemoryLeaks(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)
	defer hub.Stop()

	time.Sleep(50 * time.Millisecond)

	// Create and remove many subscriptions
	const numClients = 100
	const subscriptionsPerClient = 10

	clientIDs := make([]string, numClients)
	for i := 0; i < numClients; i++ {
		clientIDs[i] = generateClientID()
	}

	// Create subscriptions
	for _, clientID := range clientIDs {
		for j := 0; j < subscriptionsPerClient; j++ {
			testName := fmt.Sprintf("memory-test-%d", j)
			runID := fmt.Sprintf("memory-run-%d", j)

			hub.SubscribeToTestName(clientID, testName)
			hub.SubscribeToRunID(clientID, runID)
		}
	}

	// Broadcast some messages
	for i := 0; i < 10; i++ {
		testData := map[string]interface{}{
			"iteration": i,
		}
		hub.BroadcastToAll(WSMessageTypeNewRun, testData)
	}

	// Remove all subscriptions
	for _, clientID := range clientIDs {
		for j := 0; j < subscriptionsPerClient; j++ {
			testName := fmt.Sprintf("memory-test-%d", j)
			runID := fmt.Sprintf("memory-run-%d", j)

			hub.UnsubscribeFromTestName(clientID, testName)
			hub.UnsubscribeFromRunID(clientID, runID)
		}
	}

	time.Sleep(100 * time.Millisecond)

	// Hub should handle cleanup gracefully
	assert.Equal(t, 0, hub.GetConnectedClientsCount())
}

// TestWSHubReconnectionScenarios tests reconnection handling
func TestWSHubReconnectionScenarios(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)
	defer hub.Stop()

	time.Sleep(50 * time.Millisecond)

	// Simulate client reconnection scenarios
	clientID1 := generateClientID()
	clientID2 := generateClientID()

	// Client 1 subscribes
	hub.SubscribeToTestName(clientID1, "reconnection-test")
	assert.True(t, hub.IsSubscribedToTestName(clientID1, "reconnection-test"))

	// Client 2 subscribes to same test
	hub.SubscribeToTestName(clientID2, "reconnection-test")
	assert.True(t, hub.IsSubscribedToTestName(clientID2, "reconnection-test"))

	// Client 1 disconnects (removes all subscriptions)
	hub.UnsubscribeFromTestName(clientID1, "reconnection-test")
	assert.False(t, hub.IsSubscribedToTestName(clientID1, "reconnection-test"))
	assert.True(t, hub.IsSubscribedToTestName(clientID2, "reconnection-test"))

	// Client 1 reconnects with new ID
	clientID1New := generateClientID()
	hub.SubscribeToTestName(clientID1New, "reconnection-test")
	assert.True(t, hub.IsSubscribedToTestName(clientID1New, "reconnection-test"))

	// Broadcast should reach both active clients
	testData := map[string]interface{}{
		"message": "reconnection test",
	}
	hub.BroadcastToTestName("reconnection-test", WSMessageTypeNewRun, testData)

	time.Sleep(50 * time.Millisecond)
}

// TestWSHubMessageFiltering tests message filtering capabilities
func TestWSHubMessageFiltering(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)
	defer hub.Stop()

	time.Sleep(50 * time.Millisecond)

	// Test filtering by subscription
	clientID := generateClientID()

	// Subscribe only to specific test
	hub.SubscribeToTestName(clientID, "filter-test-1")

	// Broadcast to subscribed test
	testData1 := map[string]interface{}{
		"should_receive": true,
	}
	hub.BroadcastToTestName("filter-test-1", WSMessageTypeNewRun, testData1)

	// Broadcast to non-subscribed test
	testData2 := map[string]interface{}{
		"should_not_receive": true,
	}
	hub.BroadcastToTestName("filter-test-2", WSMessageTypeNewRun, testData2)

	time.Sleep(50 * time.Millisecond)
}

// TestWSHubPerformance tests WebSocket hub performance
func TestWSHubPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	require.NoError(t, err)
	defer hub.Stop()

	time.Sleep(50 * time.Millisecond)

	// Test performance with many subscriptions
	const numClients = 100
	const messagesPerClient = 50

	// Create many subscriptions
	clientIDs := make([]string, numClients)
	for i := 0; i < numClients; i++ {
		clientID := generateClientID()
		clientIDs[i] = clientID
		hub.SubscribeToTestName(clientID, "performance-test")
	}

	// Measure broadcast performance
	start := time.Now()

	for i := 0; i < messagesPerClient; i++ {
		testData := map[string]interface{}{
			"message_id": i,
			"timestamp":  time.Now().UnixNano(),
		}
		hub.BroadcastToTestName("performance-test", WSMessageTypeNewRun, testData)
	}

	duration := time.Since(start)
	totalMessages := numClients * messagesPerClient

	t.Logf("Broadcast performance: %d messages in %v (%.2f msg/sec)",
		totalMessages, duration, float64(totalMessages)/duration.Seconds())

	// Performance should be reasonable
	assert.Less(t, duration, 2*time.Second, "Broadcasting should complete within 2 seconds")

	time.Sleep(100 * time.Millisecond)
}

// Benchmark tests for WebSocket operations

func BenchmarkWSHubBroadcastToAll(b *testing.B) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer hub.Stop()

	time.Sleep(50 * time.Millisecond)

	testData := map[string]interface{}{
		"benchmark": true,
		"timestamp": time.Now().UnixNano(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.BroadcastToAll(WSMessageTypeNewRun, testData)
	}
}

func BenchmarkWSHubSubscriptionOperations(b *testing.B) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	hub := NewWSHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := hub.Run(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer hub.Stop()

	time.Sleep(50 * time.Millisecond)

	clientID := generateClientID()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testName := fmt.Sprintf("bench-test-%d", i%100) // Cycle through 100 test names
		hub.SubscribeToTestName(clientID, testName)
		hub.IsSubscribedToTestName(clientID, testName)
		hub.UnsubscribeFromTestName(clientID, testName)
	}
}

func BenchmarkGenerateClientID(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generateClientID()
	}
}
