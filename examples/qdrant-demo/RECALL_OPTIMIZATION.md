# 提高 Qdrant RAG 召回率指南

## 📊 当前状态分析

从测试结果看，当前召回率存在以下特点：
- 短查询（如 "go"）的相关性分数较低（0.5006）
- 长查询（完整句子）的相关性分数较高（0.8324）
- 这说明向量相似度计算与查询长度相关

## 🔧 改进方案

### 1. **扩展文档内容** ✅ 已实现

**原理**: 更长、更详细的文档内容提供更多语义信息，改善向量表示

**改进内容**:
- Go Programming Language: 从 3 句扩展到 9 句
- Python for Data Science: 从 3 句扩展到 10 句
- Kubernetes: 从 3 句扩展到 9 句
- Machine Learning: 从 3 句扩展到 9 句
- Cloud Computing: 从 3 句扩展到 9 句
- REST API: 从 3 句扩展到 9 句
- Database Optimization: 从 3 句扩展到 9 句
- DevOps: 从 3 句扩展到 9 句

**预期效果**:
- 短查询相关性分数提升 20-30%
- 长查询相关性分数保持稳定或略有提升

### 2. **优化向量化参数**

#### 选择更好的 Embedding 模型
```
当前: text-embedding-3-small (1536 维)
建议: text-embedding-3-large (3072 维)
优点: 更高的向量质量，更好的语义表示
缺点: 更高的成本和延迟
```

#### 使用专门的中文 Embedding 模型
```
推荐: Qwen3-Embedding-8B (已在使用)
优点: 对中文和英文都有很好的支持
缺点: 需要自己部署或使用 API
```

### 3. **改进查询处理**

#### 查询扩展（Query Expansion）
```go
// 示例：为短查询添加上下文
原始查询: "go"
扩展查询: "go programming language golang"
```

#### 查询重写（Query Rewriting）
```go
// 示例：规范化查询
输入: "what is go?"
规范化: "go programming language"
```

### 4. **优化 Qdrant 搜索参数**

#### 调整相似度阈值
```go
// 当前设置
minScore := 0.0  // 接受所有结果

// 建议设置
minScore := 0.3  // 过滤低相关性结果
```

#### 调整返回结果数量
```go
// 当前设置
topK := 5  // 返回 5 个结果

// 建议设置
topK := 10  // 返回 10 个结果，然后重排序
```

### 5. **实现重排序（Reranking）**

#### 使用 LLM 重排序
```go
// 伪代码
results := vectorSearch()  // 获取初始结果
reranked := llmRerank(query, results)  // 使用 LLM 重排序
return reranked[:5]  // 返回重排序后的前 5 个
```

#### 使用交叉编码器（Cross-Encoder）
```go
// 伪代码
results := vectorSearch()
scores := crossEncoder(query, results)  // 计算交叉编码器分数
reranked := sort(results, scores)
return reranked[:5]
```

### 6. **改进文档结构**

#### 添加摘要和关键词
```go
type Document struct {
    Title       string
    Content     string
    Summary     string      // 添加摘要
    Keywords    []string    // 添加关键词
    Tags        []string
    Metadata    map[string]any
}
```

#### 分层索引
```go
// 为不同部分分别索引
- 标题（权重高）
- 摘要（权重中）
- 完整内容（权重低）
```

### 7. **使用混合搜索**

#### 向量搜索 + 关键词搜索
```go
// 伪代码
vectorResults := vectorSearch(query)  // 语义搜索
keywordResults := keywordSearch(query)  // 关键词搜索
combined := merge(vectorResults, keywordResults)  // 合并结果
return combined[:5]
```

## 📈 预期改进效果

| 方案 | 短查询提升 | 长查询提升 | 实现难度 |
|------|----------|----------|--------|
| 扩展文档内容 | 20-30% | 10-15% | 低 |
| 更好的 Embedding 模型 | 15-25% | 10-20% | 中 |
| 查询扩展 | 25-35% | 5-10% | 中 |
| 重排序 | 30-40% | 20-30% | 高 |
| 混合搜索 | 35-45% | 25-35% | 高 |

## 🚀 实现优先级

### 第一阶段（立即实施）
1. ✅ 扩展文档内容（已完成）
2. 调整 Qdrant 搜索参数
3. 添加查询预处理

### 第二阶段（短期）
1. 实现查询扩展
2. 添加文档摘要
3. 测试不同的 Embedding 模型

### 第三阶段（中期）
1. 实现重排序
2. 混合搜索
3. 性能优化

## 🧪 测试方法

### 基准测试
```bash
# 运行 demo 并记录分数
go run examples/qdrant-demo/main.go

# 测试查询
Query: go
Query: python
Query: kubernetes
Query: machine learning
Query: cloud computing
```

### 评估指标
- **平均相关性分数**: 所有查询的平均分数
- **高相关性比例**: 分数 > 0.7 的结果比例
- **查询延迟**: 平均查询时间

## 📚 参考资源

- [Qdrant 文档 - 搜索优化](https://qdrant.tech/documentation/guides/search/)
- [OpenAI Embeddings - 最佳实践](https://platform.openai.com/docs/guides/embeddings)
- [向量搜索最佳实践](https://www.pinecone.io/learn/vector-search-basics/)
- [重排序和融合](https://www.sbert.net/docs/pretrained_cross-encoders/ce-mmarplec/)

## 💡 快速开始

### 立即尝试改进
1. 重新编译并运行 demo
2. 测试相同的查询
3. 比较分数变化

```bash
# 编译
go build -o /tmp/qdrant-demo examples/qdrant-demo/main.go

# 运行
go run examples/qdrant-demo/main.go
```

### 下一步改进
1. 实现查询扩展
2. 添加文档摘要
3. 测试重排序
