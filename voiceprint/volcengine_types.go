// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package voiceprint

import (
	"time"
)

// ============ Volcengine Types ============

// VolcengineRegisterRequest 火山引擎注册请求
type VolcengineRegisterRequest struct {
	Audio    string `json:"Audio"`    // Base64编码的WAV音频数据
	MetaInfo string `json:"MetaInfo"` // 声纹元信息，可选
	AudioName string `json:"AudioName"` // 音频样本名称
}

// VolcengineRegisterResponse 火山引擎注册响应
type VolcengineRegisterResponse struct {
	ResponseMetadata VolcengineResponseMetadata `json:"ResponseMetadata"`
	Result           VolcengineRegisterResult   `json:"Result"`
}

// VolcengineRegisterResult 注册结果
type VolcengineRegisterResult struct {
	UUID string `json:"UUID"` // 注册成功的声纹唯一标识符
}

// VolcengineQueryRequest 火山引擎查询请求
type VolcengineQueryRequest struct {
	UUIDs    []string `json:"UUIDs,omitempty"`    // 声纹ID列表，可选
	Limit    int      `json:"Limit,omitempty"`    // 每页返回的最大结果数量，默认50
	Iterator string   `json:"Iterator,omitempty"` // 分页标识
}

// VolcengineQueryResponse 火山引擎查询响应
type VolcengineQueryResponse struct {
	ResponseMetadata VolcengineResponseMetadata `json:"ResponseMetadata"`
	Result           VolcengineQueryResult      `json:"Result"`
}

// VolcengineQueryResult 查询结果
type VolcengineQueryResult struct {
	VoicePrints []VolcengineVoicePrint `json:"VoicePrints,omitempty"` // 声纹对象数组
	NextIterator string                `json:"NextIterator,omitempty"` // 下一页的分页标识
}

// VolcengineVoicePrint 声纹对象
type VolcengineVoicePrint struct {
	UUID       string `json:"UUID,omitempty"`       // 声纹唯一标识符
	MetaInfo   string `json:"MetaInfo,omitempty"`   // 声纹元信息
	AccountID  string `json:"AccountID,omitempty"`  // 火山引擎账户ID
	AudioName  string `json:"AudioName,omitempty"`  // 音频样本名称
	CreatedAt  int64  `json:"CreatedAt,omitempty"`  // 创建时间，Unix时间戳（毫秒）
	UpdatedAt  int64  `json:"UpdatedAt,omitempty"`  // 最后更新时间，Unix时间戳（毫秒）
	VoicePrint string `json:"VoicePrint,omitempty"` // 声纹特征数据
}

// VolcengineUpdateRequest 火山引擎更新请求
type VolcengineUpdateRequest struct {
	UUID      string `json:"UUID"`                // 要更新的声纹唯一标识符
	Audio     string `json:"Audio,omitempty"`     // Base64编码的WAV音频数据，可选
	MetaInfo  string `json:"MetaInfo,omitempty"`  // 声纹元信息，可选
	AudioName string `json:"AudioName,omitempty"` // 音频样本名称，可选
}

// VolcengineUpdateResponse 火山引擎更新响应
type VolcengineUpdateResponse struct {
	ResponseMetadata VolcengineResponseMetadata `json:"ResponseMetadata"`
	Result           interface{}                `json:"Result"`
}

// VolcengineDeleteRequest 火山引擎删除请求
type VolcengineDeleteRequest struct {
	UUID string `json:"UUID"` // 要删除的声纹唯一标识符
}

// VolcengineDeleteResponse 火山引擎删除响应
type VolcengineDeleteResponse struct {
	ResponseMetadata VolcengineResponseMetadata `json:"ResponseMetadata"`
	Result           interface{}                `json:"Result"`
}

// VolcengineResponseMetadata 响应元数据
type VolcengineResponseMetadata struct {
	Action    string `json:"Action"`    // 接口名称
	Region    string `json:"Region"`    // 区域
	Service   string `json:"Service"`   // 服务名称
	Version   string `json:"Version"`   // 接口版本
	RequestID string `json:"RequestId"` // 请求ID
}

// VolcengineError 火山引擎错误响应
type VolcengineError struct {
	ResponseMetadata VolcengineResponseMetadata `json:"ResponseMetadata"`
	Error            VolcengineErrorDetail      `json:"Error"`
}

// VolcengineErrorDetail 错误详情
type VolcengineErrorDetail struct {
	Code    string `json:"Code"`    // 错误码
	Message string `json:"Message"` // 错误信息
}

// VolcengineVoicePrintInfo 声纹信息（用于统一接口）
type VolcengineVoicePrintInfo struct {
	UUID       string    // 声纹唯一标识符
	MetaInfo   string    // 声纹元信息
	AudioName  string    // 音频样本名称
	CreatedAt  time.Time // 创建时间
	UpdatedAt  time.Time // 最后更新时间
	VoicePrint string    // 声纹特征数据
}
