/*
 * Copyright 2024 LingByte Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package llm defines the core LLM protocol abstraction and provider implementations.
//
// # Overview
//
// The llm package provides a unified interface for interacting with multiple LLM providers
// (OpenAI, Anthropic, Ollama, and OpenAI-compatible gateways). It abstracts away provider-specific
// details while maintaining full feature parity across all implementations.
//
// # Core Interface
//
// [ChatModel] is the primary interface for LLM interactions:
//
//	type ChatModel interface {
//		Name() string
//		Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
//		StreamChat(ctx context.Context, req ChatRequest) (ChatStream, error)
//	}
//
// All implementations support both synchronous ([Chat]) and streaming ([StreamChat]) methods.
//
// # Message Types
//
// [Message] represents a single message in a conversation:
//
//	type Message struct {
//		Role    MessageRole
//		Content string
//	}
//
// Supported roles: [RoleUser], [RoleAssistant], [RoleSystem].
//
// # Chat Requests and Responses
//
// [ChatRequest] encapsulates a chat completion request:
//
//	type ChatRequest struct {
//		Messages    []Message
//		Model       string
//		MaxTokens   int
//		Temperature float32
//		TopP        float32
//		Stop        []string
//	}
//
// [ChatResponse] contains the model's response:
//
//	type ChatResponse struct {
//		ID        string
//		Model     string
//		CreatedAt time.Time
//		Choices   []Choice
//		Usage     TokenUsage
//		Metrics   CallMetrics
//	}
//
// # Streaming
//
// For streaming responses, use [StreamChat] which returns a [ChatStream]:
//
//	stream, err := client.StreamChat(ctx, req)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer stream.Close()
//
//	for {
//		chunk, err := stream.Recv()
//		if err == io.EOF {
//			break
//		}
//		if err != nil {
//			log.Fatal(err)
//		}
//		fmt.Print(chunk.Delta)
//	}
//
//	metrics := stream.Metrics()
//	log.Printf("Latency: %v, TTFT: %v\n", metrics.Latency(), metrics.FirstTokenLatency())
//
// # Performance Metrics
//
// All responses include comprehensive [CallMetrics]:
//
//	type CallMetrics struct {
//		Provider         string
//		Model            string
//		StartAt          time.Time
//		FirstAt          time.Time
//		EndAt            time.Time
//		Bytes            int
//		Chunks           int
//		RequestBytes     int
//		ResponseBytes    int
//		HTTPStatus       int
//		PromptTokens     int
//		CompletionTokens int
//		TotalTokens      int
//	}
//
// Helper methods:
//   - [CallMetrics.Latency] returns total request latency
//   - [CallMetrics.FirstTokenLatency] returns time to first token (streaming only)
//
// # Factory Pattern
//
// Use the factory pattern to create clients without importing provider-specific packages:
//
//	cfg := llm.ClientConfig{
//		Provider: llm.ProviderOpenAI,
//		APIKey:   "sk-...",
//		Model:    "gpt-4",
//	}
//	client, err := llm.NewChatModel(cfg)
//
// Supported providers:
//   - [ProviderOpenAI]: OpenAI's /chat/completions endpoint
//   - [ProviderAnthropic]: Anthropic's /v1/messages endpoint
//   - [ProviderOllama]: Ollama local HTTP API
//   - [ProviderOpenAIResponse]: OpenAI-compatible gateways (e.g., qiniu.com)
//
// # Provider Implementations
//
// Each provider is in its own subpackage:
//
//   - [github.com/LingByte/LingVoice/pkg/protocol/llm/openai]: OpenAI implementation
//   - [github.com/LingByte/LingVoice/pkg/protocol/llm/anthropic]: Anthropic implementation
//   - [github.com/LingByte/LingVoice/pkg/protocol/llm/ollama]: Ollama implementation
//   - [github.com/LingByte/LingVoice/pkg/protocol/llm/response]: OpenAI Response implementation
//
// Providers auto-register via init() functions when imported.
//
// # Streaming Paradigms
//
// The llm package supports two streaming patterns:
//
//   - Server-streaming: Client sends request, server streams response chunks.
//     Implemented via [StreamChat] returning [ChatStream].
//   - Metrics collection: Both sync and streaming methods collect comprehensive metrics
//     for performance analysis and monitoring.
//
// # Error Handling
//
// All methods return errors for:
//   - Network failures
//   - API errors (invalid credentials, rate limits, etc.)
//   - Parsing errors (malformed responses)
//   - Validation errors (invalid requests)
//
// # Example Usage
//
// Synchronous chat:
//
//	client, err := llm.NewChatModel(llm.ClientConfig{
//		Provider: llm.ProviderOpenAI,
//		APIKey:   os.Getenv("OPENAI_API_KEY"),
//		Model:    "gpt-4",
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	resp, err := client.Chat(context.Background(), llm.ChatRequest{
//		Messages: []llm.Message{
//			{Role: llm.RoleUser, Content: "Hello!"},
//		},
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	fmt.Println(resp.Choices[0].Message.Content)
//	fmt.Printf("Latency: %v\n", resp.Metrics.Latency())
//
// Streaming chat:
//
//	stream, err := client.StreamChat(context.Background(), llm.ChatRequest{
//		Messages: []llm.Message{
//			{Role: llm.RoleUser, Content: "Tell me a story"},
//		},
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer stream.Close()
//
//	for {
//		chunk, err := stream.Recv()
//		if err == io.EOF {
//			break
//		}
//		if err != nil {
//			log.Fatal(err)
//		}
//		fmt.Print(chunk.Delta)
//	}
//
//	metrics := stream.Metrics()
//	fmt.Printf("TTFT: %v, Total latency: %v\n",
//		metrics.FirstTokenLatency(), metrics.Latency())
package protocol
