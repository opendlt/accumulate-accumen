package l0api

import (
	"context"
	"encoding/json"
	"fmt"
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