# LingLLM 响应缓存指南

## 缓存是什么？

**响应缓存** 是一个性能优化层，它将 LLM 的响应存储在内存中。当你发送相同的请求时，缓存会直接返回之前保存的响应，而不是再次调用 LLM API。

### 核心优势

1. **降低成本** - 避免重复的 API 调用
2. **提升速度** - 缓存命中时延迟从 1000ms+ 降低到 <1ms
3. **减少 API 限流** - 降低对 API 配额的消耗

---

## 工作原理

### 1. 请求哈希

缓存基于**请求内容**生成唯一的 MD5 哈希值作为缓存键：

```go
// 参与哈希的请求字段:
- Model (模型名)
- Messages (消息历史)
- Tools (工具定义)
- Temperature (温度)
- TopP (采样参数)
- MaxTokens (最大令牌数)

// 不参与哈希的字段:
- Metadata (元数据)
- ToolChoice (工具选择)
- Stop (停止词)
```

**关键点**: 只要这些字段相同，就会命中缓存。

### 2. 缓存存储

```
┌─────────────────────────────────────┐
│  MemoryCache (内存中)                │
├─────────────────────────────────────┤
│ Key (MD5 Hash)  │  Value (Response) │
├─────────────────────────────────────┤
│ a1b2c3d4e5f6... │  ChatResponse {...}│
│ f6e5d4c3b2a1... │  ChatResponse {...}│
│ ...             │  ...              │
└─────────────────────────────────────┘
```

### 3. 缓存生命周期

```
请求到达
    ↓
计算请求哈希
    ↓
查找缓存
    ├─ 缓存命中 → 返回缓存响应 (快速!)
    │
    └─ 缓存未命中 ↓
      调用 LLM API
        ↓
      获得响应
        ↓
      存储到缓存
        ↓
      返回响应
```

---

## 使用方法

### 基础用法

```go
package main

import (
    "context"
    "time"
    
    "github.com/LingByte/lingllm/protocol"
    _ "github.com/LingByte/lingllm/protocol/openai"
)

func main() {
    // 1. 创建原始模型
    model, _ := protocol.NewClient("openai", protocol.ClientConfig{
        APIKey: "your-api-key",
    })
    
    // 2. 创建缓存 (10MB, 1小时TTL)
    cache := protocol.NewMemoryCache(10*1024*1024, 1*time.Hour)
    
    // 3. 包装模型
    cachedModel := protocol.NewCachedModel(model, cache)
    
    // 4. 使用缓存模型
    req := protocol.ChatRequest{
        Model: "gpt-4",
        Messages: []protocol.Message{
            {Role: protocol.RoleUser, Content: "Hello"},
        },
    }
    
    // 第一次调用 - 缓存未命中，调用 API
    resp1, _ := cachedModel.Chat(context.Background(), req)
    // 耗时: ~1000ms (API 延迟)
    
    // 第二次调用 - 缓存命中，直接返回
    resp2, _ := cachedModel.Chat(context.Background(), req)
    // 耗时: <1ms (缓存查询)
}
```

### 监控缓存

```go
// 获取缓存统计
stats := cache.Stats()

fmt.Printf("总请求数: %d\n", stats.TotalRequests)
fmt.Printf("缓存命中: %d\n", stats.CacheHits)
fmt.Printf("缓存未命中: %d\n", stats.CacheMisses)
fmt.Printf("命中率: %.2f%%\n", stats.HitRate()*100)
fmt.Printf("缓存条目: %d\n", stats.EntryCount)
fmt.Printf("缓存大小: %d bytes\n", stats.TotalSize)
fmt.Printf("驱逐次数: %d\n", stats.EvictionCount)
```

### 缓存管理

```go
// 删除特定请求的缓存
cache.Delete(context.Background(), &req)

// 清空所有缓存
cache.Clear(context.Background())

// 获取详细指标
cacheMetrics := &metrics.CacheMetrics{
    TotalRequests:  stats.TotalRequests,
    CacheHits:      stats.CacheHits,
    CacheMisses:    stats.CacheMisses,
    HitRate:        stats.HitRate(),
    TotalSize:      stats.TotalSize,
    EntryCount:     stats.EntryCount,
    EvictionCount:  stats.EvictionCount,
    MaxSize:        10 * 1024 * 1024,
    CurrentSize:    stats.TotalSize,
}
cacheMetrics.CalculateUtilizationRate()
cacheMetrics.CalculateAvgEntrySize()

fmt.Printf("缓存利用率: %.2f%%\n", cacheMetrics.UtilizationRate*100)
fmt.Printf("平均条目大小: %d bytes\n", cacheMetrics.AvgEntrySize)
```

---

## 缓存策略

### 1. LRU 驱逐 (Least Recently Used)

当缓存满时，会删除**最少使用**的条目：

```
缓存满 (10MB)
    ↓
新请求需要存储
    ↓
找到最少使用的条目 (HitCount 最低)
    ↓
删除该条目
    ↓
存储新请求
```

### 2. TTL 过期 (Time To Live)

缓存条目有生命周期，默认 1 小时：

```
条目创建时间: 14:00
TTL: 1 小时
过期时间: 15:00

14:30 访问 → 命中 ✓
15:30 访问 → 过期，删除，重新调用 API
```

### 3. 后台清理

后台线程每分钟清理一次过期条目：

```go
// 内部自动运行
go mc.cleanupExpired() // 每 1 分钟运行一次
```

---

## 实际示例

### 场景 1: 重复查询

```go
// 用户多次问同一个问题
req := protocol.ChatRequest{
    Model: "gpt-4",
    Messages: []protocol.Message{
        {Role: protocol.RoleUser, Content: "What is AI?"},
    },
}

// 第一次
resp1, _ := cachedModel.Chat(ctx, req)  // 1000ms (API)

// 第二次
resp2, _ := cachedModel.Chat(ctx, req)  // <1ms (缓存)

// 第三次
resp3, _ := cachedModel.Chat(ctx, req)  // <1ms (缓存)

// 缓存统计
stats := cache.Stats()
// TotalRequests: 3
// CacheHits: 2
// CacheMisses: 1
// HitRate: 66.67%
```

### 场景 2: 不同请求

```go
// 不同的消息 → 不同的哈希 → 不同的缓存条目
req1 := protocol.ChatRequest{
    Model: "gpt-4",
    Messages: []protocol.Message{
        {Role: protocol.RoleUser, Content: "What is AI?"},
    },
}

req2 := protocol.ChatRequest{
    Model: "gpt-4",
    Messages: []protocol.Message{
        {Role: protocol.RoleUser, Content: "What is ML?"},  // 不同!
    },
}

resp1, _ := cachedModel.Chat(ctx, req1)  // 缓存未命中
resp2, _ := cachedModel.Chat(ctx, req2)  // 缓存未命中 (不同请求)
resp3, _ := cachedModel.Chat(ctx, req1)  // 缓存命中

stats := cache.Stats()
// EntryCount: 2 (两个不同的请求)
// CacheHits: 1
// CacheMisses: 2
```

### 场景 3: 参数变化

```go
// 相同消息，不同温度 → 不同哈希 → 不同缓存
req1 := protocol.ChatRequest{
    Model: "gpt-4",
    Messages: []protocol.Message{
        {Role: protocol.RoleUser, Content: "Tell a story"},
    },
    Temperature: 0.7,  // 创意模式
}

req2 := protocol.ChatRequest{
    Model: "gpt-4",
    Messages: []protocol.Message{
        {Role: protocol.RoleUser, Content: "Tell a story"},
    },
    Temperature: 0.1,  // 确定性模式
}

resp1, _ := cachedModel.Chat(ctx, req1)  // 缓存未命中
resp2, _ := cachedModel.Chat(ctx, req2)  // 缓存未命中 (不同参数)

// 两个不同的缓存条目
stats := cache.Stats()
// EntryCount: 2
```

---

## 性能对比

### 缓存命中 vs 缓存未命中

```
缓存未命中 (首次调用):
请求 → API 调用 → 网络延迟 → 响应 → 存储缓存
耗时: 800ms - 2000ms

缓存命中 (后续调用):
请求 → 哈希计算 → 内存查询 → 返回
耗时: <1ms

性能提升: 800-2000 倍!
```

### 实际测试结果

```
第一次请求 (缓存未命中):
latency: 1.46810133s

第二次请求 (缓存命中):
latency: <1ms (估计)

节省时间: ~1468ms
```

---

## 何时使用缓存

### ✅ 适合缓存

- 重复的用户查询
- 常见问题 (FAQ)
- 系统提示词
- 知识库查询
- 测试和开发

### ❌ 不适合缓存

- 实时数据查询
- 个性化内容
- 需要最新信息的请求
- 流式响应 (自动跳过)

---

## 配置建议

### 小型应用 (个人/测试)

```go
// 100MB, 1小时TTL
cache := protocol.NewMemoryCache(100*1024*1024, 1*time.Hour)
```

### 中型应用 (团队/生产)

```go
// 1GB, 24小时TTL
cache := protocol.NewMemoryCache(1*1024*1024*1024, 24*time.Hour)
```

### 大型应用 (企业)

```go
// 考虑使用 Redis 等外部缓存
// 当前版本仅支持内存缓存
// 未来版本将支持 Redis、Memcached 等
```

---

## 常见问题

### Q: 为什么我的请求没有命中缓存？

**A:** 检查以下几点：
1. 是否使用了 `NewCachedModel` 包装模型？
2. 请求的 Model、Messages、Temperature 等是否完全相同？
3. 缓存是否已过期 (默认 1 小时)？
4. 缓存是否被清空了？

### Q: 缓存会占用多少内存？

**A:** 取决于响应大小和缓存条目数：
- 平均响应: 1-5KB
- 10,000 条目: 10-50MB
- 配置最大大小时要考虑可用内存

### Q: 如何禁用缓存？

**A:** 不使用 `NewCachedModel` 即可：
```go
// 直接使用原始模型，不包装
resp, _ := model.Chat(ctx, req)
```

### Q: 缓存是否线程安全？

**A:** 是的，使用 `sync.RWMutex` 保护所有操作。

### Q: 流式响应会被缓存吗？

**A:** 不会。流式响应自动跳过缓存，总是调用 API。

---

## 未来改进

- [ ] Redis 缓存支持
- [ ] 分布式缓存
- [ ] 缓存预热
- [ ] 缓存统计导出
- [ ] 自定义驱逐策略
- [ ] 缓存持久化

---

## 相关代码

- 缓存实现: `@/Users/chenting/Desktop/lingllm/protocol/cache.go`
- 缓存测试: `@/Users/chenting/Desktop/lingllm/protocol/cache_test.go`
- 缓存指标: `@/Users/chenting/Desktop/lingllm/metrics/metrics.go`
- 示例代码: `@/Users/chenting/Desktop/lingllm/examples/cache-demo/main.go`
