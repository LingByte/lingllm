# RAGFlow Complete Workflow Demo

这个 demo 展示了使用 RAGFlow 和 OpenAI 的完整 RAG 工作流程：

## 工作流程

1. **数据清洗** - 规范化和清理输入数据
2. **向量化** - 使用 OpenAI 的 embedding 模型将文本转换为向量
3. **数据插入** - 将向量化的文档插入到 RAGFlow 知识库
4. **查询** - 使用语义搜索查询相关文档

## 前置条件

1. **RAGFlow 实例** - 需要一个运行中的 RAGFlow 服务
   - 默认地址：`http://localhost:9380`
   - 需要有效的 API Key

2. **OpenAI API Key** - 用于文本向量化
   - 获取地址：https://platform.openai.com/api-keys
   - 默认使用 `text-embedding-3-small` 模型

## 运行 Demo

```bash
go run examples/ragflow-demo/main.go
```

## 交互步骤

### 1. 配置输入
```
Enter RAGFlow BaseURL: http://3.230.3.163
Enter RAGFlow API Key: ragflow-xxx...
Enter Dataset/Namespace name: default
Enter OpenAI API Key: sk-xxx...
Enter OpenAI Model: text-embedding-3-small
```

### 2. 自动执行
- ✓ 连接到 RAGFlow
- ✓ 加载示例数据（8 个文档）
- ✓ 数据清洗和规范化
- ✓ 向量化文档
- ✓ 插入到 RAGFlow

### 3. 交互查询
```
Query: Go programming
Found 3 results (took 234.56ms):

[1] Score: 0.8234
    Content: Go is an open source programming language...

[2] Score: 0.7123
    Content: Go builds upon 15 years of experience...
```

## 示例查询

尝试以下查询来测试 demo：

- `Go programming` - 查找关于 Go 语言的文档
- `machine learning` - 查找机器学习相关文档
- `cloud computing` - 查找云计算相关文档
- `database optimization` - 查找数据库优化文档
- `DevOps` - 查找 DevOps 相关文档

## 数据集

Demo 包含 8 个示例文档，涵盖以下主题：

1. Go Programming Language
2. Python for Data Science
3. Kubernetes Container Orchestration
4. Machine Learning Fundamentals
5. Cloud Computing Architecture
6. REST API Design Best Practices
7. Database Optimization Techniques
8. DevOps and CI/CD Pipelines

## 性能指标

Demo 会显示以下性能指标：

- **插入时间** - 将所有文档插入 RAGFlow 所需的时间
- **查询时间** - 执行单个查询所需的时间（毫秒）
- **结果数量** - 返回的相关文档数量

## 故障排除

### 连接失败
- 检查 RAGFlow 服务是否正在运行
- 验证 BaseURL 和 API Key 是否正确
- 检查网络连接

### 查询返回空结果
- 确保文档已成功插入（检查插入时间）
- 尝试使用更通用的查询词
- 检查 RAGFlow 数据集是否为空

### OpenAI 错误
- 验证 API Key 是否有效
- 检查 API 配额是否充足
- 确保网络可以访问 OpenAI API

## 扩展

可以通过以下方式扩展此 demo：

1. **使用真实数据** - 替换 `getSampleData()` 中的示例数据
2. **自定义 embedding 模型** - 使用不同的 OpenAI 模型或其他提供商
3. **批量导入** - 从文件或数据库导入大量文档
4. **高级过滤** - 使用 MinScore 或其他过滤条件
5. **性能优化** - 批量插入和并行查询

## 相关文档

- [RAGFlow 文档](https://ragflow.io/)
- [OpenAI Embedding 文档](https://platform.openai.com/docs/guides/embeddings)
- [LingLLM 知识库文档](../../knowledge/README.md)
