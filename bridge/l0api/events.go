package l0api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/opendlt/accumulate-accumen/internal/logz"
)

// TxConfirmation represents a transaction confirmation from the WebSocket
type TxConfirmation struct {
	TxID   string `json:"txid"`
	Status string `json:"status"` // "pending", "executed", "failed"
	Code   int    `json:"code"`
	Log    string `json:"log"`
}

// Status represents the current status of a transaction subscription
type Status struct {
	TxID      string    `json:"txid"`
	Status    string    `json:"status"`
	Code      int       `json:"code"`
	Log       string    `json:"log"`
	Timestamp time.Time `json:"timestamp"`
	Final     bool      `json:"final"` // true if this is the final status (executed/failed)
}

// subscription represents an active transaction subscription
type subscription struct {
	txid     string
	ch       chan Status
	cancel   context.CancelFunc
	ctx      context.Context
	created  time.Time
}

// ManagerConfig defines configuration for the events manager
type ManagerConfig struct {
	// WebSocket connection settings
	WSEndpoint        string
	ReconnectMinDelay time.Duration
	ReconnectMaxDelay time.Duration
	MaxReconnects     int // 0 = unlimited

	// Connection health settings
	PingInterval     time.Duration
	PingTimeout      time.Duration
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration

	// Subscription settings
	SubscriptionTimeout time.Duration
	MaxSubscriptions    int
	BufferSize          int

	// Backoff jitter
	JitterFactor float64 // 0.0 to 1.0, amount of randomness in backoff
}

// DefaultManagerConfig returns a default configuration for the events manager
func DefaultManagerConfig(wsEndpoint string) *ManagerConfig {
	return &ManagerConfig{
		WSEndpoint:          wsEndpoint,
		ReconnectMinDelay:   1 * time.Second,
		ReconnectMaxDelay:   60 * time.Second,
		MaxReconnects:       0, // unlimited
		PingInterval:        30 * time.Second,
		PingTimeout:         10 * time.Second,
		ReadTimeout:         60 * time.Second,
		WriteTimeout:        10 * time.Second,
		SubscriptionTimeout: 5 * time.Minute,
		MaxSubscriptions:    1000,
		BufferSize:          100,
		JitterFactor:        0.1,
	}
}

// Manager manages WebSocket connections with auto-reconnect and individual subscription channels
type Manager struct {
	mu              sync.RWMutex
	config          *ManagerConfig
	conn            *websocket.Conn
	subscriptions   map[string]*subscription
	connected       bool
	reconnectCount  int
	logger          *logz.Logger
	ctx             context.Context
	cancel          context.CancelFunc
	shutdownCh      chan struct{}
	doneCh          chan struct{}

	// Connection state
	lastConnectTime time.Time
	totalReconnects int
	messageCount    uint64
}

// EventSubscriber manages WebSocket connections for transaction confirmations
type EventSubscriber struct {
	mu              sync.RWMutex
	wsURL           string
	conn            *websocket.Conn
	confirmationCh  chan *TxConfirmation
	subscriptions   map[string]bool // Track which TxIDs we're subscribed to
	logger          *logz.Logger
	reconnectDelay  time.Duration
	pingInterval    time.Duration
	maxReconnects   int
	connected       bool
}

// NewEventSubscriber creates a new WebSocket event subscriber
func NewEventSubscriber(apiURL string) (*EventSubscriber, error) {
	// Convert HTTP URL to WebSocket URL
	u, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("invalid API URL: %w", err)
	}

	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return nil, fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}

	// Add WebSocket endpoint path
	u.Path = "/v3/ws"

	return &EventSubscriber{
		wsURL:          u.String(),
		confirmationCh: make(chan *TxConfirmation, 100), // Buffered channel
		subscriptions:  make(map[string]bool),
		logger:         logz.New(logz.INFO, "l0api-events"),
		reconnectDelay: 5 * time.Second,
		pingInterval:   30 * time.Second,
		maxReconnects:  10,
		connected:      false,
	}, nil
}

// Start begins the WebSocket connection and message processing
func (es *EventSubscriber) Start(ctx context.Context) error {
	es.logger.Info("Starting WebSocket event subscriber: %s", es.wsURL)

	go es.connectionLoop(ctx)
	return nil
}

// Stop closes the WebSocket connection and cleanup
func (es *EventSubscriber) Stop() error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if es.conn != nil {
		es.conn.Close()
		es.conn = nil
	}

	es.connected = false
	close(es.confirmationCh)
	es.logger.Info("WebSocket event subscriber stopped")
	return nil
}

// GetConfirmationChannel returns the channel for receiving confirmations
func (es *EventSubscriber) GetConfirmationChannel() <-chan *TxConfirmation {
	return es.confirmationCh
}

// SubscribeToTx subscribes to status updates for a specific transaction ID
func (es *EventSubscriber) SubscribeToTx(txid string) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if es.subscriptions[txid] {
		return nil // Already subscribed
	}

	if !es.connected || es.conn == nil {
		es.logger.Debug("Not connected, queueing subscription for %s", txid)
		es.subscriptions[txid] = true
		return nil
	}

	// Send subscription message
	subMsg := map[string]interface{}{
		"method": "subscribe",
		"params": map[string]interface{}{
			"query": fmt.Sprintf("tx.hash='%s'", txid),
		},
	}

	if err := es.conn.WriteJSON(subMsg); err != nil {
		es.logger.Info("Failed to send subscription for %s: %v", txid, err)
		return fmt.Errorf("failed to send subscription: %w", err)
	}

	es.subscriptions[txid] = true
	es.logger.Debug("Subscribed to transaction: %s", txid)
	return nil
}

// UnsubscribeFromTx unsubscribes from a specific transaction ID
func (es *EventSubscriber) UnsubscribeFromTx(txid string) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if !es.subscriptions[txid] {
		return nil // Not subscribed
	}

	if es.connected && es.conn != nil {
		// Send unsubscription message
		unsubMsg := map[string]interface{}{
			"method": "unsubscribe",
			"params": map[string]interface{}{
				"query": fmt.Sprintf("tx.hash='%s'", txid),
			},
		}

		if err := es.conn.WriteJSON(unsubMsg); err != nil {
			es.logger.Info("Failed to send unsubscription for %s: %v", txid, err)
		}
	}

	delete(es.subscriptions, txid)
	es.logger.Debug("Unsubscribed from transaction: %s", txid)
	return nil
}

// connectionLoop manages the WebSocket connection with automatic reconnection
func (es *EventSubscriber) connectionLoop(ctx context.Context) {
	reconnectAttempts := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Attempt to connect
		if err := es.connect(); err != nil {
			es.logger.Info("Failed to connect to WebSocket: %v", err)
			reconnectAttempts++

			if reconnectAttempts >= es.maxReconnects {
				es.logger.Info("Max reconnection attempts reached, stopping")
				return
			}

			// Wait before retrying
			select {
			case <-ctx.Done():
				return
			case <-time.After(es.reconnectDelay):
				continue
			}
		}

		// Reset reconnect attempts on successful connection
		reconnectAttempts = 0

		// Handle messages until connection is lost
		es.handleConnection(ctx)

		// Mark as disconnected
		es.mu.Lock()
		es.connected = false
		if es.conn != nil {
			es.conn.Close()
			es.conn = nil
		}
		es.mu.Unlock()

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-time.After(es.reconnectDelay):
		}
	}
}

// connect establishes the WebSocket connection
func (es *EventSubscriber) connect() error {
	es.logger.Debug("Connecting to WebSocket: %s", es.wsURL)

	conn, _, err := websocket.DefaultDialer.Dial(es.wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to dial WebSocket: %w", err)
	}

	es.mu.Lock()
	es.conn = conn
	es.connected = true
	es.mu.Unlock()

	// Resubscribe to all existing subscriptions
	go es.resubscribeAll()

	es.logger.Info("Connected to WebSocket successfully")
	return nil
}

// resubscribeAll resubscribes to all previously subscribed transactions
func (es *EventSubscriber) resubscribeAll() {
	es.mu.RLock()
	txids := make([]string, 0, len(es.subscriptions))
	for txid := range es.subscriptions {
		txids = append(txids, txid)
	}
	es.mu.RUnlock()

	for _, txid := range txids {
		if err := es.SubscribeToTx(txid); err != nil {
			es.logger.Info("Failed to resubscribe to %s: %v", txid, err)
		}
	}

	es.logger.Debug("Resubscribed to %d transactions", len(txids))
}

// handleConnection handles WebSocket messages and ping/pong
func (es *EventSubscriber) handleConnection(ctx context.Context) {
	// Start ping ticker
	pingTicker := time.NewTicker(es.pingInterval)
	defer pingTicker.Stop()

	// Set read deadline for pongs
	es.conn.SetPongHandler(func(appData string) error {
		es.conn.SetReadDeadline(time.Now().Add(es.pingInterval * 2))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		case <-pingTicker.C:
			// Send ping
			if err := es.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				es.logger.Debug("Failed to send ping: %v", err)
				return
			}
		default:
			// Read messages
			es.conn.SetReadDeadline(time.Now().Add(es.pingInterval * 2))
			messageType, data, err := es.conn.ReadMessage()
			if err != nil {
				es.logger.Debug("WebSocket read error: %v", err)
				return
			}

			if messageType == websocket.TextMessage {
				es.handleMessage(data)
			}
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (es *EventSubscriber) handleMessage(data []byte) {
	es.logger.Debug("Received WebSocket message: %s", string(data))

	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		es.logger.Info("Failed to unmarshal message: %v", err)
		return
	}

	// Check if this is an event message
	if result, ok := msg["result"].(map[string]interface{}); ok {
		if events, ok := result["events"].([]interface{}); ok {
			for _, event := range events {
				if eventMap, ok := event.(map[string]interface{}); ok {
					es.processEvent(eventMap)
				}
			}
		}
	}
}

// processEvent processes individual transaction events
func (es *EventSubscriber) processEvent(event map[string]interface{}) {
	// Extract transaction hash
	var txid string
	if attributes, ok := event["attributes"].([]interface{}); ok {
		for _, attr := range attributes {
			if attrMap, ok := attr.(map[string]interface{}); ok {
				if key, ok := attrMap["key"].(string); ok && key == "tx.hash" {
					if value, ok := attrMap["value"].(string); ok {
						txid = value
						break
					}
				}
			}
		}
	}

	if txid == "" {
		es.logger.Debug("Event without transaction hash, ignoring")
		return
	}

	// Extract status information
	status := "pending"
	code := 0
	log := ""

	// Determine status from event type
	if eventType, ok := event["type"].(string); ok {
		switch eventType {
		case "tx":
			status = "executed"
		case "tx_fail":
			status = "failed"
		default:
			status = "pending"
		}
	}

	// Extract code and log if available
	if codeVal, ok := event["code"].(float64); ok {
		code = int(codeVal)
	}
	if logVal, ok := event["log"].(string); ok {
		log = logVal
	}

	// Create confirmation
	confirmation := &TxConfirmation{
		TxID:   txid,
		Status: status,
		Code:   code,
		Log:    log,
	}

	// Send to confirmation channel
	select {
	case es.confirmationCh <- confirmation:
		es.logger.Debug("Sent confirmation for %s: %s", txid, status)
	default:
		es.logger.Info("Confirmation channel full, dropping confirmation for %s", txid)
	}
}

// WaitForConfirmation waits for a specific transaction to be confirmed
func (es *EventSubscriber) WaitForConfirmation(ctx context.Context, txid string, timeout time.Duration) (*TxConfirmation, error) {
	// Subscribe to the transaction
	if err := es.SubscribeToTx(txid); err != nil {
		return nil, fmt.Errorf("failed to subscribe to transaction %s: %w", txid, err)
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Wait for confirmation
	for {
		select {
		case <-timeoutCtx.Done():
			es.UnsubscribeFromTx(txid)
			if timeoutCtx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("timeout waiting for confirmation of transaction %s", txid)
			}
			return nil, timeoutCtx.Err()

		case confirmation := <-es.confirmationCh:
			if confirmation.TxID == txid {
				es.UnsubscribeFromTx(txid)
				return confirmation, nil
			}
		}
	}
}

// GetConnectionStatus returns whether the WebSocket is connected
func (es *EventSubscriber) GetConnectionStatus() bool {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return es.connected
}

// GetSubscriptionCount returns the number of active subscriptions
func (es *EventSubscriber) GetSubscriptionCount() int {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return len(es.subscriptions)
}

// EventSubscriberStats contains statistics about the event subscriber
type EventSubscriberStats struct {
	Connected       bool `json:"connected"`
	SubscriptionCount int  `json:"subscription_count"`
	WebSocketURL    string `json:"websocket_url"`
}

// GetStats returns event subscriber statistics
func (es *EventSubscriber) GetStats() *EventSubscriberStats {
	es.mu.RLock()
	defer es.mu.RUnlock()

	return &EventSubscriberStats{
		Connected:         es.connected,
		SubscriptionCount: len(es.subscriptions),
		WebSocketURL:      es.wsURL,
	}
}

// NewManager creates a new hardened events manager
func NewManager(config *ManagerConfig) *Manager {
	if config == nil {
		config = DefaultManagerConfig("")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		config:        config,
		subscriptions: make(map[string]*subscription),
		logger:        logz.New(logz.INFO, "events-manager"),
		ctx:           ctx,
		cancel:        cancel,
		shutdownCh:    make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
}

// Start begins the manager with auto-reconnect loop
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Starting events manager: %s", m.config.WSEndpoint)

	// Start the main connection loop in a goroutine
	go m.connectionLoop(ctx)

	return nil
}

// Stop gracefully shuts down the manager and closes all subscriptions
func (m *Manager) Stop() error {
	m.logger.Info("Stopping events manager")

	// Signal shutdown
	close(m.shutdownCh)

	// Cancel context
	if m.cancel != nil {
		m.cancel()
	}

	// Close all subscription channels
	m.mu.Lock()
	for _, sub := range m.subscriptions {
		if sub.cancel != nil {
			sub.cancel()
		}
		close(sub.ch)
	}
	m.subscriptions = make(map[string]*subscription)
	m.mu.Unlock()

	// Close connection
	m.mu.Lock()
	if m.conn != nil {
		m.conn.Close()
		m.conn = nil
	}
	m.connected = false
	m.mu.Unlock()

	// Wait for connection loop to exit
	<-m.doneCh

	m.logger.Info("Events manager stopped")
	return nil
}

// SubscribeTx subscribes to status updates for a transaction and returns a channel and cancel function
func (m *Manager) SubscribeTx(txid string) (<-chan Status, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already subscribed
	if existing, exists := m.subscriptions[txid]; exists {
		m.logger.Debug("Transaction %s already subscribed", txid)
		return existing.ch, existing.cancel
	}

	// Check subscription limits
	if len(m.subscriptions) >= m.config.MaxSubscriptions {
		m.logger.Info("Maximum subscriptions reached (%d), cannot subscribe to %s",
			m.config.MaxSubscriptions, txid)
		// Return a closed channel and no-op cancel
		ch := make(chan Status)
		close(ch)
		return ch, func() {}
	}

	// Create subscription
	subCtx, cancel := context.WithTimeout(m.ctx, m.config.SubscriptionTimeout)
	ch := make(chan Status, m.config.BufferSize)

	sub := &subscription{
		txid:    txid,
		ch:      ch,
		cancel:  cancel,
		ctx:     subCtx,
		created: time.Now(),
	}

	m.subscriptions[txid] = sub

	// If connected, subscribe immediately
	if m.connected && m.conn != nil {
		go m.subscribeToTransaction(txid)
	}

	m.logger.Debug("Created subscription for transaction %s", txid)

	// Return channel and cancel function
	cancelFunc := func() {
		m.unsubscribeTx(txid)
	}

	return ch, cancelFunc
}

// connectionLoop manages the WebSocket connection with exponential backoff and jitter
func (m *Manager) connectionLoop(ctx context.Context) {
	defer close(m.doneCh)

	backoffDelay := m.config.ReconnectMinDelay

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.shutdownCh:
			return
		default:
		}

		// Attempt to connect
		if err := m.connect(ctx); err != nil {
			m.logger.Info("Failed to connect to WebSocket: %v", err)
			m.reconnectCount++
			m.totalReconnects++

			// Check max reconnects
			if m.config.MaxReconnects > 0 && m.reconnectCount >= m.config.MaxReconnects {
				m.logger.Info("Max reconnection attempts (%d) reached, stopping", m.config.MaxReconnects)
				return
			}

			// Calculate backoff with jitter
			jitteredDelay := m.addJitter(backoffDelay)
			m.logger.Debug("Reconnecting in %v (attempt %d)", jitteredDelay, m.reconnectCount)

			// Wait before retrying
			select {
			case <-ctx.Done():
				return
			case <-m.shutdownCh:
				return
			case <-time.After(jitteredDelay):
			}

			// Increase backoff delay
			backoffDelay = time.Duration(float64(backoffDelay) * 1.5)
			if backoffDelay > m.config.ReconnectMaxDelay {
				backoffDelay = m.config.ReconnectMaxDelay
			}

			continue
		}

		// Reset backoff on successful connection
		backoffDelay = m.config.ReconnectMinDelay
		m.reconnectCount = 0
		m.lastConnectTime = time.Now()

		// Handle the connection
		m.handleConnection(ctx)

		// Mark as disconnected
		m.mu.Lock()
		m.connected = false
		if m.conn != nil {
			m.conn.Close()
			m.conn = nil
		}
		m.mu.Unlock()

		m.logger.Debug("WebSocket connection lost")
	}
}

// connect establishes the WebSocket connection
func (m *Manager) connect(ctx context.Context) error {
	m.logger.Debug("Connecting to WebSocket: %s", m.config.WSEndpoint)

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, _, err := dialer.DialContext(ctx, m.config.WSEndpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to dial WebSocket: %w", err)
	}

	m.mu.Lock()
	m.conn = conn
	m.connected = true
	m.mu.Unlock()

	// Configure connection timeouts
	conn.SetReadDeadline(time.Now().Add(m.config.ReadTimeout))
	conn.SetWriteDeadline(time.Now().Add(m.config.WriteTimeout))

	// Resubscribe to all active subscriptions
	go m.resubscribeAll()

	m.logger.Info("Connected to WebSocket successfully")
	return nil
}

// resubscribeAll resubscribes to all active transactions after reconnection
func (m *Manager) resubscribeAll() {
	m.mu.RLock()
	txids := make([]string, 0, len(m.subscriptions))
	for txid := range m.subscriptions {
		txids = append(txids, txid)
	}
	m.mu.RUnlock()

	for _, txid := range txids {
		m.subscribeToTransaction(txid)
	}

	m.logger.Debug("Resubscribed to %d transactions after reconnect", len(txids))
}

// subscribeToTransaction sends a subscription message for a specific transaction
func (m *Manager) subscribeToTransaction(txid string) {
	m.mu.RLock()
	conn := m.conn
	connected := m.connected
	m.mu.RUnlock()

	if !connected || conn == nil {
		m.logger.Debug("Not connected, cannot subscribe to %s", txid)
		return
	}

	subMsg := map[string]interface{}{
		"method": "subscribe",
		"params": map[string]interface{}{
			"query": fmt.Sprintf("tx.hash='%s'", txid),
		},
	}

	conn.SetWriteDeadline(time.Now().Add(m.config.WriteTimeout))
	if err := conn.WriteJSON(subMsg); err != nil {
		m.logger.Info("Failed to send subscription for %s: %v", txid, err)
		return
	}

	m.logger.Debug("Subscribed to transaction: %s", txid)
}

// unsubscribeTx removes a subscription and closes its channel
func (m *Manager) unsubscribeTx(txid string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub, exists := m.subscriptions[txid]
	if !exists {
		return
	}

	// Send unsubscription if connected
	if m.connected && m.conn != nil {
		unsubMsg := map[string]interface{}{
			"method": "unsubscribe",
			"params": map[string]interface{}{
				"query": fmt.Sprintf("tx.hash='%s'", txid),
			},
		}

		m.conn.SetWriteDeadline(time.Now().Add(m.config.WriteTimeout))
		if err := m.conn.WriteJSON(unsubMsg); err != nil {
			m.logger.Debug("Failed to send unsubscription for %s: %v", txid, err)
		}
	}

	// Cancel context and close channel
	if sub.cancel != nil {
		sub.cancel()
	}
	close(sub.ch)
	delete(m.subscriptions, txid)

	m.logger.Debug("Unsubscribed from transaction: %s", txid)
}

// handleConnection manages the WebSocket connection and message processing
func (m *Manager) handleConnection(ctx context.Context) {
	pingTicker := time.NewTicker(m.config.PingInterval)
	defer pingTicker.Stop()

	// Set pong handler
	m.conn.SetPongHandler(func(appData string) error {
		m.conn.SetReadDeadline(time.Now().Add(m.config.ReadTimeout))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.shutdownCh:
			return
		case <-pingTicker.C:
			// Send ping
			m.conn.SetWriteDeadline(time.Now().Add(m.config.WriteTimeout))
			if err := m.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				m.logger.Debug("Failed to send ping: %v", err)
				return
			}
		default:
			// Read messages
			m.conn.SetReadDeadline(time.Now().Add(m.config.ReadTimeout))
			messageType, data, err := m.conn.ReadMessage()
			if err != nil {
				m.logger.Debug("WebSocket read error: %v", err)
				return
			}

			if messageType == websocket.TextMessage {
				m.messageCount++
				m.handleMessage(data)
			}
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (m *Manager) handleMessage(data []byte) {
	m.logger.Debug("Received WebSocket message: %s", string(data))

	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		m.logger.Info("Failed to unmarshal message: %v", err)
		return
	}

	// Check if this is an event message
	if result, ok := msg["result"].(map[string]interface{}); ok {
		if events, ok := result["events"].([]interface{}); ok {
			for _, event := range events {
				if eventMap, ok := event.(map[string]interface{}); ok {
					m.processEvent(eventMap)
				}
			}
		}
	}
}

// processEvent processes individual transaction events and sends to appropriate channels
func (m *Manager) processEvent(event map[string]interface{}) {
	// Extract transaction hash
	var txid string
	if attributes, ok := event["attributes"].([]interface{}); ok {
		for _, attr := range attributes {
			if attrMap, ok := attr.(map[string]interface{}); ok {
				if key, ok := attrMap["key"].(string); ok && key == "tx.hash" {
					if value, ok := attrMap["value"].(string); ok {
						txid = value
						break
					}
				}
			}
		}
	}

	if txid == "" {
		return
	}

	// Extract status information
	status := "pending"
	code := 0
	log := ""
	final := false

	// Determine status from event type
	if eventType, ok := event["type"].(string); ok {
		switch eventType {
		case "tx":
			status = "executed"
			final = true
		case "tx_fail":
			status = "failed"
			final = true
		default:
			status = "pending"
		}
	}

	// Extract code and log if available
	if codeVal, ok := event["code"].(float64); ok {
		code = int(codeVal)
	}
	if logVal, ok := event["log"].(string); ok {
		log = logVal
	}

	// Create status
	statusUpdate := Status{
		TxID:      txid,
		Status:    status,
		Code:      code,
		Log:       log,
		Timestamp: time.Now(),
		Final:     final,
	}

	// Send to appropriate subscription channel
	m.mu.RLock()
	sub, exists := m.subscriptions[txid]
	m.mu.RUnlock()

	if !exists {
		return
	}

	// Send status update, don't block
	select {
	case sub.ch <- statusUpdate:
		m.logger.Debug("Sent status update for %s: %s", txid, status)

		// If this is a final status, clean up the subscription
		if final {
			go func() {
				time.Sleep(100 * time.Millisecond) // Give subscribers time to read
				m.unsubscribeTx(txid)
			}()
		}
	default:
		m.logger.Debug("Status channel full for %s, dropping update", txid)
	}
}

// addJitter adds random jitter to backoff delay
func (m *Manager) addJitter(delay time.Duration) time.Duration {
	if m.config.JitterFactor <= 0 {
		return delay
	}

	// Generate random bytes for jitter
	var buf [8]byte
	rand.Read(buf[:])

	// Convert to float64 between 0 and 1
	jitter := float64(buf[0]) / 255.0

	// Apply jitter factor
	jitterAmount := float64(delay) * m.config.JitterFactor * jitter

	return delay + time.Duration(jitterAmount)
}

// GetStats returns manager statistics
func (m *Manager) GetStats() *ManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return &ManagerStats{
		Connected:         m.connected,
		SubscriptionCount: len(m.subscriptions),
		ReconnectCount:    m.reconnectCount,
		TotalReconnects:   m.totalReconnects,
		LastConnectTime:   m.lastConnectTime,
		MessageCount:      m.messageCount,
		WSEndpoint:        m.config.WSEndpoint,
	}
}

// ManagerStats contains statistics about the events manager
type ManagerStats struct {
	Connected         bool      `json:"connected"`
	SubscriptionCount int       `json:"subscription_count"`
	ReconnectCount    int       `json:"reconnect_count"`
	TotalReconnects   int       `json:"total_reconnects"`
	LastConnectTime   time.Time `json:"last_connect_time"`
	MessageCount      uint64    `json:"message_count"`
	WSEndpoint        string    `json:"ws_endpoint"`
}

// IsHealthy returns true if the manager is healthy (connected and functional)
func (m *Manager) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.connected && time.Since(m.lastConnectTime) < 5*time.Minute
}