# IM2Agent 大模型驱动版（v0.7-llm）

让 demo 里的"Agent"从硬编码状态机升级为**真能起草回复**的大模型调用。OpenAI 兼容协议，可接任意后端：OpenAI / DeepSeek / 通义千问 / 本地 vLLM / Ollama。

## 快速开始

```bash
# 1. 配置环境变量（任选一个兼容端点）
export OPENAI_API_KEY=sk-xxx
export OPENAI_BASE_URL=https://api.deepseek.com/v1   # OpenAI 默认 https://api.openai.com/v1
export LLM_MODEL=deepseek-chat                       # 或 gpt-4o-mini / qwen-turbo

# 2. 起 demo
go run ./cmd/demo -addr :8080

# 3. 健康检查
curl http://localhost:8080/api/llm/status
# {"ready":true,"model":"deepseek-chat","baseUrl":"https://api.deepseek.com/v1"}

# 4. 让 Agent 替你起草一段陌生人回复
curl -X POST http://localhost:8080/api/llm/draft \
  -H "Content-Type: application/json" \
  -d '{
    "strangerAgent": "biz-agent",
    "category": "商务合作",
    "content": "我们做企业 IM 一体化方案，希望沟通 20 分钟",
    "tone": "polite"
  }'
# {"draft":"你好，收到关于「商务合作」的合作意向……","via":"llm"}
```

## 三种运行模式

| 环境变量 | 行为 |
| --- | --- |
| 无 `OPENAI_API_KEY` | 模板降级：`/api/llm/status` 返回 `ready:false`，`/api/llm/draft` 返回本地模板 |
| 有 key 但端点不通 | 调用失败自动 fallback 到模板，`via:"template_fallback"`，note 带错误 |
| 有 key 且端点可用 | 真实 LLM 生成，`via:"llm"` |

**优雅降级是设计目标**：demo 在任何环境都能跑通，不会因为没 key 就崩。

## 语气档位

`tone` 字段可选：
- `polite`（默认）：专业但不生硬，要求对方先发材料
- `brief`：极短，"先看摘要再约时间"
- `decline`：礼貌拒绝

## 支持的 LLM 后端

| 后端 | `OPENAI_BASE_URL` | `LLM_MODEL` 示例 |
| --- | --- | --- |
| OpenAI | （留空默认） | `gpt-4o-mini` |
| DeepSeek | `https://api.deepseek.com/v1` | `deepseek-chat` |
| 通义千问（DashScope 兼容模式） | `https://dashscope.aliyuncs.com/compatible-mode/v1` | `qwen-turbo` |
| Ollama 本地 | `http://127.0.0.1:11434/v1` | `llama3.1` |
| vLLM 本地 | `http://127.0.0.1:8000/v1` | 任意已加载模型 |

## 设计要点

- **纯 stdlib**：`internal/llm` 只用 `net/http + encoding/json`，零第三方依赖，离线可编译
- **30s 超时**：`Chat()` 自带超时，前端不会因为模型慢卡死
- **3 选 1 降级**：模板 → 失败 fallback → 真实 LLM
- **不存 prompt**：所有对话在 HTTP 请求里即时完成，无服务端会话记忆（v0.9 再接 mem0）

## 代码位置

- `internal/llm/llm.go`：OpenAI 兼容 client
- `internal/llm/llm_test.go`：httptest 单测（成功 / 未配置 / API 错误）
- `cmd/demo/main.go`：`/api/llm/status` 和 `/api/llm/draft` 两个端点
