# Memory System Demo

This demo showcases the LingLLM memory system with real LLM integration.

## Features Demonstrated

### 1. Working Memory (L1)
- Current round conversation context
- ReAct chain (Thought → Action → Observation)
- Temporary variables
- Context optimization and compression

### 2. Short-Term Memory (L2)
- Recent interactions with decay
- Importance scoring
- Access count tracking
- Type-based filtering

### 3. Real LLM Integration
- Multi-turn conversations with memory context
- Memory-aware prompt generation
- Context-aware responses

## Running the Demo

### Prerequisites
- Go 1.25+
- OpenAI API key (or compatible provider)

### Basic Usage

```bash
go run examples/memory-demo/main.go \
  -apikey="your-api-key" \
  -model="gpt-4" \
  -base_url="https://api.openai.com/v1"
```

### With Qwen (通义千问)

```bash
go run examples/memory-demo/main.go \
  -apikey="your-api-key" \
  -model="qwen-turbo" \
  -base_url="https://api.qnaigc.com/v1"
```

### With Ollama (Local)

```bash
go run examples/memory-demo/main.go \
  -apikey="dummy" \
  -model="llama2" \
  -base_url="http://localhost:11434/v1"
```

## Demo Outputs

### Demo 1: Multi-turn Conversation with Memory
Shows how the system maintains conversation context across multiple turns:
- User introduces themselves
- System remembers user information
- Maintains context for subsequent queries

### Demo 2: Using Memory Context in Prompts
Demonstrates memory-augmented prompts:
- Builds system prompt with full memory context
- Includes ReAct chain in prompt
- Shows how memory improves response quality

### Demo 3: Memory Statistics
Displays memory system metrics:
- Working memory statistics
- Short-term memory statistics
- Interaction breakdown by type
- Importance decay visualization

## Memory Architecture

```
┌─────────────────────────────────────┐
│   L1: Working Memory                │
│  (Current Round Context)            │
│  - Messages                         │
│  - ReAct Chain                      │
│  - Temp Variables                   │
└─────────────────────────────────────┘
           ↓
┌─────────────────────────────────────┐
│   L2: Short-Term Memory             │
│  (Recent Interactions)              │
│  - Interaction History              │
│  - Importance Decay                 │
│  - Type-based Filtering             │
└─────────────────────────────────────┘
           ↓
┌─────────────────────────────────────┐
│   L3: User Portrait (Coming Soon)   │
│  (User Profile & Preferences)       │
└─────────────────────────────────────┘
           ↓
┌─────────────────────────────────────┐
│   L4: Long-Term Memory (Coming Soon)│
│  (Persistent Knowledge Base)        │
└─────────────────────────────────────┘
```

## API Examples

### Working Memory

```go
wm := memory.NewWorkingMemory("round-1")

// Add messages
wm.AddMessage(protocol.RoleUser, "Hello")
wm.AddMessage(protocol.RoleAssistant, "Hi there")

// Add ReAct chain
wm.AddThought("Let me think about this")
wm.AddAction("search", map[string]interface{}{})
wm.AddObservation("Found results")

// Get context
ctx := wm.GetContext()
prompt := wm.ToPrompt()

// Statistics
stats := wm.GetStats()
```

### Short-Term Memory

```go
stm := memory.NewShortTermMemory()

// Add interactions
stm.AddInteraction("msg-1", memory.InteractionTypeMessage, "Hello", 0.8)
stm.AddInteraction("action-1", memory.InteractionTypeAction, "Search", 0.9)

// Get recent interactions (with decay)
recent := stm.GetRecentInteractions(10)

// Filter by type
messages := stm.GetInteractionsByType(memory.InteractionTypeMessage)

// Statistics
stats := stm.GetStats()
```

## Next Steps

- [ ] Implement L3: User Portrait (用户画像)
- [ ] Implement L4: Long-Term Memory (长期记忆)
- [ ] Add persistence layer
- [ ] Implement memory consolidation
- [ ] Add memory search capabilities
