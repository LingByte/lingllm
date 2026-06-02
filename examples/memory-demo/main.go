package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/LingByte/lingllm/chain"
	"github.com/LingByte/lingllm/memory"
	"github.com/LingByte/lingllm/protocol"
	_ "github.com/LingByte/lingllm/protocol/openai"
)

func main() {
	apiKey := flag.String("apikey", "", "API key for the LLM provider")
	model := flag.String("model", "gpt-4", "Model name")
	baseURL := flag.String("base_url", "", "Base URL for the API")
	flag.Parse()

	if *apiKey == "" {
		log.Fatal("apikey is required")
	}

	// Create the LLM client
	client, err := protocol.NewClient(protocol.ClientConfig{
		Provider: "openai",
		APIKey:   *apiKey,
		BaseURL:  *baseURL,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Create a simple chain with one model node
	c := chain.New("memory-demo", chain.NewModelNode("llm", client))

	ctx := context.Background()

	fmt.Println("=== LingLLM Memory System Demo ===\n")

	// Demo 1: Working Memory + Short-term Memory
	fmt.Println("Demo 1: Multi-turn Conversation with Memory")
	fmt.Println("─────────────────────────────────────")
	demoMultiTurnConversation(ctx, c, *model)

	// Demo 2: Memory Context in Prompt
	fmt.Println("\nDemo 2: Using Memory Context in Prompts")
	fmt.Println("─────────────────────────────────────")
	demoMemoryContextPrompt(ctx, c, *model)

	// Demo 3: Memory Statistics
	fmt.Println("\nDemo 3: Memory Statistics and Analysis")
	fmt.Println("─────────────────────────────────────")
	demoMemoryStatistics()
}

// Demo 1: Multi-turn conversation with memory
func demoMultiTurnConversation(ctx context.Context, c *chain.NodeChain, model string) {
	// Create memory systems
	wm := memory.NewWorkingMemory("conversation-1")
	stm := memory.NewShortTermMemory()

	// Simulate a multi-turn conversation
	conversations := []struct {
		role    protocol.MessageRole
		content string
	}{
		{protocol.RoleUser, "My name is Alice and I work as a software engineer"},
		{protocol.RoleAssistant, "Nice to meet you, Alice! I'll remember that you're a software engineer."},
		{protocol.RoleUser, "I'm currently working on a Go project"},
		{protocol.RoleAssistant, "Great! Go is an excellent language for building scalable systems."},
		{protocol.RoleUser, "Can you help me with memory management in Go?"},
	}

	fmt.Println("Conversation History:")
	for i, conv := range conversations {
		wm.AddMessage(conv.role, conv.content)
		stm.AddInteraction(
			fmt.Sprintf("msg-%d", i),
			memory.InteractionTypeMessage,
			conv.content,
			0.8,
		)

		fmt.Printf("[%s]: %s\n", conv.role, conv.content)

		// For the last user message, get LLM response
		if i == len(conversations)-1 && conv.role == protocol.RoleUser {
			fmt.Println("\nGenerating response with memory context...")

			// Build request with memory context
			messages := wm.GetMessages()
			req := protocol.ChatRequest{
				Model:     model,
				Messages:  messages,
				MaxTokens: 200,
			}

			resp, err := c.Invoke(ctx, req)
			if err != nil {
				log.Printf("Error: %v", err)
				return
			}

			response := resp.FirstContent()
			fmt.Printf("[Assistant]: %s\n", response)

			// Add to memory
			wm.AddMessage(protocol.RoleAssistant, response)
			stm.AddInteraction(
				fmt.Sprintf("msg-%d", len(conversations)),
				memory.InteractionTypeMessage,
				response,
				0.8,
			)
		}
	}

	// Show memory stats
	fmt.Println("\n--- Working Memory Stats ---")
	stats := wm.GetStats()
	fmt.Printf("Round ID: %s\n", stats.RoundID)
	fmt.Printf("Messages: %d\n", stats.MessageCount)
	fmt.Printf("Duration: %v\n", stats.Duration)

	fmt.Println("\n--- Short-term Memory Stats ---")
	stmStats := stm.GetStats()
	fmt.Printf("Total Interactions: %d\n", stmStats.TotalInteractions)
	fmt.Printf("By Type: %v\n", stmStats.ByType)
	fmt.Printf("Average Importance: %.2f\n", stmStats.AverageImportance)
}

// Demo 2: Using memory context in prompts
func demoMemoryContextPrompt(ctx context.Context, c *chain.NodeChain, model string) {
	wm := memory.NewWorkingMemory("context-demo")
	stm := memory.NewShortTermMemory()

	// Build conversation context
	wm.AddMessage(protocol.RoleUser, "I'm learning Go and interested in concurrency")
	wm.AddThought("The user is interested in Go concurrency")
	wm.AddAction("search", map[string]interface{}{"query": "Go concurrency patterns"})
	wm.AddObservation("Found goroutines, channels, and sync packages")

	wm.AddMessage(protocol.RoleAssistant, "Go has great concurrency support with goroutines and channels")
	wm.AddMessage(protocol.RoleUser, "How do I handle errors in concurrent code?")

	// Add interactions to short-term memory
	stm.AddInteraction("interaction-1", memory.InteractionTypeMessage, "User interested in Go concurrency", 0.9)
	stm.AddInteraction("interaction-2", memory.InteractionTypeAction, "Searched for concurrency patterns", 0.8)
	stm.AddInteraction("interaction-3", memory.InteractionTypeObservation, "Found goroutines, channels, sync", 0.85)

	// Generate prompt with memory context
	fmt.Println("Memory Context:")
	fmt.Println(wm.ToPrompt())

	// Get recent interactions
	recent := stm.GetRecentInteractions(3)
	fmt.Println("\nRecent Interactions (with decay):")
	for i, interaction := range recent {
		fmt.Printf("%d. [%s] %s (importance: %.2f)\n",
			i+1, interaction.Type, interaction.Content, interaction.Importance)
	}

	// Make request with memory context
	fmt.Println("\nGenerating response with full memory context...")

	systemPrompt := fmt.Sprintf(`You are a helpful Go programming assistant. 
Here is the conversation context:
%s

Based on this context, answer the user's latest question.`, wm.ToPrompt())

	messages := []protocol.Message{
		{Role: protocol.RoleSystem, Content: systemPrompt},
	}
	messages = append(messages, wm.GetMessages()...)

	req := protocol.ChatRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: 300,
	}

	resp, err := c.Invoke(ctx, req)
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Response: %s\n", resp.FirstContent())
}

// Demo 3: Memory statistics and analysis
func demoMemoryStatistics() {
	fmt.Println("Creating memory with sample data...")

	wm := memory.NewWorkingMemory("stats-demo")
	stm := memory.NewShortTermMemory()

	// Add various interactions
	interactions := []struct {
		id         string
		iType      memory.InteractionType
		content    string
		importance float32
	}{
		{"msg-1", memory.InteractionTypeMessage, "Hello, how are you?", 0.7},
		{"msg-2", memory.InteractionTypeMessage, "I'm doing well, thanks for asking", 0.7},
		{"action-1", memory.InteractionTypeAction, "Searched for information", 0.8},
		{"obs-1", memory.InteractionTypeObservation, "Found relevant results", 0.85},
		{"decision-1", memory.InteractionTypeDecision, "Decided to use Go for the project", 0.9},
		{"msg-3", memory.InteractionTypeMessage, "Let's start coding", 0.75},
		{"error-1", memory.InteractionTypeError, "Compilation error in line 42", 0.6},
	}

	for _, interaction := range interactions {
		stm.AddInteraction(interaction.id, interaction.iType, interaction.content, interaction.importance)
		wm.AddMessage(protocol.RoleUser, interaction.content)
	}

	// Show statistics
	fmt.Println("\n--- Working Memory Statistics ---")
	wmStats := wm.GetStats()
	fmt.Printf("Round ID: %s\n", wmStats.RoundID)
	fmt.Printf("Total Messages: %d\n", wmStats.MessageCount)
	fmt.Printf("Thoughts: %d\n", wmStats.ThoughtCount)
	fmt.Printf("Actions: %d\n", wmStats.ActionCount)
	fmt.Printf("Observations: %d\n", wmStats.ObservationCount)
	fmt.Printf("Duration: %v\n", wmStats.Duration)

	fmt.Println("\n--- Short-term Memory Statistics ---")
	stmStats := stm.GetStats()
	fmt.Printf("Total Interactions: %d\n", stmStats.TotalInteractions)
	fmt.Println("Breakdown by Type:")
	for iType, count := range stmStats.ByType {
		fmt.Printf("  %s: %d\n", iType, count)
	}
	fmt.Printf("Average Importance: %.2f\n", stmStats.AverageImportance)

	fmt.Println("\n--- Recent Interactions (with decay) ---")
	recent := stm.GetRecentInteractions(5)
	for i, interaction := range recent {
		fmt.Printf("%d. [%s] %s\n   Importance: %.2f, Access Count: %d\n",
			i+1, interaction.Type, interaction.Content, interaction.Importance, interaction.AccessCount)
	}

	fmt.Println("\n--- Interactions by Type ---")
	messages := stm.GetInteractionsByType(memory.InteractionTypeMessage)
	fmt.Printf("Messages: %d\n", len(messages))
	for _, msg := range messages {
		fmt.Printf("  - %s\n", msg.Content)
	}

	actions := stm.GetInteractionsByType(memory.InteractionTypeAction)
	fmt.Printf("Actions: %d\n", len(actions))
	for _, action := range actions {
		fmt.Printf("  - %s\n", action.Content)
	}
}
