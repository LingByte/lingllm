// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package voiceprint

import (
	"encoding/json"
)

// XunfeiRequest 讯飞请求基础结构
type XunfeiRequest struct {
	Header    XunfeiHeader    `json:"header"`
	Parameter XunfeiParameter `json:"parameter"`
	Payload   *XunfeiPayload  `json:"payload,omitempty"`
}

// XunfeiHeader 讯飞请求头
type XunfeiHeader struct {
	AppID  string `json:"app_id"`
	Status int    `json:"status"` // 3 = 一次传完
}

// XunfeiParameter 讯飞参数
type XunfeiParameter struct {
	S782b4996 XunfeiServiceParam `json:"s782b4996"`
}

// XunfeiServiceParam 讯飞服务参数
type XunfeiServiceParam struct {
	Func string `json:"func"`

	// 创建特征库参数
	GroupID      string `json:"groupId,omitempty"`
	GroupName    string `json:"groupName,omitempty"`
	GroupInfo    string `json:"groupInfo,omitempty"`

	// 特征相关参数
	FeatureID   string `json:"featureId,omitempty"`
	FeatureInfo string `json:"featureInfo,omitempty"`
	DstFeatureID string `json:"dstFeatureId,omitempty"`

	// 查询参数
	TopK int `json:"topK,omitempty"`

	// 更新参数
	Cover bool `json:"cover,omitempty"`

	// 响应格式配置
	CreateGroupRes    *ResponseFormat `json:"createGroupRes,omitempty"`
	CreateFeatureRes  *ResponseFormat `json:"createFeatureRes,omitempty"`
	UpdateFeatureRes  *ResponseFormat `json:"updateFeatureRes,omitempty"`
	QueryFeatureListRes *ResponseFormat `json:"queryFeatureListRes,omitempty"`
	SearchScoreFeaRes *ResponseFormat `json:"searchScoreFeaRes,omitempty"`
	SearchFeaRes      *ResponseFormat `json:"searchFeaRes,omitempty"`
	DeleteFeatureRes  *ResponseFormat `json:"deleteFeatureRes,omitempty"`
	DeleteGroupRes    *ResponseFormat `json:"deleteGroupRes,omitempty"`
}

// ResponseFormat 响应格式配置
type ResponseFormat struct {
	Encoding string `json:"encoding"` // utf8
	Compress string `json:"compress"` // raw
	Format   string `json:"format"`   // json
}

// XunfeiPayload 讯飞请求负载
type XunfeiPayload struct {
	Resource *AudioResource `json:"resource,omitempty"`
}

// AudioResource 音频资源
type AudioResource struct {
	Encoding  string `json:"encoding"`   // lame
	SampleRate int   `json:"sample_rate"` // 16000
	Channels  int    `json:"channels"`    // 1
	BitDepth  int    `json:"bit_depth"`   // 16
	Status    int    `json:"status"`      // 3
	Audio     string `json:"audio"`       // base64编码的音频数据
}

// XunfeiResponse 讯飞响应基础结构
type XunfeiResponse struct {
	Header  XunfeiResponseHeader `json:"header"`
	Payload XunfeiResponsePayload `json:"payload"`
}

// XunfeiResponseHeader 讯飞响应头
type XunfeiResponseHeader struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	SID     string `json:"sid"`
}

// XunfeiResponsePayload 讯飞响应负载
type XunfeiResponsePayload struct {
	CreateGroupRes    *TextResponse `json:"createGroupRes,omitempty"`
	CreateFeatureRes  *TextResponse `json:"createFeatureRes,omitempty"`
	UpdateFeatureRes  *TextResponse `json:"updateFeatureRes,omitempty"`
	QueryFeatureListRes *TextResponse `json:"queryFeatureListRes,omitempty"`
	SearchScoreFeaRes *TextResponse `json:"searchScoreFeaRes,omitempty"`
	SearchFeaRes      *TextResponse `json:"searchFeaRes,omitempty"`
	DeleteFeatureRes  *TextResponse `json:"deleteFeatureRes,omitempty"`
	DeleteGroupRes    *TextResponse `json:"deleteGroupRes,omitempty"`
}

// TextResponse 文本响应
type TextResponse struct {
	Status string `json:"status,omitempty"`
	Text   string `json:"text"`
}

// CreateGroupResult 创建特征库结果
type CreateGroupResult struct {
	GroupID   string `json:"groupId"`
	GroupName string `json:"groupName"`
	GroupInfo string `json:"groupInfo"`
}

// CreateFeatureResult 创建特征结果
type CreateFeatureResult struct {
	FeatureID string `json:"featureId"`
}

// UpdateFeatureResult 更新特征结果
type UpdateFeatureResult struct {
	Message string `json:"msg"`
}

// QueryFeatureListResult 查询特征列表结果
type QueryFeatureListResult struct {
	Features []FeatureItem `json:"features"`
}

// FeatureItem 特征项
type FeatureItem struct {
	FeatureID   string `json:"featureId"`
	FeatureInfo string `json:"featureInfo"`
}

// SearchScoreFeaResult 1:1比对结果
type SearchScoreFeaResult struct {
	Score       float64 `json:"score"`
	FeatureID   string  `json:"featureId"`
	FeatureInfo string  `json:"featureInfo"`
}

// SearchFeaResult 1:N比对结果
type SearchFeaResult struct {
	ScoreList []SearchScoreFeaResult `json:"scoreList"`
}

// DeleteFeatureResult 删除特征结果
type DeleteFeatureResult struct {
	Message string `json:"msg"`
}

// DeleteGroupResult 删除特征库结果
type DeleteGroupResult struct {
	Message string `json:"msg"`
}

// UnmarshalJSON 自定义JSON解析，处理数组响应
func (q *QueryFeatureListResult) UnmarshalJSON(data []byte) error {
	var features []FeatureItem
	if err := json.Unmarshal(data, &features); err != nil {
		return err
	}
	q.Features = features
	return nil
}

// UnmarshalJSON 自定义JSON解析，处理数组响应
func (s *SearchFeaResult) UnmarshalJSON(data []byte) error {
	var scoreList []SearchScoreFeaResult
	if err := json.Unmarshal(data, &scoreList); err != nil {
		return err
	}
	s.ScoreList = scoreList
	return nil
}
