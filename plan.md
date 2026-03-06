# Claw（Go 版）— 实施计划

## 1. 总览

本计划将 **spec.md** 中的规格拆解为可执行的阶段与任务，按「先内核、再通道、后扩展」的顺序推进。

**核心策略**：每个 Phase 结束时都有可运行、可验证的产出物。任务按依赖关系排列——前置任务完成后才开始后续任务。

---

## 2. 阶段与里程碑

| 阶段 | 目标 | 预计耗时 | 验收标准 |
|------|------|----------|----------|
| **Phase 0** | 项目骨架与约定 | 1～2 天 | `go build` 成功，配置加载并打印，单元测试通过 |
| **Phase 1** | 内核与单通道 MVP | 1～2 周 | curl 发消息→收到 LLM 回复，工具调用正常，流式输出可用 |
| **Phase 2** | 多通道与多模型 | 1～2 周 | Telegram 或 WebSocket 通道可用，故障转移正常，技能加载生效 |
| **Phase 3** | 安全与运维 | 1 周 | Docker 构建通过，鉴权生效，审计日志可查，metrics 可抓取 |

---

## 3. Phase 0：项目骨架（约 1～2 天）

### 3.1 目录结构

```
claw/
├── cmd/
│   └── claw/                # main 入口
│       └── main.go
├── internal/
│   ├── config/              # 配置加载与校验
│   │   ├── config.go        # Config 结构体与 Load 函数
│   │   └── config_test.go
│   ├── gateway/             # 控制面：路由、会话管理
│   │   ├── gateway.go
│   │   ├── session.go       # Session 存储与生命周期
│   │   └── session_test.go
│   ├── agent/               # 执行面：Agent 循环、排队
│   │   ├── agent.go         # AgentLoop 实现
│   │   ├── queue.go         # 按 session 分 lane 排队
│   │   └── agent_test.go
│   ├── llm/                 # 能力层：Provider 抽象与实现
│   │   ├── provider.go      # Provider 接口定义
│   │   ├── openai.go        # OpenAI-compatible 实现
│   │   ├── openai_test.go
│   │   └── token.go         # Token 计数估算
│   ├── tools/               # 能力层：工具实现与注册
│   │   ├── registry.go      # 工具注册表
│   │   ├── time.go          # get_current_time
│   │   ├── file.go          # read_file, write_file
│   │   ├── command.go       # run_command
│   │   └── tools_test.go
│   ├── channels/            # 入口层：各 channel 适配器
│   │   ├── channel.go       # Channel 接口定义
│   │   ├── http.go          # HTTP + SSE channel
│   │   └── http_test.go
│   └── models/              # 内部数据结构
│       ├── message.go       # Message, ToolCall, ToolResult
│       ├── errors.go        # 统一错误码与 APIError
│       └── request.go       # ChatRequest, ChatResponse
├── configs/
│   └── config.example.yaml  # 完整示例配置（带注释）
├── spec.md
├── plan.md
├── README.md
├── Makefile                  # build, test, lint, run 快捷命令
├── .golangci.yml             # linter 配置
├── go.mod
└── go.sum
```

### 3.2 任务清单

任务按依赖顺序排列，每个任务标注了前置依赖。

#### T0.1 初始化项目（无前置）
- [ ] 初始化 `go mod init github.com/your-username/claw`，Go 1.22+
- [ ] 创建上述目录结构（空目录放 `.gitkeep`）
- [ ] 创建 `Makefile`，包含 `build`、`run`、`test`、`lint` 目标
- [ ] 创建 `.gitignore`（忽略二进制、.env、IDE 配置）

#### T0.2 配置模块（依赖 T0.1）
- [ ] 实现 `internal/config/config.go`：
  - 定义 `Config` 结构体，与 spec.md §6 的配置 schema 对应
  - `Load(path string) (*Config, error)`：从 YAML 文件加载
  - 支持 `${ENV_VAR}` 语法在字符串值中引用环境变量
  - 启动时校验必填项（如 provider api_key），缺失时返回明确错误
  - 为可选项提供合理默认值
- [ ] 编写 `config_test.go`：测试加载正常配置、缺失必填项报错、环境变量替换
- [ ] 创建 `configs/config.example.yaml`（带详细注释）

#### T0.3 统一数据结构（依赖 T0.1）
- [ ] 实现 `internal/models/message.go`：Message、ToolCall、ToolResult 结构体
- [ ] 实现 `internal/models/errors.go`：统一 APIError 结构与标准错误码常量
- [ ] 实现 `internal/models/request.go`：ChatRequest、ChatResponse

#### T0.4 入口与 README（依赖 T0.2）
- [ ] 实现 `cmd/claw/main.go`：解析 `-config` flag → 加载配置 → 打印版本后退出
- [ ] 编写 README.md：项目简介、快速开始（克隆→配置→构建→运行）
- [ ] 设置 `.golangci.yml` 基础 linter 规则

---

## 4. Phase 1：内核与单通道 MVP（约 1～2 周）

### 4.1 LLM Provider 层（依赖 Phase 0）

#### T1.1 Provider 接口定义
- [ ] 在 `internal/llm/provider.go` 定义：
  - `Provider` 接口：`Name()`, `Chat(ctx, ChatRequest) (*ChatResponse, error)`, `ChatStream(ctx, ChatRequest) (<-chan StreamChunk, error)`
  - `ChatRequest`：Model, Messages, Tools, Temperature, MaxTokens
  - `ChatResponse`：Content, ToolCalls, Usage, FinishReason
  - `StreamChunk`：Delta, ToolCalls, Done, Usage, Err

#### T1.2 OpenAI-Compatible Provider 实现（依赖 T1.1, T0.3）
- [ ] 实现 `internal/llm/openai.go`：
  - 构造函数接收 ProviderConfig，初始化 HTTP client（含超时）
  - `Chat`：组装 OpenAI 格式请求体 → POST → 解析响应
  - `ChatStream`：`stream: true` → 读取 SSE 流 → 逐行解析 → 写入 channel
  - 错误处理：HTTP 状态码映射到内部错误码（429→rate_limited，5xx→provider_error）
  - 重试逻辑：根据配置的 max_attempts + 指数退避
- [ ] 编写测试：mock HTTP server 测试正常响应、流式响应、错误响应

#### T1.3 Token 计数（依赖 T1.1）
- [ ] 实现 `internal/llm/token.go`：
  - `EstimateTokens(messages []Message) int`：简单估算（中文按 1 字 ≈ 2 token，英文按 4 字符 ≈ 1 token）
  - `TruncateMessages(messages []Message, maxTokens int) []Message`：从历史最早处移除，保留 system prompt
  - 第一期用简单估算，后续可引入 tiktoken 库

### 4.2 工具层（依赖 Phase 0）

#### T1.4 工具接口与注册表（依赖 T0.3）
- [ ] 在 `internal/tools/registry.go` 定义：
  - `Tool` 接口：`Name()`, `Description()`, `Parameters() JSONSchema`, `Execute(ctx, json.RawMessage) (ToolResult, error)`
  - `Registry` 结构：注册工具、按名称查找、生成 OpenAI function calling 格式的 tools schema 列表
- [ ] 编写 Registry 单元测试

#### T1.5 基础工具实现（依赖 T1.4, T0.2）
- [ ] `internal/tools/time.go`：`get_current_time` — 返回当前时间（可指定时区）
- [ ] `internal/tools/file.go`：
  - `read_file`：参数 `path`，在 workdir 沙箱内读取文件，输出截断至 max_output_chars
  - `write_file`：参数 `path`, `content`，在 workdir 沙箱内写入
  - 路径安全：`filepath.Clean` → 检查 `filepath.Rel` 结果不以 `..` 开头
- [ ] `internal/tools/command.go`：
  - `run_command`：参数 `command`, `args`
  - 检查 command 在白名单内；使用 `exec.CommandContext`（不经过 shell）
  - stdout + stderr 合并输出，超时与输出截断
- [ ] 为每个工具编写单元测试（正常路径 + 安全边界：路径穿越、非白名单命令）

### 4.3 Agent 执行面（依赖 T1.2, T1.5）

#### T1.6 Agent 循环（依赖 T1.2, T1.4）
- [ ] 在 `internal/agent/agent.go` 实现：
  - `Agent` 结构：持有 Provider、Registry、Config
  - `Run(ctx, session *Session, userMessage string) (string, error)`：
    1. 追加 user message 到 session
    2. 调用 `token.TruncateMessages` 确保不超出上下文窗口
    3. 调用 `provider.Chat`（带 tools schema）
    4. 若 response 含 tool_calls → 逐个执行 → 追加 tool result → 回到步骤 2
    5. 达到 max_iterations 或无 tool_calls → 返回 assistant content
  - `RunStream(ctx, session, userMessage) (<-chan StreamChunk, error)`：流式版本
- [ ] 关键测试：mock Provider 返回 tool_call → 验证工具被执行 → 验证再次调用 LLM

#### T1.7 Session 排队（依赖 T1.6）
- [ ] 在 `internal/agent/queue.go` 实现：
  - 按 session_id 分 lane，同一 session 串行（使用 `sync.Mutex` map 或 per-session channel）
  - 不同 session 完全并发
  - 同一 session 的第二个请求排队等待，而非立即失败

### 4.4 控制面 — Gateway（依赖 T1.6）

#### T1.8 Session 存储（依赖 T0.3）
- [ ] 在 `internal/gateway/session.go` 实现：
  - `SessionStore` 接口：`Get(id)`, `GetOrCreate(id, channel)`, `Delete(id)`, `List()`
  - `MemorySessionStore`：`sync.Map` 实现，后台 goroutine 定时清理过期 session
  - Session 包含：ID, Channel, Messages, CreatedAt, UpdatedAt, TokenCount, mutex
- [ ] 编写测试：并发创建/获取、TTL 过期清理

#### T1.9 Gateway 核心（依赖 T1.6, T1.8）
- [ ] 在 `internal/gateway/gateway.go` 实现：
  - `Gateway` 结构：持有 Agent、SessionStore、Config
  - `HandleMessage(ctx, sessionID, channel, message) (*ChatResponse, error)`
  - `HandleMessageStream(ctx, sessionID, channel, message) (<-chan StreamChunk, error)`
  - 生成 request_id → 注入到 context → slog 记录请求/响应摘要

### 4.5 入口层 — HTTP Channel（依赖 T1.9）

#### T1.10 HTTP 路由与处理器（依赖 T1.9）
- [ ] 在 `internal/channels/http.go` 实现：
  - 使用 `chi` 路由器，中间件：request_id 注入、结构化日志、panic recovery
  - `POST /v1/chat`：解析请求 → 调用 Gateway → 返回 JSON 或 SSE 流
  - `GET /v1/sessions/:id`：返回会话历史
  - `DELETE /v1/sessions/:id`：删除会话
  - `GET /health`：健康检查
  - `GET /status`：运行状态
- [ ] SSE 流式输出：设置 `Content-Type: text/event-stream`，逐 chunk 写入并 flush
- [ ] 编写 HTTP handler 测试（`httptest`）

### 4.6 集成与验收（依赖 T1.10）

#### T1.11 main.go 串联（依赖以上全部）
- [ ] 更新 `cmd/claw/main.go`：
  - 解析 flag → 加载 Config → 初始化 slog
  - 创建 Provider → 注册 Tools → 创建 Agent → 创建 SessionStore → 创建 Gateway
  - 创建 HTTP Channel → 启动 HTTP Server
  - 监听 SIGINT/SIGTERM → graceful shutdown（先关 HTTP → 等待进行中请求 → 退出）
- [ ] 手动验收测试脚本（可放入 `scripts/test_smoke.sh`）：
  ```bash
  # 健康检查
  curl http://localhost:8080/health

  # 简单对话
  curl -X POST http://localhost:8080/v1/chat \
    -H "Content-Type: application/json" \
    -d '{"message": "你好，请告诉我现在的时间"}'

  # 流式对话
  curl -N -X POST http://localhost:8080/v1/chat \
    -H "Content-Type: application/json" \
    -d '{"message": "读取 /tmp/test.txt 的内容", "stream": true}'
  ```
- [ ] 编写关键路径集成测试（启动完整服务 → 发 HTTP 请求 → 验证响应）

---

## 5. Phase 2：多通道与多模型（约 1～2 周）

### 5.1 多 Provider 与故障转移

#### T2.1 多 Provider 管理（依赖 Phase 1）
- [ ] 实现 `internal/llm/manager.go`：
  - `ProviderManager`：管理多个 Provider 实例
  - 按配置的 `fallback_order` 排列
  - `Chat` / `ChatStream` 方法：尝试 default → 失败则沿 fallback_order 依次尝试
  - 失败判定：网络错误、5xx、超时；不含 4xx（客户端错误不应切换）
  - 429 特殊处理：读取 `Retry-After` header，等待后重试同一 Provider
- [ ] 更新 Agent 使用 ProviderManager 替代单个 Provider

#### T2.2 Anthropic Claude Provider（可选，依赖 T1.1）
- [ ] 实现 `internal/llm/anthropic.go`：适配 Anthropic Messages API
  - 消息格式转换（OpenAI → Anthropic）
  - 工具调用格式适配
  - 流式响应解析

### 5.2 新通道

#### T2.3 Channel 接口抽象（依赖 Phase 1）
- [ ] 在 `internal/channels/channel.go` 定义：
  ```go
  type Channel interface {
      Name() string
      Start(ctx context.Context, gw *gateway.Gateway) error
      Stop(ctx context.Context) error
  }
  ```
- [ ] 重构 HTTP channel 实现该接口
- [ ] 在 main.go 中统一管理 Channel 生命周期（遍历启动/关闭）

#### T2.4 WebSocket Channel（依赖 T2.3）
- [ ] 实现 `internal/channels/websocket.go`：
  - 连接建立：`GET /v1/ws?session_id=xxx`
  - 上行：JSON 消息 `{"type": "message", "content": "..."}`
  - 下行：流式 chunk、状态推送（thinking、calling_tool）、错误
  - 心跳：定时 ping/pong，超时断开
  - 连接管理：按 session_id 关联，同 session 多连接广播

#### T2.5 Telegram Channel（依赖 T2.3）
- [ ] 实现 `internal/channels/telegram.go`：
  - 长轮询模式（`getUpdates`），可选 Webhook 模式
  - 接收文本消息 → 以 chat_id 作为 session_id → 调用 Gateway → 回发文本
  - 配置 `allowed_users` 白名单过滤
  - 长消息分段发送（Telegram 单消息限 4096 字符）

### 5.3 工具扩展

#### T2.6 Web Search 工具（依赖 T1.4）
- [ ] 实现 `internal/tools/search.go`：
  - 调用 SearXNG 或 Tavily API（可配置搜索引擎后端）
  - 参数：`query`, `num_results`
  - 返回：标题 + 摘要 + URL 列表
  - 限流：配置每分钟最大搜索次数

#### T2.7 Memory 工具（依赖 T1.4）
- [ ] 实现 `internal/tools/memory.go`：
  - `memory_get`：参数 `key`，从命名空间读取
  - `memory_set`：参数 `key`, `value`，写入命名空间
  - `memory_list`：列出当前命名空间所有 key
  - 命名空间按 session_id 隔离
  - 存储后端：内存 map（第一版），可扩展为文件/SQLite

#### T2.8 工具权限 Profile（依赖 T1.4）
- [ ] 在配置中支持工具 profile：
  ```yaml
  tools:
    profiles:
      full: ["get_current_time", "read_file", "write_file", "run_command", "web_search", "memory_*"]
      minimal: ["get_current_time", "read_file"]
      safe: ["get_current_time"]
    default_profile: "full"
  ```
- [ ] Gateway 根据 session 或 API key 关联的 profile 决定可用工具集

### 5.4 技能（Skill）加载

#### T2.9 技能系统（依赖 T1.4）
- [ ] 定义技能目录格式：
  ```
  skills/
  ├── code_review/
  │   ├── skill.yaml          # name, description, tools, enabled
  │   └── instructions.md     # 提示词模板
  └── translator/
      ├── skill.yaml
      └── instructions.md
  ```
- [ ] `skill.yaml` 结构：
  ```yaml
  name: "code_review"
  description: "代码审查助手"
  tools: ["read_file"]
  enabled: true
  ```
- [ ] 实现 `internal/skills/loader.go`：
  - 启动时扫描技能目录
  - 将 `instructions.md` 内容追加到 system prompt
  - 验证 tools 列表中的工具已注册
- [ ] 编写文档：如何创建自定义技能

---

## 6. Phase 3：安全与运维（约 1 周）

### 6.1 鉴权

#### T3.1 API Key 鉴权（依赖 Phase 1）
- [ ] 实现 `internal/channels/middleware.go`：
  - `AuthMiddleware`：检查 `Authorization: Bearer <key>` 或 `X-API-Key` header
  - 与配置中的 `auth.api_keys` 列表比对（constant-time 比较，防时序攻击）
  - `auth.enabled: false` 时跳过鉴权
  - 未通过 → 返回 401 统一错误格式

#### T3.2 Telegram 签名校验（依赖 T2.5）
- [ ] Webhook 模式下校验 Telegram 请求签名
- [ ] 长轮询模式下信任 bot token 即可

### 6.2 速率限制

#### T3.3 请求限流（依赖 Phase 1）
- [ ] 实现令牌桶算法限流中间件：
  - 按 API Key 或 IP 地址限流
  - 配置 `requests_per_minute` + `burst`
  - 超限 → 返回 429 + `Retry-After` header

### 6.3 审计与日志

#### T3.4 审计日志（依赖 Phase 1）
- [ ] 定义审计事件：`tool_executed`、`file_written`、`command_run`、`session_created`、`auth_failed`
- [ ] 审计日志输出到独立文件（或 slog 的独立 handler），格式化为 JSON
- [ ] 敏感内容脱敏：API Key 仅显示前 4 位，文件内容超长时截断

### 6.4 持久化会话

#### T3.5 SQLite Session 存储（可选，依赖 T1.8）
- [ ] 实现 `internal/gateway/session_sqlite.go`：
  - 使用 `modernc.org/sqlite`（纯 Go，无 CGO）
  - 实现 `SessionStore` 接口
  - 自动建表、按 TTL 清理

### 6.5 部署与可观测

#### T3.6 Docker 化（依赖 Phase 1）
- [ ] 编写多阶段 `Dockerfile`：
  ```dockerfile
  # 构建阶段
  FROM golang:1.22-alpine AS builder
  # ... build ...

  # 运行阶段
  FROM alpine:3.19
  COPY --from=builder /app/claw /usr/local/bin/claw
  ENTRYPOINT ["claw"]
  ```
- [ ] 编写 `docker-compose.yml`：挂载配置文件、设置环境变量
- [ ] 编写 systemd unit 文件示例（`deploy/claw.service`）

#### T3.7 Prometheus Metrics（可选，依赖 Phase 1）
- [ ] 使用 `prometheus/client_golang` 暴露 `/metrics` endpoint
- [ ] 指标：
  - `claw_requests_total{channel, status}`
  - `claw_request_duration_seconds{channel}` (histogram)
  - `claw_llm_calls_total{provider, status}`
  - `claw_llm_duration_seconds{provider}` (histogram)
  - `claw_tool_calls_total{tool, status}`
  - `claw_tokens_total{provider, type}` (prompt/completion)
  - `claw_active_sessions`

#### T3.8 Readiness 检查（依赖 Phase 1）
- [ ] `GET /ready`：检查默认 Provider 可达（轻量级 API 调用或 TCP 连接检查）
- [ ] 返回 200（ready）或 503（not ready）

---

## 7. 依赖建议（go.mod）

| 用途 | 依赖 | 备注 |
|------|------|------|
| HTTP 路由 | `github.com/go-chi/chi/v5` | 轻量、兼容 `net/http` |
| WebSocket | `github.com/coder/websocket` | 维护活跃 |
| YAML 解析 | `gopkg.in/yaml.v3` | 标准 |
| HTTP 客户端 | `net/http`（标准库） | 无需额外依赖 |
| Telegram | `gopkg.in/telebot.v3` | API 简洁 |
| 日志 | `log/slog`（标准库） | Go 1.22+ 内置 |
| 测试 | `github.com/stretchr/testify` | 断言与 mock |
| SQLite | `modernc.org/sqlite` | 纯 Go、无 CGO |
| Metrics | `github.com/prometheus/client_golang` | 可选，Phase 3 |
| UUID | `github.com/google/uuid` | request_id 生成 |

---

## 8. 测试策略

### 8.1 测试分层

| 层级 | 范围 | 工具 | 运行时机 |
|------|------|------|----------|
| 单元测试 | 函数/方法级，mock 外部依赖 | `testing` + `testify` | 每次提交 |
| 集成测试 | 模块间交互（如 Agent + mock Provider） | `testing` + `httptest` | 每次提交 |
| 冒烟测试 | 启动完整服务 → curl 验证 | `scripts/test_smoke.sh` | 发布前 |
| 手动测试 | 真实 LLM Provider 端到端 | curl / Telegram | 里程碑结束 |

### 8.2 关键测试用例（最低要求）

- [ ] Config：加载正常 YAML、缺失必填项报错、环境变量替换
- [ ] Provider：正常响应解析、流式响应解析、HTTP 错误映射、超时处理
- [ ] Tools：每个工具的正常路径 + 边界（路径穿越、非白名单命令、输出截断）
- [ ] Agent：tool_call 循环（mock Provider 第一次返回 tool_call、第二次返回 text）
- [ ] Session：并发创建/获取、TTL 过期清理
- [ ] HTTP Handler：正常请求/响应、错误格式、SSE 流式输出

### 8.3 测试覆盖率目标

- 核心模块（`agent`、`llm`、`tools`、`config`）：≥ 70%
- 整体项目：≥ 50%

---

## 9. 代码质量与 CI

### 9.1 Linter 配置（`.golangci.yml`）

启用的 linter：
- `govet`、`errcheck`、`staticcheck`：基础错误检查
- `gofmt`、`goimports`：格式化
- `gosec`：安全检查
- `misspell`：拼写检查

### 9.2 Makefile 目标

```makefile
build:     go build -o bin/claw ./cmd/claw
run:       go run ./cmd/claw -config configs/config.yaml
test:      go test ./... -race -count=1
lint:      golangci-lint run ./...
coverage:  go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out
clean:     rm -rf bin/ coverage.out
```

### 9.3 CI 流水线（GitHub Actions，可选）

```
on: [push, pull_request]
jobs:
  - lint (golangci-lint)
  - test (go test -race)
  - build (go build，验证可编译)
```

---

## 10. 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| LLM 成本失控 | 高额 API 费用 | 配置 token 预算上限、日志监控 token 消耗 |
| 工具安全漏洞 | 任意命令执行、文件泄露 | 白名单+沙箱+path clean，默认最小权限 |
| 同 session 并发 | 消息乱序、上下文污染 | 按 session 串行排队 |
| Provider 不可用 | 服务中断 | 故障转移+重试，多 Provider 配置 |
| 上下文窗口溢出 | LLM 报错或截断 | Token 估算+自动截断策略 |
| 工具执行超时 | 请求阻塞 | 独立超时+context cancel |

---

## 11. 验收标准

### Phase 0 验收
- [ ] `go build ./...` 成功
- [ ] `go test ./internal/config/...` 通过
- [ ] `./bin/claw -config configs/config.example.yaml` 能加载配置并打印后退出

### Phase 1 验收（MVP）
- [ ] `curl POST /v1/chat` 发送消息，收到 LLM 回复
- [ ] 请求「当前时间」，Agent 调用 `get_current_time` 工具并返回结果
- [ ] 请求「读取某文件」，Agent 调用 `read_file` 并返回文件内容
- [ ] `stream: true` 时，响应为 SSE 流式输出
- [ ] 路径穿越尝试（`../../../etc/passwd`）被正确拒绝
- [ ] 非白名单命令被正确拒绝
- [ ] 同一 session 多轮对话上下文保持
- [ ] 优雅关闭生效（进行中请求完成后退出）
- [ ] `go test ./...` 全部通过，无 race 条件
- [ ] README 包含完整的「克隆→配置→构建→运行→测试」步骤

### Phase 2 验收
- [ ] 至少一个新通道（WebSocket 或 Telegram）可正常收发消息
- [ ] 主 Provider 模拟故障后，自动切换到备用 Provider
- [ ] 技能加载生效，instructions 出现在系统提示中

### Phase 3 验收
- [ ] `docker build` 成功，`docker run` 能正常服务
- [ ] 无 API Key 的请求被 401 拒绝
- [ ] 审计日志文件中包含工具调用记录
- [ ] `/metrics` endpoint 返回 Prometheus 格式指标

---

## 12. 任务依赖关系图

```
Phase 0:  T0.1 ──→ T0.2 ──→ T0.4
               └──→ T0.3 ──┘

Phase 1:  T0.2 ──→ T1.1 ──→ T1.2 ──→ T1.6 ──→ T1.7
          T0.3 ──→ T1.4 ──→ T1.5 ──┘     │
                   T1.1 ──→ T1.3          │
          T0.3 ──→ T1.8 ──→ T1.9 ←───────┘
                              │
                              └──→ T1.10 ──→ T1.11

Phase 2:  T1.* ──→ T2.1
          T1.1 ──→ T2.2
          T1.* ──→ T2.3 ──→ T2.4
                        └──→ T2.5
          T1.4 ──→ T2.6, T2.7, T2.8, T2.9

Phase 3:  T1.* ──→ T3.1, T3.3, T3.4, T3.6, T3.7, T3.8
          T2.5 ──→ T3.2
          T1.8 ──→ T3.5
```

---

*计划版本：v0.2 | 与 spec.md v0.2 对应*
