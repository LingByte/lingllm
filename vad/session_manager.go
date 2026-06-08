// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package vad

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// DefaultSessionManager 默认会话管理器实现
type DefaultSessionManager struct {
	detector      Detector
	sessions      map[string]*Session
	mu            sync.RWMutex
	logger        *zap.Logger
	ttl           time.Duration
	cleanupTicker *time.Ticker
	stopChan      chan struct{}
	cleanupOnce   sync.Once
	maxSessions   int
}

// NewDefaultSessionManager 创建新的会话管理器
func NewDefaultSessionManager(detector Detector, config *Config, logger *zap.Logger) (*DefaultSessionManager, error) {
	if detector == nil {
		return nil, fmt.Errorf("detector is required")
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	ttl := config.SessionTTL
	if ttl == 0 {
		ttl = 5 * time.Minute
	}

	maxSessions := 10000
	if opts, ok := config.Options["max_sessions"].(int); ok {
		maxSessions = opts
	}

	sm := &DefaultSessionManager{
		detector:    detector,
		sessions:    make(map[string]*Session),
		logger:      logger,
		ttl:         ttl,
		stopChan:    make(chan struct{}),
		maxSessions: maxSessions,
	}

	// 启动清理 goroutine
	sm.startCleanup()

	return sm, nil
}

// startCleanup 启动过期会话清理
func (sm *DefaultSessionManager) startCleanup() {
	sm.cleanupTicker = time.NewTicker(1 * time.Minute)

	go func() {
		for {
			select {
			case <-sm.cleanupTicker.C:
				sm.cleanup()
			case <-sm.stopChan:
				sm.cleanupTicker.Stop()
				return
			}
		}
	}()
}

// cleanup 清理过期会话
func (sm *DefaultSessionManager) cleanup() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	expiredCount := 0

	for id, session := range sm.sessions {
		if now.Sub(session.LastActivityAt) > sm.ttl {
			delete(sm.sessions, id)
			expiredCount++
			sm.logger.Debug("expired session cleaned up", zap.String("session_id", id))
		}
	}

	if expiredCount > 0 {
		sm.logger.Info("session cleanup completed", zap.Int("expired_count", expiredCount))
	}
}

// GetOrCreateSession 获取或创建会话
func (sm *DefaultSessionManager) GetOrCreateSession(sessionID string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[sessionID]; exists {
		session.LastActivityAt = time.Now()
		return session
	}

	// 检查是否超过最大会话数
	if len(sm.sessions) >= sm.maxSessions {
		sm.logger.Warn("max sessions reached", zap.Int("max", sm.maxSessions))
		return nil
	}

	session := &Session{
		ID:             sessionID,
		CreatedAt:      time.Now(),
		LastActivityAt: time.Now(),
		Metadata:       make(map[string]interface{}),
	}

	sm.sessions[sessionID] = session
	sm.logger.Debug("session created", zap.String("session_id", sessionID))

	return session
}

// ProcessAudio 处理音频数据
func (sm *DefaultSessionManager) ProcessAudio(
	ctx context.Context,
	sessionID string,
	audioData []byte,
	format string,
	threshold ...float64,
) (*DetectResponse, error) {
	session := sm.GetOrCreateSession(sessionID)
	if session == nil {
		return nil, fmt.Errorf("failed to create session")
	}

	// 构建检测请求
	req := &DetectRequest{
		AudioData:   audioData,
		AudioFormat: format,
		SampleRate:  16000,
		Channels:    1,
		SessionID:   sessionID,
		Timestamp:   time.Now(),
	}

	if len(threshold) > 0 && threshold[0] > 0 {
		req.Threshold = threshold[0]
	}

	// 调用检测器
	result, err := sm.detector.Detect(ctx, req)
	if err != nil {
		sm.logger.Error("detect failed", zap.Error(err), zap.String("session_id", sessionID))
		return nil, err
	}

	// 更新会话状态
	sm.mu.Lock()
	if sess, exists := sm.sessions[sessionID]; exists {
		sess.HaveVoice = result.HaveVoice
		sess.VoiceStop = result.VoiceStop
		sess.LastSpeechProb = result.SpeechProb
		sess.LastActivityAt = time.Now()
	}
	sm.mu.Unlock()

	return result, nil
}

// GetSession 获取会话
func (sm *DefaultSessionManager) GetSession(sessionID string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[sessionID]
}

// ResetSession 重置会话
func (sm *DefaultSessionManager) ResetSession(ctx context.Context, sessionID string) error {
	sm.mu.Lock()
	if session, exists := sm.sessions[sessionID]; exists {
		session.HaveVoice = false
		session.VoiceStop = false
		session.LastSpeechProb = 0
		session.LastActivityAt = time.Now()
	}
	sm.mu.Unlock()

	// 调用检测器的重置方法
	if httpDetector, ok := sm.detector.(*HTTPDetector); ok {
		return httpDetector.ResetSession(ctx, sessionID)
	}

	if wsDetector, ok := sm.detector.(*WebSocketDetector); ok {
		return wsDetector.ResetSession(ctx, sessionID)
	}

	return nil
}

// DeleteSession 删除会话
func (sm *DefaultSessionManager) DeleteSession(ctx context.Context, sessionID string) error {
	sm.mu.Lock()
	delete(sm.sessions, sessionID)
	sm.mu.Unlock()

	sm.logger.Debug("session deleted", zap.String("session_id", sessionID))

	// 调用检测器的重置方法
	if httpDetector, ok := sm.detector.(*HTTPDetector); ok {
		return httpDetector.ResetSession(ctx, sessionID)
	}

	if wsDetector, ok := sm.detector.(*WebSocketDetector); ok {
		return wsDetector.ResetSession(ctx, sessionID)
	}

	return nil
}

// ListSessions 列出所有活跃会话
func (sm *DefaultSessionManager) ListSessions() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessions := make([]string, 0, len(sm.sessions))
	for id := range sm.sessions {
		sessions = append(sessions, id)
	}
	return sessions
}

// SetTTL 设置会话过期时间
func (sm *DefaultSessionManager) SetTTL(ttl time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.ttl = ttl
}

// GetStats 获取统计信息
func (sm *DefaultSessionManager) GetStats() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return map[string]interface{}{
		"total_sessions": len(sm.sessions),
		"max_sessions":   sm.maxSessions,
		"ttl":            sm.ttl.String(),
	}
}

// Close 关闭会话管理器
func (sm *DefaultSessionManager) Close() error {
	sm.cleanupOnce.Do(func() {
		close(sm.stopChan)
	})

	sm.mu.Lock()
	sm.sessions = make(map[string]*Session)
	sm.mu.Unlock()

	return nil
}
