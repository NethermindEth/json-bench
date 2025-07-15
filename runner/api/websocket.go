package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/types"
)

// WSMessageType represents different types of WebSocket messages
type WSMessageType string

const (
	// Connection management messages
	WSMessageTypeConnection    WSMessageType = "connection"
	WSMessageTypePing          WSMessageType = "ping"
	WSMessageTypePong          WSMessageType = "pong"
	WSMessageTypeError         WSMessageType = "error"
	WSMessageTypeDisconnection WSMessageType = "disconnection"

	// Real-time update messages
	WSMessageTypeNewRun             WSMessageType = "new_run"
	WSMessageTypeRegressionDetected WSMessageType = "regression_detected"
	WSMessageTypeBaselineUpdated    WSMessageType = "baseline_updated"
	WSMessageTypeAnalysisComplete   WSMessageType = "analysis_complete"

	// Status messages
	WSMessageTypeRunStarted  WSMessageType = "run_started"
	WSMessageTypeRunProgress WSMessageType = "run_progress"
	WSMessageTypeRunComplete WSMessageType = "run_complete"
	WSMessageTypeRunFailed   WSMessageType = "run_failed"
)

// WSMessage represents a WebSocket message structure
type WSMessage struct {
	Type      WSMessageType `json:"type"`
	Data      interface{}   `json:"data,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
	ID        string        `json:"id,omitempty"`
	ClientID  string        `json:"client_id,omitempty"`
	Error     *WSError      `json:"error,omitempty"`
}

// WSError represents error information in WebSocket messages
type WSError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// WSClient represents a connected WebSocket client
type WSClient struct {
	ID          string
	Conn        *websocket.Conn
	Send        chan []byte
	Hub         *WSHub
	RemoteAddr  string
	UserAgent   string
	ConnectedAt time.Time
	LastPing    time.Time
	mu          sync.RWMutex
}

// WSHub manages WebSocket connections and message broadcasting
type WSHub struct {
	// Client management
	clients    map[*WSClient]bool
	register   chan *WSClient
	unregister chan *WSClient

	// Message broadcasting
	broadcast chan []byte

	// Subscription management
	subscriptions map[*WSClient]map[string]bool // client -> subscription topics

	// Configuration
	config WSHubConfig

	// Dependencies
	log logrus.FieldLogger

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup
	mu     sync.RWMutex
}

// WSHubConfig holds configuration for the WebSocket hub
type WSHubConfig struct {
	// Connection limits
	MaxClients     int           `json:"max_clients"`
	WriteTimeout   time.Duration `json:"write_timeout"`
	ReadTimeout    time.Duration `json:"read_timeout"`
	PingInterval   time.Duration `json:"ping_interval"`
	PongTimeout    time.Duration `json:"pong_timeout"`
	MaxMessageSize int64         `json:"max_message_size"`

	// Buffer sizes
	ClientBufferSize    int `json:"client_buffer_size"`
	BroadcastBufferSize int `json:"broadcast_buffer_size"`

	// Connection management
	EnablePingPong      bool `json:"enable_ping_pong"`
	DisconnectOnError   bool `json:"disconnect_on_error"`
	LogConnectionEvents bool `json:"log_connection_events"`
}

// NewWSHub creates a new WebSocket hub instance
func NewWSHub(log logrus.FieldLogger) *WSHub {
	ctx, cancel := context.WithCancel(context.Background())

	// Default configuration
	config := WSHubConfig{
		MaxClients:          100,
		WriteTimeout:        10 * time.Second,
		ReadTimeout:         60 * time.Second,
		PingInterval:        54 * time.Second,
		PongTimeout:         60 * time.Second,
		MaxMessageSize:      512 * 1024, // 512KB
		ClientBufferSize:    256,
		BroadcastBufferSize: 1000,
		EnablePingPong:      true,
		DisconnectOnError:   true,
		LogConnectionEvents: true,
	}

	return &WSHub{
		clients:       make(map[*WSClient]bool),
		register:      make(chan *WSClient, config.MaxClients),
		unregister:    make(chan *WSClient, config.MaxClients),
		broadcast:     make(chan []byte, config.BroadcastBufferSize),
		subscriptions: make(map[*WSClient]map[string]bool),
		config:        config,
		log:           log.WithField("component", "websocket-hub"),
		ctx:           ctx,
		cancel:        cancel,
		done:          make(chan struct{}),
	}
}

// Run starts the WebSocket hub and manages client connections
func (h *WSHub) Run(ctx context.Context) error {
	h.log.Info("Starting WebSocket hub")

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.runHub()
	}()

	// Start ping/pong handler if enabled
	if h.config.EnablePingPong {
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			h.runPingPongHandler()
		}()
	}

	h.log.WithFields(logrus.Fields{
		"max_clients":     h.config.MaxClients,
		"ping_interval":   h.config.PingInterval,
		"enable_pingpong": h.config.EnablePingPong,
	}).Info("WebSocket hub started successfully")

	return nil
}

// Stop gracefully shuts down the WebSocket hub
func (h *WSHub) Stop() error {
	h.log.Info("Stopping WebSocket hub")

	// Cancel context to stop all goroutines
	h.cancel()

	// Close all client connections
	h.mu.Lock()
	for client := range h.clients {
		h.closeClient(client)
	}
	h.mu.Unlock()

	// Close channels
	close(h.register)
	close(h.unregister)
	close(h.broadcast)

	// Wait for all goroutines to finish
	h.wg.Wait()

	close(h.done)
	h.log.Info("WebSocket hub stopped")
	return nil
}

// RegisterClient registers a new WebSocket client
func (h *WSHub) RegisterClient(conn *websocket.Conn, clientID, remoteAddr, userAgent string) *WSClient {
	client := &WSClient{
		ID:          clientID,
		Conn:        conn,
		Send:        make(chan []byte, h.config.ClientBufferSize),
		Hub:         h,
		RemoteAddr:  remoteAddr,
		UserAgent:   userAgent,
		ConnectedAt: time.Now(),
		LastPing:    time.Now(),
	}

	// Configure connection
	conn.SetReadLimit(h.config.MaxMessageSize)
	conn.SetReadDeadline(time.Now().Add(h.config.ReadTimeout))
	conn.SetPongHandler(func(string) error {
		client.mu.Lock()
		client.LastPing = time.Now()
		client.mu.Unlock()
		conn.SetReadDeadline(time.Now().Add(h.config.ReadTimeout))
		return nil
	})

	// Register client
	select {
	case h.register <- client:
		h.log.WithFields(logrus.Fields{
			"client_id":   clientID,
			"remote_addr": remoteAddr,
			"user_agent":  userAgent,
		}).Info("WebSocket client registered")
	case <-h.ctx.Done():
		h.log.Warn("Cannot register client, hub is shutting down")
		client.close()
		return nil
	default:
		h.log.Warn("Client registration channel full, rejecting connection")
		client.close()
		return nil
	}

	// Start client handlers
	go client.writePump()
	go client.readPump()

	return client
}

// BroadcastToAll sends a message to all connected clients
func (h *WSHub) BroadcastToAll(messageType WSMessageType, data interface{}) {
	h.broadcastMessage(messageType, data, nil)
}

// BroadcastToSubscribers sends a message to clients subscribed to specific topics
func (h *WSHub) BroadcastToSubscribers(messageType WSMessageType, data interface{}, topics []string) {
	h.broadcastMessage(messageType, data, topics)
}

// NotifyNewRun broadcasts notification about a new benchmark run
func (h *WSHub) NotifyNewRun(run *types.HistoricRun) {
	data := map[string]interface{}{
		"run_id":     run.ID,
		"test_name":  run.TestName,
		"timestamp":  run.Timestamp,
		"git_commit": run.GitCommit,
		"status":     "completed",
		"summary": map[string]interface{}{
			"total_requests":     run.TotalRequests,
			"total_errors":       run.TotalErrors,
			"overall_error_rate": run.OverallErrorRate,
			"avg_latency_ms":     run.AvgLatencyMs,
			"p95_latency_ms":     run.P95LatencyMs,
			"best_client":        run.BestClient,
			"performance_scores": run.PerformanceScores,
		},
	}

	h.BroadcastToAll(WSMessageTypeNewRun, data)
	h.log.WithField("run_id", run.ID).Info("Broadcasted new run notification")
}

// NotifyRegression broadcasts notification about detected performance regression
func (h *WSHub) NotifyRegression(regression *types.Regression, run *types.HistoricRun) {
	data := map[string]interface{}{
		"regression_id":   regression.ID,
		"run_id":          regression.RunID,
		"baseline_run_id": regression.BaselineRunID,
		"client":          regression.Client,
		"metric":          regression.Metric,
		"method":          regression.Method,
		"severity":        regression.Severity,
		"percent_change":  regression.PercentChange,
		"absolute_change": regression.AbsoluteChange,
		"baseline_value":  regression.BaselineValue,
		"current_value":   regression.CurrentValue,
		"is_significant":  regression.IsSignificant,
		"p_value":         regression.PValue,
		"detected_at":     regression.DetectedAt,
		"run_info": map[string]interface{}{
			"test_name":  run.TestName,
			"git_commit": run.GitCommit,
			"timestamp":  run.Timestamp,
		},
	}

	h.BroadcastToAll(WSMessageTypeRegressionDetected, data)
	h.log.WithFields(logrus.Fields{
		"regression_id": regression.ID,
		"run_id":        regression.RunID,
		"client":        regression.Client,
		"severity":      regression.Severity,
	}).Warn("Broadcasted regression detection notification")
}

// NotifyBaselineUpdated broadcasts notification about baseline update
func (h *WSHub) NotifyBaselineUpdated(baselineName, runID, testName string) {
	data := map[string]interface{}{
		"baseline_name": baselineName,
		"run_id":        runID,
		"test_name":     testName,
		"updated_at":    time.Now(),
	}

	h.BroadcastToAll(WSMessageTypeBaselineUpdated, data)
	h.log.WithFields(logrus.Fields{
		"baseline_name": baselineName,
		"run_id":        runID,
		"test_name":     testName,
	}).Info("Broadcasted baseline update notification")
}

// NotifyAnalysisComplete broadcasts notification about completed analysis
func (h *WSHub) NotifyAnalysisComplete(runID, testName string, analysisResults interface{}) {
	data := map[string]interface{}{
		"run_id":           runID,
		"test_name":        testName,
		"analysis_results": analysisResults,
		"completed_at":     time.Now(),
	}

	h.BroadcastToAll(WSMessageTypeAnalysisComplete, data)
	h.log.WithFields(logrus.Fields{
		"run_id":    runID,
		"test_name": testName,
	}).Info("Broadcasted analysis completion notification")
}

// GetConnectedClientsCount returns the number of currently connected clients
func (h *WSHub) GetConnectedClientsCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// GetClientInfo returns information about connected clients
func (h *WSHub) GetClientInfo() []map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var clients []map[string]interface{}
	for client := range h.clients {
		client.mu.RLock()
		clientInfo := map[string]interface{}{
			"id":           client.ID,
			"remote_addr":  client.RemoteAddr,
			"user_agent":   client.UserAgent,
			"connected_at": client.ConnectedAt,
			"last_ping":    client.LastPing,
		}
		client.mu.RUnlock()
		clients = append(clients, clientInfo)
	}

	return clients
}

// Internal methods

// runHub manages the main hub loop
func (h *WSHub) runHub() {
	ticker := time.NewTicker(30 * time.Second) // Cleanup ticker
	defer ticker.Stop()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if len(h.clients) >= h.config.MaxClients {
				h.mu.Unlock()
				h.log.Warn("Maximum client limit reached, rejecting connection")
				client.close()
				continue
			}
			h.clients[client] = true
			h.subscriptions[client] = make(map[string]bool)
			h.mu.Unlock()

			// Send welcome message
			welcomeMsg := WSMessage{
				Type:      WSMessageTypeConnection,
				Data:      map[string]interface{}{"status": "connected", "client_id": client.ID},
				Timestamp: time.Now(),
				ClientID:  client.ID,
			}
			client.sendMessage(welcomeMsg)

			if h.config.LogConnectionEvents {
				h.log.WithFields(logrus.Fields{
					"client_id":     client.ID,
					"remote_addr":   client.RemoteAddr,
					"total_clients": len(h.clients),
				}).Info("WebSocket client connected")
			}

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				delete(h.subscriptions, client)
				h.mu.Unlock()
				h.closeClient(client)

				if h.config.LogConnectionEvents {
					h.log.WithFields(logrus.Fields{
						"client_id":     client.ID,
						"remote_addr":   client.RemoteAddr,
						"total_clients": len(h.clients),
					}).Info("WebSocket client disconnected")
				}
			} else {
				h.mu.Unlock()
			}

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					// Client's send channel is full, close the connection
					delete(h.clients, client)
					delete(h.subscriptions, client)
					h.closeClient(client)
				}
			}
			h.mu.RUnlock()

		case <-ticker.C:
			// Perform periodic cleanup
			h.cleanupDeadConnections()

		case <-h.ctx.Done():
			return
		}
	}
}

// runPingPongHandler manages ping/pong heartbeat mechanism
func (h *WSHub) runPingPongHandler() {
	ticker := time.NewTicker(h.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.sendPingToAllClients()
		case <-h.ctx.Done():
			return
		}
	}
}

// sendPingToAllClients sends ping messages to all connected clients
func (h *WSHub) sendPingToAllClients() {
	h.mu.RLock()
	for client := range h.clients {
		go func(c *WSClient) {
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				h.unregister <- c
			}
		}(client)
	}
	h.mu.RUnlock()
}

// cleanupDeadConnections removes clients that haven't responded to pings
func (h *WSHub) cleanupDeadConnections() {
	if !h.config.EnablePingPong {
		return
	}

	now := time.Now()
	deadline := now.Add(-h.config.PongTimeout)

	h.mu.RLock()
	var deadClients []*WSClient
	for client := range h.clients {
		client.mu.RLock()
		if client.LastPing.Before(deadline) {
			deadClients = append(deadClients, client)
		}
		client.mu.RUnlock()
	}
	h.mu.RUnlock()

	for _, client := range deadClients {
		h.log.WithField("client_id", client.ID).Warn("Removing dead WebSocket connection")
		h.unregister <- client
	}
}

// broadcastMessage sends a message to clients based on subscription topics
func (h *WSHub) broadcastMessage(messageType WSMessageType, data interface{}, topics []string) {
	message := WSMessage{
		Type:      messageType,
		Data:      data,
		Timestamp: time.Now(),
	}

	msgBytes, err := json.Marshal(message)
	if err != nil {
		h.log.WithError(err).Error("Failed to marshal broadcast message")
		return
	}

	if topics == nil {
		// Broadcast to all clients
		select {
		case h.broadcast <- msgBytes:
		default:
			h.log.Warn("Broadcast channel full, dropping message")
		}
	} else {
		// Broadcast to subscribed clients only
		h.mu.RLock()
		for client := range h.clients {
			subscribed := false
			for _, topic := range topics {
				if h.subscriptions[client][topic] {
					subscribed = true
					break
				}
			}
			if subscribed {
				select {
				case client.Send <- msgBytes:
				default:
					// Client's send channel is full
					h.log.WithField("client_id", client.ID).Warn("Client send channel full")
				}
			}
		}
		h.mu.RUnlock()
	}
}

// closeClient safely closes a client connection
func (h *WSHub) closeClient(client *WSClient) {
	close(client.Send)
	client.Conn.Close()
}

// WSClient methods

// sendMessage sends a message to this specific client
func (c *WSClient) sendMessage(message WSMessage) {
	if c.Send == nil {
		c.Hub.log.WithField("client_id", c.ID).Warn("Client send channel is nil")
		return
	}

	msgBytes, err := json.Marshal(message)
	if err != nil {
		c.Hub.log.WithError(err).Error("Failed to marshal client message")
		return
	}

	select {
	case c.Send <- msgBytes:
	default:
		c.Hub.log.WithField("client_id", c.ID).Warn("Client send channel full")
	}
}

// readPump handles incoming messages from the client
func (c *WSClient) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(c.Hub.config.MaxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(c.Hub.config.ReadTimeout))
	c.Conn.SetPongHandler(func(string) error {
		c.mu.Lock()
		c.LastPing = time.Now()
		c.mu.Unlock()
		c.Conn.SetReadDeadline(time.Now().Add(c.Hub.config.ReadTimeout))
		return nil
	})

	for {
		var msg WSMessage
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.Hub.log.WithError(err).WithField("client_id", c.ID).Error("WebSocket read error")
			}
			break
		}

		// Handle different message types
		switch msg.Type {
		case WSMessageTypePing:
			pongMsg := WSMessage{
				Type:      WSMessageTypePong,
				Timestamp: time.Now(),
				ClientID:  c.ID,
			}
			c.sendMessage(pongMsg)

		case WSMessageTypePong:
			c.mu.Lock()
			c.LastPing = time.Now()
			c.mu.Unlock()

		default:
			// Handle other message types as needed
			c.Hub.log.WithFields(logrus.Fields{
				"client_id":    c.ID,
				"message_type": msg.Type,
			}).Debug("Received WebSocket message")
		}
	}
}

// writePump handles outgoing messages to the client
func (c *WSClient) writePump() {
	ticker := time.NewTicker(c.Hub.config.PingInterval)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(c.Hub.config.WriteTimeout))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				c.Hub.log.WithError(err).WithField("client_id", c.ID).Error("WebSocket write error")
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(c.Hub.config.WriteTimeout))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.Hub.ctx.Done():
			return
		}
	}
}

// close closes the client connection
func (c *WSClient) close() {
	c.Conn.Close()
}

// HandleWebSocketConnection creates a WebSocket handler for HTTP server integration
func (h *WSHub) HandleWebSocketConnection(upgrader *websocket.Upgrader) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			h.log.WithError(err).Error("Failed to upgrade WebSocket connection")
			return
		}

		// Generate client ID
		clientID := generateClientID()
		remoteAddr := r.RemoteAddr
		userAgent := r.UserAgent()

		// Register the client
		client := h.RegisterClient(conn, clientID, remoteAddr, userAgent)
		if client == nil {
			h.log.Warn("Failed to register WebSocket client")
			conn.Close()
			return
		}

		h.log.WithFields(logrus.Fields{
			"client_id":   clientID,
			"remote_addr": remoteAddr,
		}).Info("WebSocket connection established")
	}
}

// generateClientID creates a unique client identifier
func generateClientID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if random fails
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405")))
	}
	return hex.EncodeToString(bytes)
}
