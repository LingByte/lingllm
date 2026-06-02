# Multi-Layer Memory System Demo

This demo showcases the complete L1+L2 memory system with real LLM integration.

## Features Demonstrated

### L1: Working Memory (工作记忆)
- Current round conversation context
- ReAct chain (Thought → Action → Observation)
- Temporary variables
- Automatic context optimization

### L2: Short-Term Memory (短期会话记忆)
- Recent N rounds summary storage
- Automatic eviction when capacity exceeded
- Disk persistence (JSON format)
- Context generation for LLM prompts

### Integration Pattern
1. **Round Start**: Create L1 working memory
2. **Conversation**: Multi-turn conversation in L1
3. **Summarization**: Generate summary from L1 and add to L2
4. **L1 Clear**: Clear L1 for next round
5. **Context Reuse**: Use L2 context in next round
6. **Persistence**: L2 automatically saves to disk

## Running the Demo

### Prerequisites
- Go 1.25+
- OpenAI API key (or compatible provider)

### Basic Usage

```bash
go run examples/memory-layers-demo/main.go \
  -apikey="your-api-key" \
  -model="gpt-4" \
  -base_url="https://api.openai.com/v1" \
  -data_dir="./memory_data"
```

### With Qwen (通义千问)

```bash
go run examples/memory-layers-demo/main.go \
  -apikey="your-api-key" \
  -model="qwen-turbo" \
  -base_url="https://api.qnaigc.com/v1" \
  -data_dir="./memory_data"
```

### With Ollama (Local)

```bash
go run examples/memory-layers-demo/main.go \
  -apikey="dummy" \
  -model="llama2" \
  -base_url="http://localhost:11434/v1" \
  -data_dir="./memory_data"
```

## Demo Flow

### Scenario
User has a 4-round conversation about Go programming:
1. **Round 1**: What are goroutines?
2. **Round 2**: How do channels work?
3. **Round 3**: Buffered vs unbuffered channels?
4. **Round 4**: Error handling in concurrent code?

### What Happens

Each round:
1. L1 is created fresh for the round
2. L2 context from previous rounds is shown
3. Multi-turn conversation happens in L1
4. L1 state is displayed (messages, thoughts, etc.)
5. L1 is summarized and added to L2
6. L2 state is displayed (stored rounds)
7. L1 is cleared for next round

### Output Example

```
Round 1: round-1
👤 User: What are goroutines in Go?
🤖 Assistant: Goroutines are lightweight threads...

📊 L1 State (before summarization):
  Round ID: round-1
  Messages: 7
  Thoughts: 0
  Actions: 0
  Observations: 0
  Duration: 2.5s

💾 Summarizing round to L2...
✓ Round round-1 summarized and added to L2

📚 L2 State (after summarization):
  Stored Rounds: 1/3
  TTL: 24h0m0s
  Stored Rounds:
    1. round-1 (at 12:34:56)
       Summary: User asked about goroutines. Assistant explained...
       Key Points: [goroutines are lightweight threads]
```

## Persistence

### L2 Storage Format

L2 summaries are stored in JSON format at `memory_data/l2_demo-user.json`:

```json
{
  "round_index": ["round-1", "round-2", "round-3"],
  "summaries": {
    "round-1": {
      "round_id": "round-1",
      "summary": "User asked about goroutines...",
      "key_points": ["goroutines are lightweight threads"],
      "messages": 7,
      "thoughts": 0,
      "actions": 0,
      "observations": 0,
      "timestamp": "2026-06-03T00:34:56Z",
      "expires_at": "2026-06-04T00:34:56Z"
    }
  }
}
```

### Persistence Features
- Automatic save on each round summarization
- Automatic load on startup
- Subject ID sanitization (safe filenames)
- JSON format for human readability

## Memory Architecture

```
Round 1:
  L1 (Working Memory)
    ├─ Messages: 7
    ├─ Thoughts: 0
    └─ Duration: 2.5s
         ↓ (summarize)
  L2 (Short-Term Memory)
    └─ round-1 summary

Round 2:
  L2 Context (from round-1)
    ↓ (used as system context)
  L1 (Working Memory)
    ├─ Messages: 8
    ├─ Thoughts: 0
    └─ Duration: 3.1s
         ↓ (summarize)
  L2 (Short-Term Memory)
    ├─ round-1 summary
    └─ round-2 summary

Round 3:
  L2 Context (from round-1, round-2)
    ↓ (used as system context)
  L1 (Working Memory)
    ├─ Messages: 9
    ├─ Thoughts: 0
    └─ Duration: 2.8s
         ↓ (summarize)
  L2 (Short-Term Memory)
    ├─ round-1 summary
    ├─ round-2 summary
    └─ round-3 summary

Round 4:
  L2 Context (from round-2, round-3) [round-1 evicted]
    ↓ (used as system context)
  L1 (Working Memory)
    ├─ Messages: 10
    ├─ Thoughts: 0
    └─ Duration: 3.2s
         ↓ (summarize)
  L2 (Short-Term Memory)
    ├─ round-2 summary
    ├─ round-3 summary
    └─ round-4 summary
```

## API Usage Examples

### Create L2 with Persistence

```go
stm := memory.NewShortTermMemory(3, 24*time.Hour)
if err := stm.BindPersistence("./memory_data", "user-123"); err != nil {
    log.Fatal(err)
}
```

### Generate Summary from L1

```go
summary := stm.GenerateSummaryFromWorkingMemory(wm)
evicted, err := stm.AddRoundSummary(summary)
if evicted != nil {
    // Handle evicted round (e.g., consolidate to L4)
}
```

### Use L2 Context in Next Round

```go
l2Context := stm.BuildContextString(2)
wm.AddMessage(protocol.RoleSystem, l2Context)
```

### Convert L2 to Messages

```go
messages := stm.ToMessages(3)
// Use in LLM request
```

## Next Steps

- [ ] Implement L3: User Portrait (用户画像)
- [ ] Implement L4: Long-Term Memory (长期记忆)
- [ ] Add memory consolidation from L2 to L4
- [ ] Add memory search capabilities
- [ ] Add memory analytics
