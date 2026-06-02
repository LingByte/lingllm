# LingLLM 架构设计文档

## 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                      应用层 (Application)                    │
│  (用户代码、示例、Web UI、CLI 工具)                          │
└─────────────────────────────────────────────────────────────┘
                            ▲
                            │
┌─────────────────────────────────────────────────────────────┐
│                      业务逻辑层 (Business Logic)             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   Chain      │  │   Tools      │  │   Prompt     │      │
│  │  (处理流)    │  │  (工具调用)  │  │  (提示词)    │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
                            ▲
                            │
┌─────────────────────────────────────────────────────────────┐
│                      协议层 (Protocol)                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   Types      │  │   Stream     │  │   Response   │      │
│  │  (数据结构)  │  │  (流式处理)  │  │  (响应处理)  │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
                            ▲
                            │
┌─────────────────────────────────────────────────────────────┐
│                      提供商层 (Providers)                    │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   OpenAI     │  │  Anthropic   │  │   Ollama     │      │
│  │  (实现)      │  │  (实现)      │  │  (实现)      │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
                            ▲
                            │
┌─────────────────────────────────────────────────────────────┐
│                      外部服务 (External)                     │
│  (LLM API、数据库、缓存、监控)                               │
└─────────────────────────────────────────────────────────────┘
```

---

## 核心模块设计

### 1. Protocol 模块

**职责**: 定义统一的数据结构和接口

```
protocol/
├── types.go          # ChatRequest, ChatResponse, Message, Tool
├── stream.go         # 流式处理接口和实现
├── response/         # 响应处理
├── sse/              # Server-Sent Events
├── openai/           # OpenAI 提供商
├── anthropic/        # Anthropic 提供商
└── ollama/           # Ollama 提供商
```

**关键接口**:
```go
type ChatModel interface {
    Name() string
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    StreamChat(ctx context.Context, req ChatRequest) (ChatStream, error)
}

type ChatStream interface {
    Recv() (*ChatStreamChunk, error)
    Close() error
}
```

### 2. Chain 模块

**职责**: 实现链式处理管道

```
chain/
├── chain.go          # 核心链实现
├── chain_test.go     # 测试
└── README.md         # 文档
```

**关键接口**:
```go
type Node interface {
    Name() string
    Invoke(ctx context.Context, input ChatRequest) (*ChatResponse, error)
    ProcessResult(ctx context.Context, input ChatRequest, result *ChatResponse) (*ChatResponse, error)
    Stream(ctx context.Context, input ChatRequest) (ChatStream, error)
    Transform(ctx context.Context, reader ChatStream) (ChatStream, error)
}

type NodeChain struct {
    name  string
    nodes []Node
}
```

**节点类型**:
- `ModelNode`: 调用 LLM 模型
- `ProcessorNode`: 处理响应
- `StreamProcessorNode`: 处理流

### 3. Tools 模块

**职责**: 管理工具执行和工具链

```
tools/
├── tools.go          # 核心实现
├── tools_test.go     # 测试
└── README.md         # 文档
```

**关键接口**:
```go
type ToolExecutor interface {
    Execute(ctx context.Context, toolName string, args json.RawMessage) (string, error)
    GetTools() []protocol.Tool
}

type ToolChain struct {
    executor  ToolExecutor
    model     protocol.ChatModel
    maxRounds int
}
```

**特性**:
- 支持 OpenAI 原生工具调用
- 支持 ReAct 风格解析
- 多轮工具调用
- 可配置最大轮数

### 4. Prompt 模块

**职责**: 提示词工程和模板管理

```
prompt/
├── template.go       # 模板系统
├── fewshot.go        # 少样本学习
├── reasoning.go      # 推理链
├── message.go        # 消息构建
├── templateset.go    # 模板集合
└── *_test.go         # 测试
```

**关键接口**:
```go
type Template struct {
    Name   string
    Blocks []Block
}

type Block interface {
    Render(data map[string]interface{}) (string, error)
}
```

### 5. Metrics 模块

**职责**: 性能指标收集

```
metrics/
├── metrics.go        # 指标定义
└── metrics_test.go   # 测试
```

**关键结构**:
```go
type CallMetrics struct {
    Provider  string
    Model     string
    StartTime time.Time
    FirstTokenTime time.Time
    EndTime   time.Time
    TokenUsage TokenUsage
    Error     error
}
```

### 6. Shared 模块

**职责**: 共享工具和常量

```
shared/
├── models/           # 模型常量库
│   ├── openai.go
│   ├── anthropic.go
│   ├── google.go
│   ├── constants.go   # 分类常量
│   └── *_test.go
└── utils/            # 工具函数
```

---

## 数据流

### 1. 基础聊天流程

```
User Input
    ↓
ChatRequest (protocol)
    ↓
ChatModel.Chat()
    ↓
Provider (OpenAI/Anthropic/Ollama)
    ↓
ChatResponse (protocol)
    ↓
User Output
```

### 2. 工具调用流程

```
ChatRequest with Tools
    ↓
Model Response (with ToolCalls)
    ↓
Extract ToolCalls
    ↓
Validate Parameters (新增)
    ↓
Execute Tools (Sequential/Parallel)
    ↓
Collect Results
    ↓
Append to Messages
    ↓
Next Round (if needed)
    ↓
Final Response
```

### 3. 链式处理流程

```
Initial Request
    ↓
Node 1 (ModelNode)
    ↓
Response 1
    ↓
Node 2 (ProcessorNode)
    ↓
Response 2
    ↓
Node 3 (ModelNode)
    ↓
Final Response
```

### 4. 流式处理流程

```
StreamChat Request
    ↓
ChatStream (chunks)
    ↓
StreamProcessorNode (transform)
    ↓
Processed Chunks
    ↓
Collect to Response
    ↓
Final Response
```

---

## 扩展点

### 1. 添加新的 LLM 提供商

```go
// protocol/newprovider/newprovider.go

type NewProviderClient struct {
    config *Config
    client *http.Client
}

func (c *NewProviderClient) Name() string {
    return "newprovider"
}

func (c *NewProviderClient) Chat(ctx context.Context, req protocol.ChatRequest) (*protocol.ChatResponse, error) {
    // 实现 Chat 方法
}

func (c *NewProviderClient) StreamChat(ctx context.Context, req protocol.ChatRequest) (protocol.ChatStream, error) {
    // 实现 StreamChat 方法
}

// 注册提供商
func init() {
    protocol.RegisterFactory("newprovider", NewClient)
}
```

### 2. 添加自定义工具执行器

```go
// 实现 ToolExecutor 接口
type CustomExecutor struct {
    tools map[string]protocol.Tool
    // 自定义字段
}

func (e *CustomExecutor) Execute(ctx context.Context, toolName string, args json.RawMessage) (string, error) {
    // 自定义执行逻辑
}

func (e *CustomExecutor) GetTools() []protocol.Tool {
    // 返回工具列表
}

// 使用
toolChain := tools.NewToolChain(model, customExecutor)
```

### 3. 添加自定义链节点

```go
// 实现 Node 接口
type CustomNode struct {
    name string
    // 自定义字段
}

func (n *CustomNode) Name() string {
    return n.name
}

func (n *CustomNode) Invoke(ctx context.Context, input protocol.ChatRequest) (*protocol.ChatResponse, error) {
    // 自定义逻辑
}

func (n *CustomNode) ProcessResult(ctx context.Context, input protocol.ChatRequest, result *protocol.ChatResponse) (*protocol.ChatResponse, error) {
    // 处理前一个节点的结果
}

func (n *CustomNode) Stream(ctx context.Context, input protocol.ChatRequest) (protocol.ChatStream, error) {
    // 流式处理
}

func (n *CustomNode) Transform(ctx context.Context, reader protocol.ChatStream) (protocol.ChatStream, error) {
    // 转换流
}

// 使用
chain := chain.New("custom_chain",
    chain.NewModelNode("model", model),
    &CustomNode{name: "custom"},
)
```

---

## 性能考虑

### 1. 并发处理

- 使用 goroutines 处理并行工具执行
- 使用 channels 进行线程安全通信
- 使用 sync.WaitGroup 管理并发

### 2. 内存优化

- 流式处理避免一次性加载大数据
- 及时释放不需要的资源
- 使用对象池减少 GC 压力

### 3. 缓存策略

- 缓存 LLM 响应
- 缓存工具执行结果
- 使用 TTL 自动过期

---

## 安全考虑

### 1. 输入验证

- 验证工具参数
- 验证提示词内容
- 验证 API 密钥

### 2. 错误处理

- 捕获所有错误
- 提供有意义的错误消息
- 记录错误日志

### 3. 超时控制

- 设置 API 调用超时
- 设置工具执行超时
- 设置链执行超时

---

## 测试策略

### 1. 单元测试

- 测试每个函数的正常和异常情况
- 目标覆盖率: 80%+

### 2. 集成测试

- 测试模块间的交互
- 测试完整的工作流

### 3. 端到端测试

- 测试完整的应用流程
- 使用真实的 LLM API

### 4. 性能测试

- 测试吞吐量
- 测试延迟
- 测试内存使用

---

## 部署架构

### 单机部署

```
┌─────────────┐
│  LingLLM    │
│  Application│
└─────────────┘
      ↓
┌─────────────┐
│  LingLLM    │
│  Library    │
└─────────────┘
      ↓
┌─────────────────────────────────┐
│  External Services              │
│  - OpenAI API                   │
│  - Anthropic API                │
│  - Local Ollama                 │
└─────────────────────────────────┘
```

### 分布式部署 (未来)

```
┌──────────────────────────────────────┐
│  Load Balancer                       │
└──────────────────────────────────────┘
         ↓         ↓         ↓
    ┌────────┐ ┌────────┐ ┌────────┐
    │Instance│ │Instance│ │Instance│
    │   1    │ │   2    │ │   3    │
    └────────┘ └────────┘ └────────┘
         ↓         ↓         ↓
    ┌──────────────────────────────┐
    │  Shared Cache (Redis)        │
    └──────────────────────────────┘
         ↓
    ┌──────────────────────────────┐
    │  Message Queue (RabbitMQ)    │
    └──────────────────────────────┘
         ↓
    ┌──────────────────────────────┐
    │  Database (PostgreSQL)       │
    └──────────────────────────────┘
```

---

## 版本演进

### v1.0.0 (当前)
- ✅ 核心功能完成
- ✅ 多提供商支持
- ✅ 工具链实现
- ✅ 链式处理

### v1.1.0 (下一版本)
- 🔄 工具参数验证
- 🔄 并行工具执行
- 🔄 链条件分支
- 🔄 工具结果缓存

### v1.2.0
- 🔄 响应缓存层
- 🔄 评估框架
- 🔄 提示词优化

### v2.0.0
- 🔄 分布式支持
- 🔄 插件系统
- 🔄 Web UI

---

## 相关资源

- [开发路线图](./DEVELOPMENT_ROADMAP.md)
- [实现指南](./IMPLEMENTATION_GUIDE.md)
- [API 文档](./README.md)
