# WebSocket Implementation for JSON-RPC Benchmark Runner

## Overview

This implementation provides complete WebSocket support for real-time communication between the benchmark runner and dashboard clients. The implementation follows the 🐼 ethPandaOps 🐼 Go coding standards and provides production-ready WebSocket functionality.

## Files

- `websocket.go` - Main WebSocket hub implementation
- `websocket_test.go` - Comprehensive tests for WebSocket functionality
- `websocket_integration_example.go` - Integration example showing how to use the WebSocket hub
- `WEBSOCKET_README.md` - This documentation file

## Features

### WSHub Struct
- **Client Management**: Handles connection lifecycle, registration, and cleanup
- **Message Broadcasting**: Supports both global and topic-based message broadcasting
- **Subscription Management**: Allows clients to subscribe to specific message types
- **Connection Limits**: Configurable maximum client connections
- **Heartbeat Mechanism**: Ping/pong heartbeat to detect dead connections
- **Graceful Shutdown**: Proper cleanup of all connections and goroutines

### Message Types
The implementation supports the following WebSocket message types:

#### Connection Management
- `connection` - Initial connection acknowledgment
- `ping` - Heartbeat ping messages
- `pong` - Heartbeat pong responses
- `error` - Error notifications
- `disconnection` - Clean disconnection notifications

#### Real-time Updates
- `new_run` - Notification about new benchmark runs
- `regression_detected` - Performance regression alerts
- `baseline_updated` - Baseline update notifications
- `analysis_complete` - Analysis completion notifications

#### Status Messages
- `run_started` - Benchmark run started
- `run_progress` - Progress updates during runs
- `run_complete` - Benchmark run completed
- `run_failed` - Benchmark run failed

### Configuration Options

The WSHub supports extensive configuration:

```go
type WSHubConfig struct {
    MaxClients          int           // Maximum concurrent connections (default: 100)
    WriteTimeout        time.Duration // Write timeout (default: 10s)
    ReadTimeout         time.Duration // Read timeout (default: 60s)
    PingInterval        time.Duration // Ping interval (default: 54s)
    PongTimeout         time.Duration // Pong timeout (default: 60s)
    MaxMessageSize      int64         // Maximum message size (default: 512KB)
    ClientBufferSize    int           // Client send buffer size (default: 256)
    BroadcastBufferSize int           // Broadcast buffer size (default: 1000)
    EnablePingPong      bool          // Enable heartbeat (default: true)
    DisconnectOnError   bool          // Disconnect on errors (default: true)
    LogConnectionEvents bool          // Log connection events (default: true)
}
```

## API Reference

### Core Methods

#### `NewWSHub(log logrus.FieldLogger) *WSHub`
Creates a new WebSocket hub instance with default configuration.

#### `Run(ctx context.Context) error`
Starts the WebSocket hub and begins handling client connections.

#### `Stop() error`
Gracefully shuts down the hub, closing all connections and cleaning up resources.

#### `RegisterClient(conn *websocket.Conn, clientID, remoteAddr, userAgent string) *WSClient`
Registers a new WebSocket client connection.

### Broadcasting Methods

#### `BroadcastToAll(messageType WSMessageType, data interface{})`
Sends a message to all connected clients.

#### `BroadcastToSubscribers(messageType WSMessageType, data interface{}, topics []string)`
Sends a message to clients subscribed to specific topics.

### Notification Methods

#### `NotifyNewRun(run *types.HistoricRun)`
Broadcasts notification about a new benchmark run completion.

#### `NotifyRegression(regression *types.Regression, run *types.HistoricRun)`
Broadcasts notification about detected performance regressions.

#### `NotifyBaselineUpdated(baselineName, runID, testName string)`
Broadcasts notification about baseline updates.

#### `NotifyAnalysisComplete(runID, testName string, analysisResults interface{})`
Broadcasts notification about completed analysis.

### Utility Methods

#### `GetConnectedClientsCount() int`
Returns the current number of connected clients.

#### `GetClientInfo() []map[string]interface{}`
Returns detailed information about all connected clients.

#### `HandleWebSocketConnection(upgrader *websocket.Upgrader) func(w http.ResponseWriter, r *http.Request)`
Returns an HTTP handler function for WebSocket endpoint integration.

## Integration Guide

### 1. Initialize WebSocket Hub

```go
// Create WebSocket hub
wsHub := NewWSHub(log)

// Start the hub
err := wsHub.Run(ctx)
if err != nil {
    return fmt.Errorf("failed to start WebSocket hub: %w", err)
}
```

### 2. Add WebSocket Endpoint

```go
// Create WebSocket upgrader
upgrader := websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true // Configure based on your security requirements
    },
}

// Add WebSocket endpoint to router
router.HandleFunc("/api/ws", wsHub.HandleWebSocketConnection(&upgrader))
```

### 3. Add Notifications Throughout Application

```go
// When a benchmark run completes
wsHub.NotifyNewRun(run)

// When regressions are detected
wsHub.NotifyRegression(regression, run)

// When baselines are updated
wsHub.NotifyBaselineUpdated(baselineName, runID, testName)

// When analysis completes
wsHub.NotifyAnalysisComplete(runID, testName, results)
```

### 4. Graceful Shutdown

```go
// Stop WebSocket hub during application shutdown
err := wsHub.Stop()
if err != nil {
    log.WithError(err).Error("Failed to stop WebSocket hub")
}
```

## Client-Side Usage

### JavaScript Example

```javascript
const ws = new WebSocket('ws://localhost:8080/api/ws');

ws.onopen = function(event) {
    console.log('WebSocket connected');
};

ws.onmessage = function(event) {
    const message = JSON.parse(event.data);
    
    switch(message.type) {
        case 'new_run':
            handleNewRun(message.data);
            break;
        case 'regression_detected':
            handleRegression(message.data);
            break;
        case 'baseline_updated':
            handleBaselineUpdate(message.data);
            break;
        case 'analysis_complete':
            handleAnalysisComplete(message.data);
            break;
        case 'ping':
            // Respond to ping with pong
            ws.send(JSON.stringify({type: 'pong', timestamp: new Date()}));
            break;
    }
};

ws.onclose = function(event) {
    console.log('WebSocket disconnected');
    // Implement reconnection logic here
};

ws.onerror = function(error) {
    console.error('WebSocket error:', error);
};
```

## Message Format

All WebSocket messages follow this structure:

```json
{
    "type": "message_type",
    "data": {
        // Message-specific data
    },
    "timestamp": "2023-12-01T10:00:00Z",
    "id": "optional-message-id",
    "client_id": "optional-client-id",
    "error": {
        "code": 500,
        "message": "Error description",
        "details": "Additional error details"
    }
}
```

## Error Handling

The implementation includes comprehensive error handling:

- **Connection Errors**: Automatic cleanup of failed connections
- **Message Errors**: Graceful handling of malformed messages
- **Buffer Overflow**: Protection against client buffer overflows
- **Timeout Handling**: Configurable timeouts for reads and writes
- **Panic Recovery**: Goroutine panic recovery to prevent crashes

## Performance Considerations

- **Concurrency**: Uses goroutines for handling multiple clients efficiently
- **Memory Management**: Proper cleanup of resources to prevent memory leaks
- **Buffer Sizes**: Configurable buffer sizes to optimize for your use case
- **Connection Limits**: Configurable maximum connections to prevent resource exhaustion
- **Heartbeat**: Efficient dead connection detection and cleanup

## Security Considerations

- **Origin Checking**: Configure `CheckOrigin` function in upgrader for production use
- **Rate Limiting**: Consider adding rate limiting for message broadcasting
- **Authentication**: Add authentication/authorization as needed for your use case
- **Message Validation**: Validate incoming messages before processing

## Testing

Run the tests to verify functionality:

```bash
go test -v ./runner/api/websocket_test.go ./runner/api/websocket.go
```

All tests should pass, verifying:
- Hub creation and configuration
- Start/stop lifecycle
- Message broadcasting
- Notification methods
- Client ID generation
- Message type definitions

## Monitoring

The implementation provides monitoring capabilities:

- **Connection Count**: Track number of active connections
- **Client Information**: Get detailed client connection info
- **Message Statistics**: Monitor message send/receive rates
- **Error Tracking**: Log and track connection errors

## Production Deployment

For production deployment, consider:

1. **Load Balancing**: Use Redis or similar for multi-instance WebSocket support
2. **SSL/TLS**: Enable secure WebSocket connections (wss://)
3. **Monitoring**: Add metrics collection for connection and message statistics
4. **Logging**: Configure appropriate log levels for production
5. **Resource Limits**: Set appropriate connection and message limits
6. **Health Checks**: Implement health check endpoints for monitoring

This implementation provides a solid foundation for real-time communication in the JSON-RPC benchmark runner and can be extended as needed for additional features.