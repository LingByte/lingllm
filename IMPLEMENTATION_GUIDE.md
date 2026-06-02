# LingLLM 实现指南

## 快速开始开发

### 环境设置
```bash
# 克隆项目
git clone https://github.com/LingByte/lingllm.git
cd lingllm

# 安装依赖
go mod download

# 运行测试
go test ./...

# 检查代码格式
gofmt -l .
```

---

## 第一个特性: 工具参数验证

### 1. 需求分析

**目标**: 在执行工具前验证参数是否符合工具定义

**当前问题**:
```go
// 现在直接执行，没有验证
result, err := tc.executor.Execute(ctx, call.Function.Name, call.Function.Arguments)
```

**改进方案**:
```go
// 应该先验证参数
validator := NewToolValidator()
if err := validator.Validate(tool, call.Function.Arguments); err != nil {
    return fmt.Errorf("invalid arguments: %w", err)
}
result, err := tc.executor.Execute(ctx, call.Function.Name, call.Function.Arguments)
```

### 2. 设计接口

```go
// tools/validator.go

package tools

import (
    "encoding/json"
    "fmt"
    
    "github.com/LingByte/lingllm/protocol"
)

// ParameterValidator validates tool parameters against tool definitions
type ParameterValidator interface {
    Validate(tool protocol.Tool, args json.RawMessage) error
}

// JSONSchemaValidator validates parameters using JSON Schema
type JSONSchemaValidator struct {
    // 实现细节
}

// NewJSONSchemaValidator creates a new JSON Schema validator
func NewJSONSchemaValidator() *JSONSchemaValidator {
    return &JSONSchemaValidator{}
}

// Validate checks if arguments match the tool's schema
func (v *JSONSchemaValidator) Validate(tool protocol.Tool, args json.RawMessage) error {
    // 1. 解析参数
    var params map[string]interface{}
    if err := json.Unmarshal(args, &params); err != nil {
        return fmt.Errorf("invalid JSON: %w", err)
    }
    
    // 2. 检查必需参数
    // 3. 检查参数类型
    // 4. 检查参数值范围
    
    return nil
}
```

### 3. 集成到 ToolChain

```go
// tools/tools.go

type ToolChain struct {
    executor  ToolExecutor
    model     protocol.ChatModel
    maxRounds int
    validator ParameterValidator  // 新增
}

// NewToolChain creates a new tool chain with validation
func NewToolChain(model protocol.ChatModel, executor ToolExecutor) *ToolChain {
    return &ToolChain{
        executor:  executor,
        model:     model,
        maxRounds: 5,
        validator: NewJSONSchemaValidator(),  // 默认使用 JSON Schema 验证
    }
}

// WithValidator sets a custom parameter validator
func (tc *ToolChain) WithValidator(validator ParameterValidator) *ToolChain {
    tc.validator = validator
    return tc
}

// ExecuteWithTools 中的修改
func (tc *ToolChain) ExecuteWithTools(ctx context.Context, req protocol.ChatRequest) (*protocol.ChatResponse, error) {
    // ... 现有代码 ...
    
    for _, call := range toolCalls {
        // 新增: 验证参数
        tool := tc.findTool(call.Function.Name)
        if tool != nil {
            if err := tc.validator.Validate(*tool, call.Function.Arguments); err != nil {
                result = fmt.Sprintf("Error: Invalid parameters - %v", err)
                // 继续处理，不中断
            } else {
                result, execErr = tc.executor.Execute(ctx, call.Function.Name, call.Function.Arguments)
            }
        }
        
        // ... 现有代码 ...
    }
    
    // ... 现有代码 ...
}

// findTool 辅助方法
func (tc *ToolChain) findTool(name string) *protocol.Tool {
    for _, tool := range tc.executor.GetTools() {
        if tool.Name == name {
            return &tool
        }
    }
    return nil
}
```

### 4. 编写测试

```go
// tools/validator_test.go

package tools

import (
    "encoding/json"
    "testing"
    
    "github.com/LingByte/lingllm/protocol"
)

func TestJSONSchemaValidator(t *testing.T) {
    validator := NewJSONSchemaValidator()
    
    tool := protocol.Tool{
        Name: "get_weather",
        Description: "Get weather for a location",
        InputSchema: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "location": map[string]interface{}{
                    "type": "string",
                },
                "unit": map[string]interface{}{
                    "type": "string",
                    "enum": []string{"celsius", "fahrenheit"},
                },
            },
            "required": []string{"location"},
        },
    }
    
    tests := []struct {
        name    string
        args    json.RawMessage
        wantErr bool
    }{
        {
            name:    "valid arguments",
            args:    json.RawMessage(`{"location": "San Francisco", "unit": "celsius"}`),
            wantErr: false,
        },
        {
            name:    "missing required parameter",
            args:    json.RawMessage(`{"unit": "celsius"}`),
            wantErr: true,
        },
        {
            name:    "invalid enum value",
            args:    json.RawMessage(`{"location": "San Francisco", "unit": "kelvin"}`),
            wantErr: true,
        },
        {
            name:    "invalid JSON",
            args:    json.RawMessage(`{invalid}`),
            wantErr: true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validator.Validate(tool, tt.args)
            if (err != nil) != tt.wantErr {
                t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### 5. 提交 PR

```bash
# 创建分支
git checkout -b feature/tool-parameter-validation

# 提交代码
git add .
git commit -m "feat: add tool parameter validation

- Implement ParameterValidator interface
- Add JSONSchemaValidator implementation
- Integrate validation into ToolChain
- Add comprehensive tests for validation
- Improve error messages for invalid parameters"

# 推送
git push origin feature/tool-parameter-validation
```

---

## 第二个特性: 并行工具执行

### 1. 设计

```go
// tools/parallel.go

package tools

import (
    "context"
    "sync"
    
    "github.com/LingByte/lingllm/protocol"
)

// ParallelToolExecutor executes multiple tools in parallel
type ParallelToolExecutor struct {
    executor ToolExecutor
    maxConcurrency int
}

// ExecuteParallel executes multiple tool calls concurrently
func (pte *ParallelToolExecutor) ExecuteParallel(
    ctx context.Context,
    calls []protocol.ToolCall,
) (map[string]string, error) {
    results := make(map[string]string)
    errors := make(map[string]error)
    
    // 使用 semaphore 限制并发数
    sem := make(chan struct{}, pte.maxConcurrency)
    var wg sync.WaitGroup
    var mu sync.Mutex
    
    for _, call := range calls {
        wg.Add(1)
        go func(c protocol.ToolCall) {
            defer wg.Done()
            
            sem <- struct{}{}        // 获取许可
            defer func() { <-sem }() // 释放许可
            
            result, err := pte.executor.Execute(ctx, c.Function.Name, c.Function.Arguments)
            
            mu.Lock()
            if err != nil {
                errors[c.ID] = err
            } else {
                results[c.ID] = result
            }
            mu.Unlock()
        }(call)
    }
    
    wg.Wait()
    
    if len(errors) > 0 {
        return results, fmt.Errorf("parallel execution errors: %v", errors)
    }
    
    return results, nil
}
```

### 2. 集成到 ToolChain

```go
// tools/tools.go

type ToolChain struct {
    executor           ToolExecutor
    model              protocol.ChatModel
    maxRounds          int
    validator          ParameterValidator
    parallelExecution  bool  // 新增
    maxConcurrency     int   // 新增
}

// WithParallelExecution enables parallel tool execution
func (tc *ToolChain) WithParallelExecution(maxConcurrency int) *ToolChain {
    tc.parallelExecution = true
    tc.maxConcurrency = maxConcurrency
    return tc
}

// ExecuteWithTools 中的修改
func (tc *ToolChain) ExecuteWithTools(ctx context.Context, req protocol.ChatRequest) (*protocol.ChatResponse, error) {
    // ... 现有代码 ...
    
    if tc.parallelExecution {
        // 使用并行执行
        parallelExec := &ParallelToolExecutor{
            executor: tc.executor,
            maxConcurrency: tc.maxConcurrency,
        }
        results, err := parallelExec.ExecuteParallel(ctx, toolCalls)
        // 处理结果
    } else {
        // 使用顺序执行
        // ... 现有代码 ...
    }
    
    // ... 现有代码 ...
}
```

---

## 第三个特性: 链的条件分支

### 1. 设计

```go
// chain/conditional.go

package chain

import (
    "context"
    
    "github.com/LingByte/lingllm/protocol"
)

// ConditionalNode executes different branches based on a condition
type ConditionalNode struct {
    name        string
    condition   func(context.Context, *protocol.ChatResponse) bool
    trueBranch  Node
    falseBranch Node
}

// NewConditionalNode creates a new conditional node
func NewConditionalNode(
    name string,
    condition func(context.Context, *protocol.ChatResponse) bool,
    trueBranch, falseBranch Node,
) *ConditionalNode {
    return &ConditionalNode{
        name:        name,
        condition:   condition,
        trueBranch:  trueBranch,
        falseBranch: falseBranch,
    }
}

// Invoke executes the appropriate branch
func (n *ConditionalNode) Invoke(ctx context.Context, input protocol.ChatRequest) (*protocol.ChatResponse, error) {
    return nil, fmt.Errorf("conditional node %s requires a previous response", n.name)
}

// ProcessResult evaluates the condition and executes the appropriate branch
func (n *ConditionalNode) ProcessResult(
    ctx context.Context,
    input protocol.ChatRequest,
    result *protocol.ChatResponse,
) (*protocol.ChatResponse, error) {
    if n.condition(ctx, result) {
        return n.trueBranch.ProcessResult(ctx, input, result)
    }
    return n.falseBranch.ProcessResult(ctx, input, result)
}

// Name returns the node name
func (n *ConditionalNode) Name() string {
    return n.name
}

// Stream is not supported for conditional nodes
func (n *ConditionalNode) Stream(ctx context.Context, input protocol.ChatRequest) (protocol.ChatStream, error) {
    return nil, fmt.Errorf("conditional node does not support streaming")
}
```

### 2. 使用示例

```go
// 创建条件分支
conditional := NewConditionalNode(
    "check_sentiment",
    func(ctx context.Context, resp *protocol.ChatResponse) bool {
        // 检查响应是否包含积极情绪
        content := resp.FirstContent()
        return strings.Contains(strings.ToLower(content), "positive")
    },
    // 积极情绪分支
    NewModelNode("positive_handler", positiveModel),
    // 消极情绪分支
    NewModelNode("negative_handler", negativeModel),
)

// 在链中使用
chain := New("sentiment_chain",
    NewModelNode("analyzer", analyzerModel),
    conditional,
)
```

---

## 开发工作流

### 1. 创建特性分支
```bash
git checkout -b feature/your-feature-name
```

### 2. 开发和测试
```bash
# 运行特定包的测试
go test ./tools -v

# 运行所有测试
go test ./...

# 检查覆盖率
go test ./... -cover

# 生成覆盖率报告
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### 3. 代码格式化
```bash
# 格式化代码
gofmt -w .

# 检查格式
gofmt -l .
```

### 4. 提交和推送
```bash
git add .
git commit -m "feat: add your feature

- Detailed description of changes
- List of improvements
- Any breaking changes"

git push origin feature/your-feature-name
```

### 5. 创建 Pull Request
- 在 GitHub 上创建 PR
- 填写 PR 模板
- 等待代码审查
- 根据反馈进行修改

---

## 常见问题

### Q: 如何添加新的 LLM 提供商?
A: 在 `protocol/` 目录下创建新的提供商包，实现 `ChatModel` 接口。

### Q: 如何自定义工具执行?
A: 实现 `ToolExecutor` 接口并传递给 `ToolChain`。

### Q: 如何添加新的链节点类型?
A: 实现 `Node` 接口并在 `chain/` 包中定义。

### Q: 如何贡献文档?
A: 编辑 README.md 或创建新的 .md 文件，提交 PR。

---

## 资源

- [Go 官方文档](https://golang.org/doc/)
- [项目 GitHub](https://github.com/LingByte/lingllm)
- [讨论区](https://github.com/LingByte/lingllm/discussions)
