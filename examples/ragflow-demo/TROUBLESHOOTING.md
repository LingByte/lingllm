# RAGFlow Demo - Troubleshooting Guide

## Current Status

The RAGFlow demo has been successfully created with the following features:

✅ **Implemented:**
- Configuration management (hardcoded for quick testing)
- OpenAI embedder integration with custom BaseURL support
- RAGFlow health check and connection verification
- Document preparation and embedding
- Document insertion into RAGFlow
- Interactive query interface
- Dataset verification

⚠️ **Known Issues:**
- Documents are being uploaded to RAGFlow (API returns success), but search queries return no results
- This suggests documents may not be properly indexed by RAGFlow

## Debugging Steps

### 1. Verify Documents in RAGFlow Web UI

1. Open RAGFlow Web UI: `http://3.230.3.163/`
2. Navigate to the "Default" dataset
3. Check if documents are visible in the dataset
4. If documents are not visible, they may not have been uploaded correctly

### 2. Check Upload Response

The demo now prints the first document's details:
```
First document: ID=doc1, Title=Go Programming Language, Content length=XXX
```

If this shows correct values, the documents are being prepared properly.

### 3. Verify API Endpoints

RAGFlow uses the following endpoints:
- **Health Check**: `/v1/system/healthz` (primary), `/health`, `/api/health`, `/api/v1/health` (fallback)
- **List Datasets**: `/api/v1/datasets`
- **Upload Documents**: `/api/v1/datasets/{datasetID}/documents`
- **Search**: `/api/v1/datasets/{datasetID}/search`

### 4. Check RAGFlow Logs

If you have access to RAGFlow server logs, check for:
- Document upload errors
- Indexing failures
- API authentication issues

## Possible Solutions

### Solution 1: Manual Document Upload

If the API upload is not working, try uploading documents manually through RAGFlow Web UI:
1. Open RAGFlow Web UI
2. Go to the "Default" dataset
3. Use the upload interface to add documents
4. Then try querying

### Solution 2: Check API Key Permissions

Ensure the API key has permissions to:
- Create/access datasets
- Upload documents
- Perform searches

### Solution 3: Verify RAGFlow Configuration

Check if RAGFlow requires:
- Specific document format
- Additional metadata fields
- Document chunking configuration

## Configuration

Current hardcoded configuration in `main.go`:
```go
ragflowConfig := RagflowConfig{
    BaseURL:   "http://3.230.3.163",
    APIKey:    "ragflow-pfQrfKWmOQAYXe_my6hRLrV8bTQON57Cg6f_YB9UFV4",
    Namespace: "Default",
}

embdConfig := EmbedderConfig{
    Provider: "openai",
    APIKey:   "32QKNUANTPLLAW0OM5BE8URXDXVC1L8PCU82UIWW",
    Model:    "Qwen3-Embedding-8B",
    BaseURL:  "https://ai.gitee.com/v1",
}
```

To change configuration, edit these values in `main.go` and rebuild.

## Next Steps

1. **Verify document upload** through RAGFlow Web UI
2. **Check RAGFlow documentation** for specific API requirements
3. **Test with manual documents** uploaded through Web UI
4. **Adjust request format** if needed based on RAGFlow API response

## Files Modified

- `/Users/chenting/Desktop/lingllm/knowledge/ragflow.go` - RAGFlow handler implementation
- `/Users/chenting/Desktop/lingllm/examples/ragflow-demo/main.go` - Demo application
- `/Users/chenting/Desktop/lingllm/embedder/openai.go` - OpenAI embedder (no changes)
- `/Users/chenting/Desktop/lingllm/knowledge/types.go` - Type definitions (no changes)

## Support

For more information about RAGFlow, visit: https://github.com/infiniflow/ragflow
