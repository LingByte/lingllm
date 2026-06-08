// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package vad

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// WebSocketDetector WebSocket VAD 检测器
type WebSocketDetector struct {
	baseURL    string
	logger     *zap.Logger
	config     *Config
	mu         sync.RWMutex
	conn       *websocket.Conn
	connected  bool
	closeChan  chan struct{}
	respChan   map[string]chan *DetectResponse
	respMuLock sync.RWMutex
}

// NewWebSocketDetector 创建新的 WebSocket VAD 检测器
func NewWebSocketDetector(config *Config, logger *zap.Logger) (*WebSocketDetector, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if config.BaseURL == "" {
		return nil, fmt.Errorf("base_url is required for WebSocket detector")
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	detector := &WebSocketDetector{
		baseURL:  config.BaseURL,
		logger:   logger,
		config:   config,
		closeChan: make(chan struct{}),
		respChan: make(map[string]chan *DetectResponse),
	}

	// 连接到 WebSocket 服务
	if err := detector.connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	// 启动消息读取 goroutine
	go detector.readMessages()

	return detector, nil
}

// connect 连接到 WebSocket 服务
func (d *WebSocketDetector) connect() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(d.baseURL, nil)
	if err != nil {
		d.logger.Error("failed to connect to WebSocket", zap.Error(err))
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	d.conn = conn
	d.connected = true
	d.logger.Info("WebSocket connected", zap.String("url", d.baseURL))

	return nil
}

// readMessages 读取 WebSocket 消息
func (d *WebSocketDetector) readMessages() {
	defer func() {
		d.mu.Lock()
		d.connected = false
		d.mu.Unlock()
	}()

	for {
		select {
		case <-d.closeChan:
			return
		default:
		}

		d.mu.RLock()
		conn := d.conn
		d.mu.RUnlock()

		if conn == nil {
			return
		}

		var msg WebSocketMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				d.logger.Error("WebSocket error", zap.Error(err))
			}
			return
		}

		// 处理响应
		if msg.Type == "vad_result" && msg.Result != nil {
			d.respMuLock.RLock()
			respChan, exists := d.respChan[msg.SessionID]
			d.respMuLock.RUnlock()

			if exists {
				select {
				case respChan <- msg.Result:
				case <-d.closeChan:
					return
				}
			}
		}
	}
}

// Detect 检测音频中的语音活动
func (d *WebSocketDetector) Detect(ctx context.Context, req *DetectRequest) (*DetectResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}

	if req.SessionID == "" {
		return nil, fmt.Errorf("session_id is required for WebSocket detector")
	}

	d.mu.RLock()
	connected := d.connected
	conn := d.conn
	d.mu.RUnlock()

	if !connected || conn == nil {
		return nil, fmt.Errorf("WebSocket not connected")
	}

	// 创建响应通道
	respChan := make(chan *DetectResponse, 1)
	d.respMuLock.Lock()
	d.respChan[req.SessionID] = respChan
	d.respMuLock.Unlock()

	defer func() {
		d.respMuLock.Lock()
		delete(d.respChan, req.SessionID)
		d.respMuLock.Unlock()
		close(respChan)
	}()

	// 编码音频数据
	audioBase64 := base64.StdEncoding.EncodeToString(req.AudioData)

	// 构建消息
	msg := WebSocketMessage{
		Type:      "audio",
		Data:      audioBase64,
		Format:    req.AudioFormat,
		SessionID: req.SessionID,
		Timestamp: time.Now(),
	}

	// 发送消息
	d.mu.Lock()
	err := d.conn.WriteJSON(msg)
	d.mu.Unlock()

	if err != nil {
		d.logger.Error("failed to send message", zap.Error(err))
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	// 等待响应
	timeout := d.config.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	select {
	case resp := <-respChan:
		if resp != nil {
			resp.Timestamp = time.Now()
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("detect timeout")
	case <-d.closeChan:
		return nil, fmt.Errorf("detector closed")
	}
}

// HealthCheck 健康检查
func (d *WebSocketDetector) HealthCheck(ctx context.Context) error {
	d.mu.RLock()
	connected := d.connected
	d.mu.RUnlock()

	if !connected {
		return fmt.Errorf("WebSocket not connected")
	}

	// 尝试发送 ping 消息
	d.mu.Lock()
	err := d.conn.WriteMessage(websocket.PingMessage, []byte{})
	d.mu.Unlock()

	if err != nil {
		d.logger.Error("health check failed", zap.Error(err))
		return fmt.Errorf("health check failed: %w", err)
	}

	d.logger.Info("WebSocket health check passed")
	return nil
}

// Close 关闭检测器
func (d *WebSocketDetector) Close() error {
	close(d.closeChan)

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.conn != nil {
		return d.conn.Close()
	}

	return nil
}

// Provider 返回提供商名称
func (d *WebSocketDetector) Provider() Provider {
	return ProviderWebSocket
}

// ResetSession 重置会话
func (d *WebSocketDetector) ResetSession(ctx context.Context, sessionID string) error {
	d.mu.RLock()
	connected := d.connected
	conn := d.conn
	d.mu.RUnlock()

	if !connected || conn == nil {
		return fmt.Errorf("WebSocket not connected")
	}

	msg := WebSocketMessage{
		Type:      "reset",
		SessionID: sessionID,
		Timestamp: time.Now(),
	}

	d.mu.Lock()
	err := d.conn.WriteJSON(msg)
	d.mu.Unlock()

	if err != nil {
		d.logger.Error("failed to reset session", zap.Error(err))
		return fmt.Errorf("failed to reset session: %w", err)
	}

	d.logger.Debug("session reset", zap.String("session_id", sessionID))
	return nil
}

// IsConnected 检查是否已连接
func (d *WebSocketDetector) IsConnected() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.connected
}
