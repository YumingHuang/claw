# Claw（Go 版个人 AI 助手）— 产品与技术规格

## 1. 项目概述

### 1.1 目标

构建一个**类似 OpenClaw** 的、用 **Go 语言**实现的**个人 AI 助手**，具备：

- **本地优先**：核心逻辑与数据可在本机运行，隐私可控
- **多通道接入**：通过多种入口与用户交互（即时通讯、HTTP/WebSocket、CLI 等）
- **模型无关**：支持接入多种 LLM（OpenAI、Claude、本地模型等）
- **可扩展**：通过「技能 / 工具」扩展能力，支持执行真实世界动作（读文件、运行命令、搜索等）
- **流式响应**：支持 SSE / WebSocket 流式输出，降低首字等待时间

### 1.2 核心价值

- 自托管、可部署在自有环境，数据不离开用户掌控范围
- 单一内核、多入口的统一 AI 助手运行时
- 用 Go 实现：部署简单（单二进制）、并发友好、易运维
- 配置即行为：通过 YAML 配置控制人设、工具权限、模型选择，无需改代码

### 1.3 设计原则

| 原则 | 说明 |
|------|------|
| **接口优先** | 各层之间通过 Go interface 解耦，便于替换与测试 |
| **显式优于隐式** | 不使用全局变量和 init()，依赖通过构造函数注入 |
| **快速失败** | 启动时校验配置完整性，缺失必要项直接 panic 并给出明确错误信息 |
| **资源受限** | 所有 goroutine 可通过 context 取消，graceful shutdown 有超时上限 |
| **安全默认** | 工具默认关闭，需显式启用；命令执行默认空白名单 |

---

## 2. 系统架构

采用「多入口、单内核」的分层架构，参考 OpenClaw 思路并适配 Go 生态。

### 2.1 分层说明

| 层级 | 名称 | 职责 |
|------|------|------|
| **入口层 (Ingress)** | 通道与接入 | 接收用户消息：飞书/WhatsApp/Discord/HTTP/WebSocket/CLI 等，将消息标准化后送入内核 |
| **控制面 (Control Plane)** | 网关 / 协调 | 长驻核心进程：会话管理、鉴权、通道与 Provider 连接管理、对外暴露 WebSocket/HTTP API |
| **执行面 (Execution Plane)** | Agent 运行时 | 运行 Agent 循环：路由到会话、排队、调用 LLM、执行工具、重试与故障转移 |
| **能力层 (Capability)** | 工具与模型 | 工具实现（文件、命令、搜索、记忆等）；多 LLM Provider 抽象与切换、故障转移 |
| **数据层 (Data)** | 持久化与审计 | 会话、媒体、配置、日志与审计存储 |

### 2.2 数据流（简化）

```
用户消息 → [入口层] → 标准化事件 → [控制面] 会话/鉴权 → [执行面] Agent 循环
                                                              ↓
                                         LLM 调用 ← [能力层] ← 工具调用
                                                              ↓
用户回复 ← [入口层] ← 标准化回复 ← [控制面] ← [执行面] 生成回复
```

### 2.3 消息生命周期

一条用户消息从进入系统到返回回复，经历以下阶段：

```
1. RECEIVED    — Channel 收到原始消息，分配 request_id
2. NORMALIZED  — 转为内部 Message 结构，关联 session_id
3. QUEUED      — 进入 session 队列，等待当前轮次完成
4. PROCESSING  — Agent 循环开始：组装上下文 → 调用 LLM
5. TOOL_CALL   — （可选，可多轮）LLM 请求工具 → 执行 → 结果追加到上下文 → 再次调用 LLM
6. STREAMING   — LLM 返回流式 token → 通过 Channel 实时推送给用户
7. COMPLETED   — 最终回复写入会话历史，释放 session 队列槽位
8. ERROR       — 任何阶段出错均转入此状态，附带错误码与可读描述
```

### 2.4 技术选型

| 领域 | 选型 | 理由 |
|------|------|------|
| 语言 | Go 1.22+ | slog 稳定、泛型成熟 |
| 配置 | `gopkg.in/yaml.v3` + `os.Getenv` | 轻量；Viper 可选但非必须 |
| 并发 | goroutine + channel + `sync.Map` | 按 session 分 lane 排队 |
| HTTP | `net/http` + `github.com/go-chi/chi/v5` | 标准库兼容、中间件生态好 |
| WebSocket | `github.com/coder/websocket` | 维护活跃、API 简洁 |
| LLM 调用 | 统一 Provider 接口 | 对接 OpenAI-compatible、Anthropic、本地服务 |
| 日志 | `log/slog` | 标准库、结构化、零依赖 |
| 测试 | `testing` + `github.com/stretchr/testify` | 断言与 mock 辅助 |

---

## 3. 功能范围

### 3.1 第一期（MVP）

- **单通道**：HTTP API（同步 + SSE 流式）
- **单模型**：支持一个 OpenAI-compatible Provider
- **基础工具**：`get_current_time`、`read_file`、`write_file`、`run_command`
- **会话**：内存会话（按 session_id 维护上下文），可配置 TTL 与最大历史长度
- **上下文管理**：Token 计数 + 截断策略，防止超出模型上下文窗口
- **系统提示**：支持配置文件中定义 system prompt（人设、行为约束）
- **配置**：YAML 配置文件 + 环境变量覆盖
- **结构化日志**：使用 slog，request_id 贯穿请求链路
- **优雅关闭**：收到 SIGINT/SIGTERM 后，等待进行中请求完成（超时后强制退出）

### 3.2 第二期（多通道与多模型）

- **WebSocket 通道**：双向实时通信，支持打字状态推送
- **飞书通道**：支持飞书开放平台事件订阅（Webhook）与长连接两种收消息模式
- **多模型**：多 Provider 配置与自动故障转移（主→备）
- **工具扩展**：`web_search`、`memory_get/set/list`（键值记忆，按 session_id 隔离）
- **技能/插件**：从目录加载技能配置（YAML + Markdown），将 instructions 注入系统提示
- **工具权限 Profile**：按 profile（full / minimal / custom）控制可用工具集

### 3.3 第三期（进阶）

- **鉴权**：HTTP API Key（Bearer / X-API-Key）、Webhook 签名校验
- **媒体**：图片/语音的接收、存储与转发给多模态 LLM
- **审计与日志**：敏感操作审计日志（命令执行、文件写入），日志可脱敏
- **持久化会话**：SQLite 落盘，支持跨重启恢复
- **持久化记忆**：`memory_*` 在配置 SQLite 后跨重启保留
- **速率限制**：按 session / API key 限制请求频率（令牌桶算法）
- **部署**：多阶段 Docker 构建、docker-compose 示例、systemd unit 文件
- **可观测**：Prometheus metrics（请求数、延迟 P50/P99、工具调用次数、Token 消耗）

### 3.4 非目标（当前不做）

- 不实现完整 UI 管理后台（仅提供 `GET /status` 状态页面）
- 不替代 OpenClaw 的 TypeScript 生态，仅作 Go 版参考实现
- 不承诺与 OpenClaw 协议/插件 100% 兼容
- 不实现多租户隔离（面向个人使用）
- 不实现模型微调或训练功能

---

## 4. 核心概念定义

### 4.1 概念总览

| 概念 | 说明 | 生命周期 |
|------|------|----------|
| **Session** | 一次用户对话上下文，由 `session_id` 标识 | 创建 → 活跃 → 过期回收 |
| **Channel** | 消息入口适配器（HTTP、WebSocket、飞书等） | 随进程启停 |
| **Provider** | LLM 服务提供方，实现统一聊天接口 | 配置加载时初始化 |
| **Tool** | Agent 可调用的原子能力 | 注册后常驻 |
| **Skill** | 工具的使用说明 + 提示词集合（第二期） | 启动时从目录加载 |
| **AgentLoop** | 单次请求的 LLM 多轮调用循环 | 随请求创建与结束 |

### 4.2 Session 详细设计

```go
type Session struct {
    ID        string
    Channel   string       // 来源通道标识
    Messages  []Message    // 对话历史
    Metadata  map[string]string
    CreatedAt time.Time
    UpdatedAt time.Time
    TokenCount int         // 当前历史消耗的估算 token 数
}
```

**上下文窗口管理策略**：

- 每次调用 LLM 前，估算当前消息历史的 token 数（使用 tiktoken 兼容算法或简单字符数/4 近似）
- 若超出模型上下文窗口（可配置，如 128000），从历史最早消息开始移除，但**始终保留 system prompt**
- 移除时以完整的 user-assistant 对为单位，避免上下文断裂
- 可配置 `max_history_messages` 作为硬性上限（如 100 条）

### 4.3 Tool 详细设计

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() JSONSchema          // OpenAI function calling 格式的参数 schema
    Execute(ctx context.Context, params json.RawMessage) (ToolResult, error)
}

type ToolResult struct {
    Content  string `json:"content"`
    IsError  bool   `json:"is_error,omitempty"`
}
```

**工具安全模型**：

| 安全层 | 机制 |
|--------|------|
| 白名单 | `run_command` 仅允许配置中列出的命令（如 `ls`, `cat`, `date`） |
| 路径沙箱 | `read_file` / `write_file` 限制在配置的 `workdir` 下，禁止 `..` 路径穿越 |
| 超时 | 每个工具调用有独立超时（默认 30s，可配置） |
| 最大轮数 | Agent 循环最多执行 N 轮工具调用（默认 10），防止无限循环 |
| 输出截断 | 工具输出超过 `max_tool_output_chars`（默认 10000）时截断 |

### 4.4 Provider 接口

```go
type Provider interface {
    Name() string
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
}

type ChatRequest struct {
    Model       string
    Messages    []Message
    Tools       []ToolSchema
    Temperature float64
    MaxTokens   int
}

type StreamChunk struct {
    Delta     string      // 增量文本
    ToolCalls []ToolCall  // 增量工具调用
    Done      bool
    Usage     *Usage      // 仅最后一个 chunk 包含
    Err       error
}
```

**故障转移策略**（第二期）：

1. 主 Provider 调用失败（网络错误 / 5xx / 超时）时，自动重试 1 次
2. 重试仍失败，切换到备用 Provider（需在配置中指定 fallback 列表）
3. 所有 Provider 均失败，返回用户友好的错误提示
4. 429（限流）响应时，按 `Retry-After` header 等待后重试

---

## 5. 接口与协议

### 5.1 内部消息结构

```go
type Message struct {
    Role       string          `json:"role"`        // "system" | "user" | "assistant" | "tool"
    Content    string          `json:"content"`
    ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
    ToolCallID string          `json:"tool_call_id,omitempty"`
    Timestamp  time.Time       `json:"timestamp"`
}

type ToolCall struct {
    ID       string          `json:"id"`
    Name     string          `json:"name"`
    Arguments json.RawMessage `json:"arguments"`
}
```

### 5.2 HTTP API

#### `POST /v1/chat` — 同步聊天

请求：
```json
{
  "session_id": "abc-123",           // 可选，为空则自动生成
  "message": "今天天气怎么样？",
  "stream": false                     // 是否流式返回
}
```

响应（非流式）：
```json
{
  "session_id": "abc-123",
  "request_id": "req-uuid",
  "message": {
    "role": "assistant",
    "content": "我无法直接查看天气，但可以帮你...",
    "timestamp": "2026-03-06T10:30:00Z"
  },
  "usage": {
    "prompt_tokens": 150,
    "completion_tokens": 45,
    "total_tokens": 195
  },
  "tool_calls_count": 0
}
```

响应（流式，`stream: true`）：返回 `text/event-stream`（SSE）

```
event: chunk
data: {"delta": "我无法", "done": false}

event: chunk
data: {"delta": "直接查看天气", "done": false}

event: done
data: {"session_id": "abc-123", "usage": {"total_tokens": 195}}
```

#### `GET /v1/sessions/:id` — 获取会话历史

响应：
```json
{
  "session_id": "abc-123",
  "messages": [...],
  "created_at": "2026-03-06T10:00:00Z",
  "message_count": 12,
  "token_count": 3400
}
```

#### `DELETE /v1/sessions/:id` — 删除会话

#### `GET /health` — 健康检查

响应：
```json
{
  "status": "ok",
  "version": "0.1.0",
  "uptime_seconds": 3600,
  "providers": {
    "openai": "healthy"
  }
}
```

#### `GET /status` — 运行状态（调试用）

响应：
```json
{
  "active_sessions": 5,
  "total_requests": 1234,
  "providers": [
    {"name": "openai", "status": "healthy", "requests": 1000, "errors": 2}
  ],
  "tools": ["get_current_time", "read_file", "write_file", "run_command"]
}
```

### 5.3 WebSocket 协议（第二期）

连接：`GET /v1/ws?session_id=abc-123`

上行消息（客户端→服务端）：
```json
{"type": "message", "content": "你好"}
{"type": "ping"}
```

下行消息（服务端→客户端）：
```json
{"type": "chunk", "delta": "你好", "done": false}
{"type": "chunk", "delta": "！", "done": true, "usage": {...}}
{"type": "status", "status": "thinking"}
{"type": "status", "status": "calling_tool", "tool": "read_file"}
{"type": "pong"}
{"type": "error", "code": "rate_limited", "message": "请稍后再试"}
```

### 5.4 错误响应格式

所有 API 错误统一格式：

```json
{
  "error": {
    "code": "invalid_request",
    "message": "session_id is required",
    "request_id": "req-uuid"
  }
}
```

标准错误码：

| HTTP 状态 | 错误码 | 说明 |
|-----------|--------|------|
| 400 | `invalid_request` | 请求格式错误 |
| 401 | `unauthorized` | 缺失或无效的 API Key |
| 404 | `session_not_found` | 会话不存在或已过期 |
| 429 | `rate_limited` | 请求过于频繁 |
| 500 | `internal_error` | 服务端内部错误 |
| 502 | `provider_error` | LLM Provider 返回错误 |
| 504 | `provider_timeout` | LLM Provider 调用超时 |

---

## 6. 配置 Schema

完整配置结构（YAML）：

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  read_timeout: 30s      # HTTP 读超时
  write_timeout: 120s     # HTTP 写超时（需留足 LLM 响应时间）
  shutdown_timeout: 15s   # 优雅关闭超时

log:
  level: "info"           # debug | info | warn | error
  format: "json"          # json | text
  output: "stdout"        # stdout | stderr | 文件路径

system_prompt: |
  你是 Claw，一个智能个人助手。

  ## 核心行为
  - 主动思考用户问题背后的真实意图，不要只回答表面问题
  - 给出完整、有条理的回答，必要时分步骤说明
  - 如果用户的问题模糊，先给出最可能的回答，再简要补充其他可能性
  - 主动提供相关的补充信息和建议，而不是等用户追问
  - 当涉及多步操作时，一次性给出完整方案

  ## 工具使用
  - 需要查时间、读写文件、执行命令时，主动调用工具
  - 可以组合多个工具完成复杂任务
  - 当用户提到偏好、习惯时，用 memory_set 记住；后续用 memory_get 回忆

  ## 风格
  - 像一个经验丰富的同事在对话，自然、直接
  - 不确定时坦诚说明，但同时给出最佳建议
  - 中文回复，技术术语可保留英文

session:
  ttl: 1h                 # 会话过期时间
  max_history: 100        # 最大历史消息条数
  cleanup_interval: 5m    # 过期会话清理间隔

providers:
  default: "openai"       # 默认使用的 provider 名称
  list:
    - name: "openai"
      type: "openai_compatible"
      base_url: "https://api.openai.com/v1"
      api_key: "${OPENAI_API_KEY}"      # 支持环境变量引用
      model: "gpt-4o"
      max_tokens: 4096
      temperature: 0.7
      timeout: 60s
      context_window: 128000            # 模型上下文窗口大小
    - name: "backup"
      type: "openai_compatible"
      base_url: "https://api.backup.com/v1"
      api_key: "${BACKUP_API_KEY}"
      model: "gpt-4o-mini"
      timeout: 30s
      context_window: 128000
  fallback_order: ["openai", "backup"]  # 故障转移顺序
  retry:
    max_attempts: 2
    backoff: "1s"          # 指数退避基础间隔

tools:
  workdir: "/home/user/workspace"       # 文件操作沙箱根目录
  allowed_commands:                      # run_command 白名单
    - "ls"
    - "cat"
    - "date"
    - "wc"
    - "head"
    - "tail"
  max_output_chars: 10000               # 工具输出最大字符数
  timeout: 30s                           # 单次工具调用超时
  max_iterations: 10                     # Agent 循环最大轮数

channels:
  http:
    enabled: true
  websocket:
    enabled: false
    ping_interval: 30s
  feishu:
    enabled: false
    app_id: "${FEISHU_APP_ID}"
    app_secret: "${FEISHU_APP_SECRET}"
    long_connection: false
    verification_token: "${FEISHU_VERIFICATION_TOKEN}"
    encrypt_key: "${FEISHU_ENCRYPT_KEY}"

auth:
  enabled: false
  api_keys:
    - "${CLAW_API_KEY}"

rate_limit:
  enabled: false
  requests_per_minute: 30
  burst: 5
```

**环境变量覆盖规则**：配置值中 `${VAR_NAME}` 会在加载时替换为对应环境变量。环境变量也可以通过 `CLAW_` 前缀直接覆盖配置项（如 `CLAW_SERVER_PORT=9090` 覆盖 `server.port`）。

---

## 7. 非功能需求

### 7.1 可用性

- 单实例运行，会话默认内存存储，可通过配置切换为持久化
- 优雅关闭：收到终止信号后，停止接收新请求，等待进行中请求完成（超时后强制退出）
- 启动时校验所有必要配置项与 Provider 可达性

### 7.2 安全

- API Key 与密钥仅通过配置/环境变量注入，不写死在代码中
- 工具执行限制在配置的目录与命令白名单内
- 路径参数做 `filepath.Clean` + 前缀检查，防止路径穿越
- `run_command` 不使用 shell 执行（直接 `exec.Command`），避免命令注入

### 7.3 可观测

- 结构化日志（JSON 格式），每条日志包含 request_id、session_id
- request_id 在入口层生成，贯穿整个请求链路
- 关键指标日志：LLM 调用延迟、token 消耗、工具调用次数
- Prometheus metrics endpoint（`/metrics`）
- 提供 `GET /health` 与 `GET /ready`

### 7.4 性能目标

| 指标 | 目标 |
|------|------|
| 首字延迟（流式） | ≤ LLM 首字延迟 + 50ms |
| 内存占用（空闲） | ≤ 50MB |
| 并发会话 | ≥ 100 |
| 启动时间 | ≤ 2s |
| 优雅关闭 | ≤ 配置的 shutdown_timeout |

---

## 8. Agent 循环详细流程

```
func AgentLoop(ctx, session, userMessage):
    session.Append(userMessage)
    iteration = 0

    LOOP:
        if iteration >= max_iterations:
            return Error("达到最大工具调用轮数")

        messages = buildContext(session)    // system prompt + 历史（截断至窗口内）
        toolSchemas = getEnabledTools()

        response = provider.Chat(ctx, messages, toolSchemas)
        // 或 provider.ChatStream(ctx, ...) 用于流式

        if response.HasToolCalls():
            for each toolCall in response.ToolCalls:
                result = toolRegistry.Execute(ctx, toolCall)
                session.Append(ToolCallMessage(toolCall))
                session.Append(ToolResultMessage(toolCall.ID, result))
            iteration++
            goto LOOP

        session.Append(AssistantMessage(response.Content))
        return response.Content
```

关键约束：
- `buildContext` 确保总 token 数不超过模型窗口限制
- 每次工具调用受独立超时保护
- 整个 AgentLoop 受请求级超时保护（`server.write_timeout`）
- `Run` 和 `RunStream` 均支持完整的 tool call 循环
- `ChatRequest` 携带配置中的 `temperature` 和 `max_tokens`，确保配置直达 LLM

---

## 9. 文档与交付物

- **spec.md**（本文）：产品与架构规格
- **plan.md**：实施计划与里程碑
- **README.md**：构建、配置、运行与最小示例
- **config.example.yaml**：完整配置示例，含注释说明
- 代码内注释：仅在关键设计决策与非显然逻辑处添加

---

## 10. 术语表

| 术语 | 定义 |
|------|------|
| Agent 循环 | LLM 调用 → 判断是否需要工具 → 执行工具 → 再次调用 LLM 的循环过程 |
| 上下文窗口 | LLM 单次调用能处理的最大 token 数 |
| 故障转移 | 主 Provider 失败时自动切换到备用 Provider |
| 路径沙箱 | 限制文件操作在指定目录内，防止访问系统其他文件 |
| 命令白名单 | 仅允许执行预先配置的命令列表 |
| SSE | Server-Sent Events，HTTP 流式推送协议 |
| Token | LLM 处理文本的最小单位，约 4 个英文字符或 1-2 个中文字符 |

---

*文档版本：v0.2 | 基于 OpenClaw 架构思路，适配 Go 实现*
