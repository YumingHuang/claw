# Claw — 可执行任务清单

> 本文件将 plan.md 拆解为**原子级可执行任务**。每个任务有明确的输入、产出、完成标准和预估时间。
> 按顺序执行，标记 `[x]` 表示已完成。

---

## Phase 0：项目骨架

### T0.1 初始化项目结构

**预估**：30 分钟 | **前置**：无 | **产出**：可编译的空项目

- [x] **T0.1.1** 初始化 Go module
  ```bash
  go mod init github.com/YumingHuang/claw
  ```

- [x] **T0.1.2** 创建目录结构
  ```bash
  mkdir -p cmd/claw
  mkdir -p internal/{config,gateway,agents,llm,tools,channels,models}
  mkdir -p configs scripts
  ```

- [x] **T0.1.3** 创建 `cmd/claw/main.go` 占位入口
  ```go
  package main

  import "fmt"

  const version = "0.1.0"

  func main() {
      fmt.Printf("claw %s\n", version)
  }
  ```
  **完成标准**：`go run ./cmd/claw` 输出 `claw 0.1.0`

- [x] **T0.1.4** 创建 `.gitignore`
  ```
  bin/
  *.exe
  .env
  coverage.out
  .idea/
  .vscode/
  .DS_Store
  ```

- [x] **T0.1.5** 创建 `Makefile`
  ```makefile
  .PHONY: build run test lint coverage clean

  build:
  	go build -o bin/claw ./cmd/claw

  run:
  	go run ./cmd/claw -config configs/config.yaml

  test:
  	go test ./... -race -count=1

  lint:
  	golangci-lint run ./...

  coverage:
  	go test ./... -coverprofile=coverage.out
  	go tool cover -html=coverage.out -o coverage.html

  clean:
  	rm -rf bin/ coverage.out coverage.html
  ```
  **完成标准**：`make build && ./bin/claw` 输出版本号

- [x] **T0.1.6** 初始化 git 仓库
  ```bash
  git init && git add . && git commit -m "init: project skeleton"
  ```

---

### T0.2 配置模块

**预估**：2 小时 | **前置**：T0.1 | **产出**：可从 YAML + ENV 加载完整配置的 config 包

- [x] **T0.2.1** 添加 YAML 依赖
  ```bash
  go get gopkg.in/yaml.v3
  ```

- [x] **T0.2.2** 创建 `internal/config/config.go` — Config 结构体
  - 文件：`internal/config/config.go`
  - 定义以下结构体（与 spec.md §6 对应）：
    ```go
    type Config struct {
        Server       ServerConfig    `yaml:"server"`
        Log          LogConfig       `yaml:"log"`
        SystemPrompt string          `yaml:"system_prompt"`
        Session      SessionConfig   `yaml:"session"`
        Providers    ProvidersConfig `yaml:"providers"`
        Tools        ToolsConfig     `yaml:"tools"`
        Channels     ChannelsConfig  `yaml:"channels"`
        Auth         AuthConfig      `yaml:"auth"`
        RateLimit    RateLimitConfig `yaml:"rate_limit"`
    }

    type ServerConfig struct {
        Host            string        `yaml:"host"`
        Port            int           `yaml:"port"`
        ReadTimeout     time.Duration `yaml:"read_timeout"`
        WriteTimeout    time.Duration `yaml:"write_timeout"`
        ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
    }

    type LogConfig struct {
        Level  string `yaml:"level"`   // debug|info|warn|error
        Format string `yaml:"format"`  // json|text
        Output string `yaml:"output"`  // stdout|stderr|filepath
    }

    type SessionConfig struct {
        TTL             time.Duration `yaml:"ttl"`
        MaxHistory      int           `yaml:"max_history"`
        CleanupInterval time.Duration `yaml:"cleanup_interval"`
    }

    type ProviderConfig struct {
        Name          string        `yaml:"name"`
        Type          string        `yaml:"type"`
        BaseURL       string        `yaml:"base_url"`
        APIKey        string        `yaml:"api_key"`
        Model         string        `yaml:"model"`
        MaxTokens     int           `yaml:"max_tokens"`
        Temperature   float64       `yaml:"temperature"`
        Timeout       time.Duration `yaml:"timeout"`
        ContextWindow int           `yaml:"context_window"`
    }

    type ProvidersConfig struct {
        Default       string           `yaml:"default"`
        List          []ProviderConfig `yaml:"list"`
        FallbackOrder []string         `yaml:"fallback_order"`
        Retry         RetryConfig      `yaml:"retry"`
    }

    type RetryConfig struct {
        MaxAttempts int           `yaml:"max_attempts"`
        Backoff     time.Duration `yaml:"backoff"`
    }

    type ToolsConfig struct {
        Workdir         string        `yaml:"workdir"`
        AllowedCommands []string      `yaml:"allowed_commands"`
        MaxOutputChars  int           `yaml:"max_output_chars"`
        Timeout         time.Duration `yaml:"timeout"`
        MaxIterations   int           `yaml:"max_iterations"`
    }

    type ChannelsConfig struct {
        HTTP      HTTPChannelConfig      `yaml:"http"`
        WebSocket WebSocketChannelConfig `yaml:"websocket"`
        Feishu    FeishuChannelConfig    `yaml:"feishu"`
    }

    type HTTPChannelConfig struct {
        Enabled bool `yaml:"enabled"`
    }

    type WebSocketChannelConfig struct {
        Enabled      bool          `yaml:"enabled"`
        PingInterval time.Duration `yaml:"ping_interval"`
    }

    type FeishuChannelConfig struct {
        Enabled      bool     `yaml:"enabled"`
        Token        string   `yaml:"token"`
        AllowedUsers []int64  `yaml:"allowed_users"`
    }

    type AuthConfig struct {
        Enabled bool     `yaml:"enabled"`
        APIKeys []string `yaml:"api_keys"`
    }

    type RateLimitConfig struct {
        Enabled           bool `yaml:"enabled"`
        RequestsPerMinute int  `yaml:"requests_per_minute"`
        Burst             int  `yaml:"burst"`
    }
    ```

- [x] **T0.2.3** 实现 `Load` 函数
  - 文件：`internal/config/config.go`（追加）
  - 签名：`func Load(path string) (*Config, error)`
  - 逻辑：
    1. `os.ReadFile(path)` 读取 YAML
    2. 对内容做环境变量替换：用 `os.ExpandEnv` 或正则匹配 `${VAR}` → `os.Getenv("VAR")`
    3. `yaml.Unmarshal` 到 Config
    4. 调用 `cfg.setDefaults()` 填充默认值
    5. 调用 `cfg.validate()` 校验必填项
  - 默认值：
    - `server.host` = `"0.0.0.0"`, `server.port` = `8080`
    - `server.read_timeout` = `30s`, `server.write_timeout` = `120s`, `server.shutdown_timeout` = `15s`
    - `log.level` = `"info"`, `log.format` = `"json"`, `log.output` = `"stdout"`
    - `session.ttl` = `1h`, `session.max_history` = `100`, `session.cleanup_interval` = `5m`
    - `tools.max_output_chars` = `10000`, `tools.timeout` = `30s`, `tools.max_iterations` = `10`
    - `channels.http.enabled` = `true`
    - `providers.retry.max_attempts` = `2`, `providers.retry.backoff` = `1s`
  - 校验规则：
    - `providers.list` 不能为空
    - `providers.default` 必须能在 `providers.list` 中找到
    - 每个 provider 的 `api_key` 和 `base_url` 不能为空
    - `tools.workdir` 若非空则必须是绝对路径

- [x] **T0.2.4** 编写 `internal/config/config_test.go`
  - 测试用例：
    1. `TestLoad_ValidConfig` — 加载完整 YAML，验证所有字段正确
    2. `TestLoad_Defaults` — 加载最小 YAML（只有 providers），验证默认值
    3. `TestLoad_EnvSubstitution` — YAML 中写 `${TEST_KEY}`，设置环境变量，验证替换
    4. `TestLoad_MissingProvider` — 不配 providers，验证返回错误
    5. `TestLoad_InvalidDefaultProvider` — default 指向不存在的 provider 名
    6. `TestLoad_FileNotFound` — 文件路径不存在
  - 辅助：在 `testdata/` 目录放测试用的 YAML 文件
  - **完成标准**：`go test ./internal/config/ -v` 全部 PASS

- [x] **T0.2.5** 创建 `configs/config.example.yaml`
  - 内容对应 spec.md §6 的完整 YAML，每个字段加中文注释
  - **完成标准**：`Load("configs/config.example.yaml")` 不报错（需设置相应环境变量或将 api_key 设为占位值）

---

### T0.3 统一数据结构

**预估**：1 小时 | **前置**：T0.1 | **产出**：models 包，供其他模块引用

- [x] **T0.3.1** 创建 `internal/models/message.go`
  - 文件：`internal/models/message.go`
  - 定义：
    ```go
    type Message struct {
        Role       string     `json:"role"`
        Content    string     `json:"content"`
        ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
        ToolCallID string     `json:"tool_call_id,omitempty"`
        Timestamp  time.Time  `json:"timestamp"`
    }

    type ToolCall struct {
        ID        string          `json:"id"`
        Name      string          `json:"name"`
        Arguments json.RawMessage `json:"arguments"`
    }

    type ToolResult struct {
        Content string `json:"content"`
        IsError bool   `json:"is_error,omitempty"`
    }

    type Usage struct {
        PromptTokens     int `json:"prompt_tokens"`
        CompletionTokens int `json:"completion_tokens"`
        TotalTokens      int `json:"total_tokens"`
    }
    ```
  - 提供工厂函数：`NewUserMessage(content)`, `NewAssistantMessage(content)`, `NewSystemMessage(content)`, `NewToolResultMessage(toolCallID, result)`

- [x] **T0.3.2** 创建 `internal/models/errors.go`
  - 文件：`internal/models/errors.go`
  - 定义：
    ```go
    type APIError struct {
        Code      string `json:"code"`
        Message   string `json:"message"`
        RequestID string `json:"request_id,omitempty"`
        HTTPStatus int   `json:"-"`
    }

    func (e *APIError) Error() string { ... }

    // 预定义错误码
    var (
        ErrInvalidRequest  = &APIError{Code: "invalid_request", HTTPStatus: 400}
        ErrUnauthorized    = &APIError{Code: "unauthorized", HTTPStatus: 401}
        ErrSessionNotFound = &APIError{Code: "session_not_found", HTTPStatus: 404}
        ErrRateLimited     = &APIError{Code: "rate_limited", HTTPStatus: 429}
        ErrInternal        = &APIError{Code: "internal_error", HTTPStatus: 500}
        ErrProviderError   = &APIError{Code: "provider_error", HTTPStatus: 502}
        ErrProviderTimeout = &APIError{Code: "provider_timeout", HTTPStatus: 504}
    )

    // 基于模板创建新错误（带具体 message）
    func NewAPIError(base *APIError, message string) *APIError { ... }
    ```

- [x] **T0.3.3** 创建 `internal/models/request.go`
  - 文件：`internal/models/request.go`
  - 定义：
    ```go
    type ChatRequest struct {
        SessionID string `json:"session_id"`
        Message   string `json:"message"`
        Stream    bool   `json:"stream"`
    }

    type ChatResponse struct {
        SessionID      string   `json:"session_id"`
        RequestID      string   `json:"request_id"`
        Message        Message  `json:"message"`
        Usage          *Usage   `json:"usage,omitempty"`
        ToolCallsCount int      `json:"tool_calls_count"`
    }

    type StreamChunk struct {
        Delta     string     `json:"delta,omitempty"`
        ToolCalls []ToolCall `json:"tool_calls,omitempty"`
        Done      bool       `json:"done"`
        Usage     *Usage     `json:"usage,omitempty"`
        Err       error      `json:"-"`
    }
    ```
  - **完成标准**：`go build ./internal/models/` 无错误

---

### T0.4 入口串联与 README

**预估**：1 小时 | **前置**：T0.2 | **产出**：可解析 flag、加载配置并退出的 main

- [x] **T0.4.1** 更新 `cmd/claw/main.go`
  - 使用 `flag` 包解析 `-config` 参数（默认 `configs/config.yaml`）和 `-version` 参数
  - 调用 `config.Load(configPath)` 加载配置
  - 初始化 `slog`（根据 `cfg.Log` 选择 JSON/Text handler、设置 level）
  - 打印 `slog.Info("claw started", "version", version, "port", cfg.Server.Port)` 后退出
  - **完成标准**：
    ```bash
    ./bin/claw -config configs/config.example.yaml
    # 输出类似 {"level":"INFO","msg":"claw started","version":"0.1.0","port":8080}
    ./bin/claw -version
    # 输出 claw 0.1.0
    ```

- [x] **T0.4.2** 编写 `README.md`
  - 内容：
    1. 项目简介（1 段话）
    2. 特性列表
    3. 快速开始：前置条件（Go 1.22+）→ 克隆 → 配置 → 构建 → 运行
    4. 配置说明（指向 config.example.yaml）
    5. 开发（make test / make lint）
    6. License 占位

- [x] **T0.4.3** 创建 `.golangci.yml`
  ```yaml
  linters:
    enable:
      - govet
      - errcheck
      - staticcheck
      - gosimple
      - ineffassign
      - unused
      - gofmt
      - goimports
      - gosec
      - misspell
  linters-settings:
    gosec:
      excludes:
        - G104  # 允许不检查某些错误（按需调整）
  ```
  **完成标准**：`make lint` 无报错（如未安装 golangci-lint 可跳过）

---

### Phase 0 验收检查

```bash
# 全部通过即表示 Phase 0 完成
go build ./...                              # ✅ 编译成功
go test ./internal/config/ -v               # ✅ 配置测试通过
./bin/claw -config configs/config.example.yaml  # ✅ 加载配置正常
./bin/claw -version                         # ✅ 输出版本号
```

---

## Phase 1：内核与单通道 MVP

### T1.1 Provider 接口定义

**预估**：30 分钟 | **前置**：T0.2, T0.3 | **产出**：`internal/llm/provider.go`

- [x] **T1.1.1** 创建 `internal/llm/provider.go`
  - 定义 `Provider` 接口：
    ```go
    type Provider interface {
        Name() string
        Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
        ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error)
    }
    ```
  - 定义 LLM 层专用的请求/响应结构（区别于 models 中的 HTTP 层结构）：
    ```go
    type ChatRequest struct {
        Model       string
        Messages    []models.Message
        Tools       []ToolSchema
        Temperature float64
        MaxTokens   int
    }

    type ChatResponse struct {
        Content      string
        ToolCalls    []models.ToolCall
        Usage        models.Usage
        FinishReason string
    }

    type StreamChunk struct {
        Delta     string
        ToolCalls []models.ToolCall
        Done      bool
        Usage     *models.Usage
        Err       error
    }

    type ToolSchema struct {
        Type     string      `json:"type"`      // "function"
        Function FunctionDef `json:"function"`
    }

    type FunctionDef struct {
        Name        string          `json:"name"`
        Description string          `json:"description"`
        Parameters  json.RawMessage `json:"parameters"`
    }
    ```
  - **完成标准**：`go build ./internal/llm/` 无错误

---

### T1.2 OpenAI-Compatible Provider

**预估**：4 小时 | **前置**：T1.1 | **产出**：可调用 OpenAI API 的 Provider 实现

- [x] **T1.2.1** 创建 `internal/llm/openai.go` — 结构体与构造函数
  - `OpenAIProvider` 结构体：持有 `http.Client`、`baseURL`、`apiKey`、`model`、`config`
  - `NewOpenAIProvider(cfg config.ProviderConfig) (*OpenAIProvider, error)`
  - 设置 `http.Client` 的 `Timeout` 为 `cfg.Timeout`

- [x] **T1.2.2** 实现 `Chat` 方法（非流式）
  - 组装 OpenAI `/chat/completions` 请求体：
    - `model`, `messages`（转为 OpenAI 格式）, `tools`, `temperature`, `max_tokens`
    - `stream: false`
  - POST 请求，读取响应体
  - 解析 JSON → 提取 `choices[0].message.content`、`tool_calls`、`usage`
  - 错误处理：
    - HTTP 429 → `ErrRateLimited`（附带 Retry-After 信息）
    - HTTP 4xx → `ErrInvalidRequest`（附带 API 返回的 error.message）
    - HTTP 5xx → `ErrProviderError`
    - 网络错误 / 超时 → `ErrProviderTimeout`

- [x] **T1.2.3** 实现 `ChatStream` 方法（流式）
  - 请求体加 `"stream": true, "stream_options": {"include_usage": true}`
  - 读取响应 body 为 `bufio.Scanner`，按行读取
  - 解析 SSE 格式：跳过空行和 `event:` 行，处理 `data:` 行
  - `data: [DONE]` 时关闭 channel
  - 每个 chunk 解析 `choices[0].delta` → 写入 `StreamChunk` channel
  - 最后一个 chunk 的 `usage` 字段传入
  - 错误时写入 `StreamChunk{Err: err}` 然后关闭 channel

- [x] **T1.2.4** 实现消息格式转换辅助函数
  - `func toOpenAIMessages(msgs []models.Message) []map[string]interface{}`
  - `func toOpenAITools(tools []ToolSchema) []map[string]interface{}`
  - 处理 role 映射、tool_calls 和 tool_call_id 的序列化

- [x] **T1.2.5** 创建 `internal/llm/openai_test.go`
  - 使用 `httptest.NewServer` mock OpenAI API
  - 测试用例：
    1. `TestChat_TextResponse` — 正常文本回复
    2. `TestChat_ToolCallResponse` — 回复包含 tool_calls
    3. `TestChat_Error429` — 返回 429，验证错误类型
    4. `TestChat_Error500` — 返回 500
    5. `TestChat_Timeout` — server 延迟响应，验证超时
    6. `TestChatStream_Normal` — 流式正常 chunk 序列
    7. `TestChatStream_WithToolCall` — 流式中包含工具调用
  - **完成标准**：`go test ./internal/llm/ -v` 全部 PASS

---

### T1.3 Token 计数

**预估**：1 小时 | **前置**：T0.3 | **产出**：token 估算与消息截断工具

- [x] **T1.3.1** 创建 `internal/llm/token.go`
  - 实现：
    ```go
    // 简单估算：英文 4 字符 ≈ 1 token，中文 1 字 ≈ 2 token
    func EstimateTokens(text string) int

    // 估算整个消息列表的 token 数（含 role 等 overhead）
    func EstimateMessagesTokens(messages []models.Message) int

    // 截断消息列表使其不超过 maxTokens
    // 规则：保留第一条 system message，从最早的非 system 消息开始移除
    // 以 user-assistant 对为单位移除
    func TruncateMessages(messages []models.Message, maxTokens int) []models.Message
    ```

- [x] **T1.3.2** 创建 `internal/llm/token_test.go`
  - 测试用例：
    1. `TestEstimateTokens_English` — 纯英文
    2. `TestEstimateTokens_Chinese` — 纯中文
    3. `TestEstimateTokens_Mixed` — 中英混合
    4. `TestTruncateMessages_NoTruncation` — token 未超限
    5. `TestTruncateMessages_KeepSystemPrompt` — 截断后 system 仍在
    6. `TestTruncateMessages_RemoveOldest` — 验证移除顺序
  - **完成标准**：`go test ./internal/llm/ -v -run Token` 全部 PASS

---

### T1.4 工具接口与注册表

**预估**：1 小时 | **前置**：T0.3 | **产出**：`internal/tools/registry.go`

- [x] **T1.4.1** 创建 `internal/tools/registry.go`
  - 定义 `Tool` 接口：
    ```go
    type Tool interface {
        Name() string
        Description() string
        Parameters() json.RawMessage  // JSON Schema
        Execute(ctx context.Context, params json.RawMessage) (models.ToolResult, error)
    }
    ```
  - 实现 `Registry`：
    ```go
    type Registry struct {
        tools map[string]Tool
    }

    func NewRegistry() *Registry
    func (r *Registry) Register(tool Tool) error     // 重名返回错误
    func (r *Registry) Get(name string) (Tool, bool)
    func (r *Registry) Execute(ctx context.Context, name string, params json.RawMessage) (models.ToolResult, error)
    func (r *Registry) List() []Tool
    func (r *Registry) Schemas() []llm.ToolSchema    // 生成 OpenAI function calling 格式
    ```

- [x] **T1.4.2** 创建 `internal/tools/registry_test.go`
  - 使用一个简单的 mock tool 测试注册、查找、重名、Execute
  - **完成标准**：`go test ./internal/tools/ -v -run Registry` 全部 PASS

---

### T1.5 基础工具实现

**预估**：3 小时 | **前置**：T1.4, T0.2 | **产出**：4 个可用的工具

- [x] **T1.5.1** 实现 `internal/tools/time.go` — `get_current_time`
  - 参数 schema：`{"type": "object", "properties": {"timezone": {"type": "string", "description": "IANA 时区，如 Asia/Shanghai"}}, "required": []}`
  - Execute：解析 timezone（默认 Local）→ `time.Now().In(loc).Format(time.RFC3339)`
  - 返回 `ToolResult{Content: "2026-03-06T18:30:00+08:00"}`

- [x] **T1.5.2** 实现 `internal/tools/file.go` — `read_file`
  - 构造函数接收 `workdir string` 和 `maxOutputChars int`
  - 参数 schema：`{"properties": {"path": {"type": "string"}}, "required": ["path"]}`
  - Execute：
    1. `filepath.Clean(path)` → `filepath.Join(workdir, cleaned)`
    2. 检查 `filepath.Rel(workdir, absPath)`，若以 `..` 开头 → 返回 `ToolResult{IsError: true, Content: "path outside sandbox"}`
    3. `os.ReadFile` → 截断至 `maxOutputChars` → 返回内容
    4. 文件不存在 → `ToolResult{IsError: true, Content: "file not found: ..."}`

- [x] **T1.5.3** 实现 `internal/tools/file.go` — `write_file`
  - 同一文件中再注册一个 tool
  - 参数 schema：`{"properties": {"path": {...}, "content": {"type": "string"}}, "required": ["path", "content"]}`
  - Execute：同样的路径沙箱检查 → `os.MkdirAll` 父目录 → `os.WriteFile` → 返回 "written N bytes"

- [x] **T1.5.4** 实现 `internal/tools/command.go` — `run_command`
  - 构造函数接收 `allowedCommands []string` 和 `timeout time.Duration`
  - 参数 schema：`{"properties": {"command": {"type": "string"}, "args": {"type": "array", "items": {"type": "string"}}}, "required": ["command"]}`
  - Execute：
    1. 检查 `command` 在 `allowedCommands` 中（精确匹配）
    2. 使用 `exec.CommandContext(ctx, command, args...)`，**不经过 shell**
    3. 合并 stdout + stderr → `cmd.CombinedOutput()`
    4. 截断至 maxOutputChars
    5. 返回输出文本，若 exit code != 0 → `ToolResult{IsError: true, Content: "exit code 1: ..."}`

- [x] **T1.5.5** 创建 `internal/tools/tools_test.go`
  - 测试用例：
    1. `TestTimeTool_Default` — 无 timezone 参数
    2. `TestTimeTool_WithTimezone` — 指定 `Asia/Shanghai`
    3. `TestReadFile_Normal` — 正常读取
    4. `TestReadFile_PathTraversal` — `../../etc/passwd` 被拒
    5. `TestReadFile_NotFound` — 文件不存在
    6. `TestReadFile_Truncation` — 大文件被截断
    7. `TestWriteFile_Normal` — 正常写入
    8. `TestWriteFile_PathTraversal` — 路径穿越被拒
    9. `TestRunCommand_Allowed` — 白名单命令
    10. `TestRunCommand_Denied` — 非白名单命令
    11. `TestRunCommand_Timeout` — 命令超时（用 `sleep` 测试）
  - 使用 `t.TempDir()` 做文件工具的沙箱目录
  - **完成标准**：`go test ./internal/tools/ -v` 全部 PASS

---

### T1.6 Agent 循环

**预估**：3 小时 | **前置**：T1.2, T1.4, T1.3 | **产出**：`internal/agent/agent.go`

- [x] **T1.6.1** 创建 `internal/agent/agent.go` — Agent 结构体
  ```go
  type Agent struct {
      provider      llm.Provider
      toolRegistry  *tools.Registry
      systemPrompt  string
      maxIterations int
      contextWindow int
  }

  func NewAgent(provider llm.Provider, registry *tools.Registry, opts AgentOptions) *Agent
  ```

- [x] **T1.6.2** 实现 `Run` 方法（非流式 Agent 循环）
  - 签名：`func (a *Agent) Run(ctx context.Context, session *Session, userMessage string) (string, error)`
  - 伪代码：
    1. 追加 `NewUserMessage(userMessage)` 到 session.Messages
    2. `iteration := 0`
    3. **LOOP**:
       - 若 `iteration >= maxIterations` → 返回错误
       - `msgs := a.buildContext(session)` （system prompt + TruncateMessages）
       - `resp, err := a.provider.Chat(ctx, &llm.ChatRequest{...})`
       - 若 err → 返回
       - 若 `len(resp.ToolCalls) > 0`：
         - 追加 assistant message（含 tool_calls）到 session
         - 逐个执行工具：`a.toolRegistry.Execute(ctx, tc.Name, tc.Arguments)`
         - 追加每个 tool result message 到 session
         - `iteration++` → goto LOOP
       - 否则：追加 assistant message 到 session → 返回 `resp.Content`

- [x] **T1.6.3** 实现 `buildContext` 辅助方法
  - 构建消息列表：system prompt message + session.Messages
  - 调用 `llm.TruncateMessages(messages, a.contextWindow)` 截断

- [x] **T1.6.4** 定义 Agent 层的 Session 结构
  ```go
  type Session struct {
      ID         string
      Channel    string
      Messages   []models.Message
      CreatedAt  time.Time
      UpdatedAt  time.Time
      mu         sync.Mutex
  }
  ```

- [x] **T1.6.5** 创建 `internal/agent/agent_test.go`
  - 定义 `mockProvider` 实现 `llm.Provider` 接口
  - 测试用例：
    1. `TestRun_SimpleTextResponse` — Provider 直接返回文本
    2. `TestRun_SingleToolCall` — 第一次返回 tool_call，第二次返回文本
    3. `TestRun_MultipleToolCalls` — 一次响应含多个 tool_call
    4. `TestRun_MaxIterationsExceeded` — Provider 每次都返回 tool_call，验证上限
    5. `TestRun_ToolExecutionError` — 工具执行失败，验证错误被传回 LLM
    6. `TestRun_ContextCancelled` — 中途取消 context
  - **完成标准**：`go test ./internal/agent/ -v` 全部 PASS

---

### T1.7 Session 排队

**预估**：1 小时 | **前置**：T1.6 | **产出**：`internal/agent/queue.go`

- [x] **T1.7.1** 创建 `internal/agent/queue.go`
  ```go
  type SessionQueue struct {
      locks sync.Map  // map[string]*sync.Mutex
  }

  func NewSessionQueue() *SessionQueue

  // Acquire 获取 session 锁，同一 session 串行
  func (q *SessionQueue) Acquire(sessionID string)

  // Release 释放 session 锁
  func (q *SessionQueue) Release(sessionID string)
  ```
  - 实现：`sync.Map` 存储 per-session 的 `*sync.Mutex`，`LoadOrStore` 保证并发安全

- [x] **T1.7.2** 编写测试
  - `TestSessionQueue_ConcurrentDifferentSessions` — 不同 session 可并发
  - `TestSessionQueue_SameSessionSerial` — 同 session 串行（用 goroutine + time 验证）
  - **完成标准**：`go test ./internal/agent/ -v -run Queue` 全部 PASS

---

### T1.8 Session 存储

**预估**：1.5 小时 | **前置**：T0.3 | **产出**：`internal/gateway/session.go`

- [x] **T1.8.1** 创建 `internal/gateway/session.go`
  - 定义接口：
    ```go
    type SessionStore interface {
        Get(id string) (*agent.Session, bool)
        GetOrCreate(id string, channel string) *agent.Session
        Delete(id string)
        List() []*agent.Session
        Count() int
    }
    ```
  - 实现 `MemorySessionStore`：
    ```go
    type MemorySessionStore struct {
        sessions sync.Map
        ttl      time.Duration
        maxHistory int
    }

    func NewMemorySessionStore(ttl time.Duration, maxHistory int, cleanupInterval time.Duration) *MemorySessionStore
    ```
  - 后台 goroutine（接收 `context.Context`，可取消）：
    - 每 `cleanupInterval` 扫描一次
    - 删除 `UpdatedAt + ttl < now` 的 session

- [x] **T1.8.2** 创建 `internal/gateway/session_test.go`
  - 测试用例：
    1. `TestGetOrCreate_New` — 新 session 创建
    2. `TestGetOrCreate_Existing` — 已有 session 返回同一个
    3. `TestDelete` — 删除后 Get 返回 false
    4. `TestTTLCleanup` — 设置短 TTL，等待后验证 session 被清理
    5. `TestConcurrentAccess` — 多 goroutine 并发 GetOrCreate
  - **完成标准**：`go test ./internal/gateway/ -v` 全部 PASS

---

### T1.9 Gateway 核心

**预估**：2 小时 | **前置**：T1.6, T1.7, T1.8 | **产出**：`internal/gateway/gateway.go`

- [x] **T1.9.1** 创建 `internal/gateway/gateway.go`
  - 添加 UUID 依赖：`go get github.com/google/uuid`
  - 实现：
    ```go
    type Gateway struct {
        agent        *agent.Agent
        sessions     SessionStore
        queue        *agent.SessionQueue
        config       *config.Config
    }

    func NewGateway(agent *agent.Agent, sessions SessionStore, queue *agent.SessionQueue, cfg *config.Config) *Gateway

    func (g *Gateway) HandleMessage(ctx context.Context, sessionID, channel, message string) (*models.ChatResponse, error) {
        // 1. 生成 request_id（uuid）
        // 2. 注入 request_id 到 ctx（用 context.WithValue）
        // 3. session = g.sessions.GetOrCreate(sessionID, channel)
        // 4. g.queue.Acquire(sessionID) / defer Release
        // 5. result, err = g.agents.Run(ctx, session, message)
        // 6. 构造 ChatResponse 返回
        // 7. slog 记录请求摘要（request_id, session_id, latency, token count）
    }

    func (g *Gateway) HandleMessageStream(ctx context.Context, sessionID, channel, message string) (<-chan models.StreamChunk, error) {
        // 类似，但调用 agents.RunStream
    }

    func (g *Gateway) GetSession(id string) (*agent.Session, bool)
    func (g *Gateway) DeleteSession(id string)
    func (g *Gateway) SessionCount() int
    ```

- [x] **T1.9.2** 定义 context key 类型
  - 文件：`internal/gateway/context.go`
  ```go
  type contextKey string

  const (
      ContextKeyRequestID contextKey = "request_id"
      ContextKeySessionID contextKey = "session_id"
  )

  func RequestIDFromContext(ctx context.Context) string
  ```

- [x] **T1.9.3** 编写 Gateway 测试
  - mock Agent 和 SessionStore
  - 测试 HandleMessage 的正常流程和错误传播
  - **完成标准**：`go test ./internal/gateway/ -v` 全部 PASS

---

### T1.10 HTTP Channel

**预估**：3 小时 | **前置**：T1.9 | **产出**：完整 HTTP API

- [x] **T1.10.1** 添加 chi 依赖
  ```bash
  go get github.com/go-chi/chi/v5
  ```

- [x] **T1.10.2** 创建 `internal/channels/http.go` — 路由与中间件
  ```go
  type HTTPChannel struct {
      gateway *gateway.Gateway
      server  *http.Server
      config  *config.Config
  }

  func NewHTTPChannel(gw *gateway.Gateway, cfg *config.Config) *HTTPChannel

  func (h *HTTPChannel) Router() chi.Router {
      r := chi.NewRouter()
      r.Use(h.requestIDMiddleware)
      r.Use(h.loggingMiddleware)
      r.Use(h.recoveryMiddleware)

      r.Post("/v1/chat", h.handleChat)
      r.Get("/v1/sessions/{id}", h.handleGetSession)
      r.Delete("/v1/sessions/{id}", h.handleDeleteSession)
      r.Get("/health", h.handleHealth)
      r.Get("/status", h.handleStatus)
      return r
  }

  func (h *HTTPChannel) Start(ctx context.Context) error    // 启动 HTTP server
  func (h *HTTPChannel) Stop(ctx context.Context) error     // graceful shutdown
  ```

- [x] **T1.10.3** 实现 `handleChat` 处理器
  - 解析 `models.ChatRequest` from body
  - 若 `session_id` 为空 → 生成 UUID
  - 若 `stream == false`：调用 `gateway.HandleMessage` → 返回 JSON
  - 若 `stream == true`：
    1. 设置 header：`Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
    2. 获取 `http.Flusher`
    3. 调用 `gateway.HandleMessageStream` 得到 channel
    4. 循环读取 channel → 格式化为 SSE（`event: chunk\ndata: {...}\n\n`）→ write + flush
    5. 最终发送 `event: done\ndata: {...}\n\n`

- [x] **T1.10.4** 实现其他处理器
  - `handleGetSession`：从 URL 取 `{id}` → `gateway.GetSession` → 返回 JSON
  - `handleDeleteSession`：`gateway.DeleteSession` → 204
  - `handleHealth`：返回 `{"status":"ok","version":"0.1.0"}`
  - `handleStatus`：返回活跃 session 数、工具列表等

- [x] **T1.10.5** 实现中间件
  - `requestIDMiddleware`：生成 UUID → 写入 context 和 `X-Request-ID` response header
  - `loggingMiddleware`：slog 记录 method、path、status、latency
  - `recoveryMiddleware`：recover panic → 500 + slog.Error

- [x] **T1.10.6** 实现统一 JSON 响应辅助函数
  ```go
  func writeJSON(w http.ResponseWriter, status int, data interface{})
  func writeError(w http.ResponseWriter, err *models.APIError)
  ```

- [x] **T1.10.7** 创建 `internal/channels/http_test.go`
  - 使用 `httptest.NewRecorder` + chi 路由器
  - 测试用例：
    1. `TestHandleChat_Sync` — POST /v1/chat 非流式
    2. `TestHandleChat_MissingMessage` — 缺少 message 字段 → 400
    3. `TestHandleHealth` — 返回 200 + status ok
    4. `TestHandleGetSession_NotFound` — 返回 404
    5. `TestHandleDeleteSession` — 返回 204
    6. `TestRequestIDMiddleware` — 验证 X-Request-ID header
  - **完成标准**：`go test ./internal/channels/ -v` 全部 PASS

---

### T1.11 Main 串联与集成

**预估**：2 小时 | **前置**：T1.1-T1.10 | **产出**：完整可运行的 MVP

- [x] **T1.11.1** 更新 `cmd/claw/main.go` — 完整启动流程
  ```go
  func main() {
      // 1. 解析 flag
      // 2. 加载 Config
      // 3. 初始化 slog
      // 4. 创建 OpenAI Provider
      // 5. 创建 Tool Registry，注册 4 个工具
      // 6. 创建 Agent
      // 7. 创建 MemorySessionStore
      // 8. 创建 SessionQueue
      // 9. 创建 Gateway
      // 10. 创建 HTTPChannel
      // 11. 启动 HTTP Server（goroutine）
      // 12. 监听 SIGINT/SIGTERM
      // 13. 收到信号 → httpChannel.Stop(ctx with shutdown_timeout)
      // 14. slog.Info("claw stopped")
  }
  ```

- [x] **T1.11.2** 创建冒烟测试脚本 `scripts/test_smoke.sh`
  ```bash
  #!/bin/bash
  set -e
  BASE="http://localhost:8080"

  echo "=== Health Check ==="
  curl -s "$BASE/health" | jq .

  echo "=== Simple Chat ==="
  curl -s -X POST "$BASE/v1/chat" \
    -H "Content-Type: application/json" \
    -d '{"message": "你好，请告诉我现在的时间"}' | jq .

  echo "=== Stream Chat ==="
  curl -N -X POST "$BASE/v1/chat" \
    -H "Content-Type: application/json" \
    -d '{"message": "Hello!", "stream": true}'

  echo ""
  echo "=== Status ==="
  curl -s "$BASE/status" | jq .
  ```

- [x] **T1.11.3** 手动端到端验证
  - 启动服务：`make run`
  - 执行 `scripts/test_smoke.sh`
  - 验证：
    - [x] /health 返回 200
    - [x] 简单对话能收到 LLM 回复
    - [x] 请求「当前时间」触发 tool_call
    - [x] 流式输出正常
    - [x] Ctrl+C 优雅关闭

- [x] **T1.11.4** 编写集成测试 `internal/integration_test.go`（可选，build tag `integration`）
  - 使用 mock Provider 启动完整 HTTP server
  - 发送请求验证端到端流程
  - **完成标准**：`go test -tags integration ./... -v` 通过

---

### Phase 1 验收检查

```bash
go build ./...                                          # ✅ 编译成功
go test ./... -race -count=1                             # ✅ 全部通过
curl -s localhost:8080/health | jq .status               # ✅ "ok"
curl -s -X POST localhost:8080/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message":"现在几点？"}' | jq .message.content    # ✅ 包含时间
curl -s -X POST localhost:8080/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message":"读取 /tmp/test.txt"}' | jq .           # ✅ 工具调用成功
# 路径穿越被拒绝                                          # ✅
# 非白名单命令被拒绝                                       # ✅
# Ctrl+C 优雅关闭                                         # ✅
```

---

## Phase 2：多通道与多模型

### T2.1 多 Provider 管理

**预估**：2 小时 | **前置**：Phase 1 | **产出**：`internal/llm/manager.go`

- [x] **T2.1.1** 创建 `internal/llm/manager.go`
  ```go
  type ProviderManager struct {
      providers     map[string]Provider
      fallbackOrder []string
      retry         config.RetryConfig
  }

  func NewProviderManager(providers map[string]Provider, fallbackOrder []string, retry config.RetryConfig) *ProviderManager
  ```
  - `Chat` / `ChatStream`：按 fallbackOrder 尝试，可重试的错误才切换
  - 判定可重试：网络错误、5xx、超时。4xx 不切换。
  - 429 读取 Retry-After → `time.Sleep` → 重试同一 Provider

- [x] **T2.1.2** 让 `ProviderManager` 实现 `Provider` 接口
  - Agent 无需修改，只需在 main 中传入 ProviderManager 替代单个 Provider

- [x] **T2.1.3** 编写测试
  - mock 两个 Provider，主 Provider 返回错误，验证自动切到备用
  - **完成标准**：测试通过

---

### T2.2 Anthropic Provider（可选）

**预估**：3 小时 | **前置**：T1.1

- [x] **T2.2.1** 创建 `internal/llm/anthropic.go`
  - Anthropic Messages API 与 OpenAI 的差异：
    - endpoint: `/v1/messages`
    - header: `x-api-key` 和 `anthropic-version`
    - system message 单独字段（不在 messages 数组中）
    - tool_use / tool_result 的格式不同
  - 实现 Chat 和 ChatStream
- [x] **T2.2.2** 编写测试

---

### T2.3 Channel 接口抽象

**预估**：1 小时 | **前置**：Phase 1

- [x] **T2.3.1** 创建 `internal/channels/channel.go`
  ```go
  type Channel interface {
      Name() string
      Start(ctx context.Context) error
      Stop(ctx context.Context) error
  }
  ```
- [x] **T2.3.2** 重构 `HTTPChannel` 实现该接口
- [x] **T2.3.3** 更新 `main.go`：统一 Channel 管理（遍历启动/关闭）

---

### T2.4 WebSocket Channel

**预估**：3 小时 | **前置**：T2.3

- [x] **T2.4.1** 添加依赖：`go get github.com/coder/websocket`
- [x] **T2.4.2** 创建 `internal/channels/websocket.go`
  - `GET /v1/ws?session_id=xxx` → 升级连接
  - 读循环：解析 JSON 消息 → 调用 Gateway
  - 写循环：从 stream channel 读取 → 发送 JSON
  - 心跳：定时 ping，超时关闭
- [x] **T2.4.3** 编写测试

---

### T2.5 飞书 Channel

**预估**：3 小时 | **前置**：T2.3

- [x] **T2.5.1** 添加依赖：`go get github.com/larksuite/oapi-sdk-go/v3`
- [x] **T2.5.2** 创建 `internal/channels/feishu.go`
  - 通过飞书开放平台事件订阅接收消息
  - chat_id 作为 session_id
  - 使用飞书 SDK 发送回复
  - 长消息分段发送
- [x] **T2.5.3** 编写测试

---

### T2.6 Web Search 工具

**预估**：2 小时 | **前置**：T1.4

- [x] **T2.6.1** 创建 `internal/tools/search.go`
  - 对接 Tavily API（或 SearXNG）
  - 参数：`query`, `num_results`
  - 返回：标题 + 摘要 + URL
- [x] **T2.6.2** 编写测试

---

### T2.7 Memory 工具

**预估**：1.5 小时 | **前置**：T1.4

- [x] **T2.7.1** 创建 `internal/tools/memory.go`
  - `memory_get`、`memory_set`、`memory_list`
  - 按 session_id 做 namespace 隔离
  - 内存 map 存储
- [x] **T2.7.2** 编写测试

---

### T2.8 工具权限 Profile

**预估**：1 小时 | **前置**：T1.4

- [x] **T2.8.1** 扩展 `config.ToolsConfig`，增加 profiles 和 default_profile
- [x] **T2.8.2** Registry 增加 `FilterByProfile(profile string) []ToolSchema` 方法
- [x] **T2.8.3** Gateway 按 profile 过滤可用工具

---

### T2.9 技能系统

**预估**：2 小时 | **前置**：T1.4

- [x] **T2.9.1** 创建 `internal/skills/loader.go`
  - 扫描技能目录 → 解析 skill.yaml → 读取 instructions.md
  - 验证引用的 tools 已注册
  - 返回拼接后的 system prompt 补充内容
- [x] **T2.9.2** 编写测试（用 testdata 目录模拟技能）

---

### Phase 2 验收检查

```bash
# WebSocket 或飞书通道可用
# Provider 故障转移正常（模拟主 Provider 故障）
# 技能加载后 system prompt 包含 instructions 内容
```

---

## Phase 3：安全与运维

### T3.1 API Key 鉴权

**预估**：1 小时 | **前置**：Phase 1

- [x] **T3.1.1** 创建 `internal/channels/middleware.go` — `AuthMiddleware`
  - 检查 `Authorization: Bearer <key>` 或 `X-API-Key` header
  - `crypto/subtle.ConstantTimeCompare` 比对
  - `auth.enabled: false` 时跳过
- [x] **T3.1.2** 编写测试

---

### T3.2 飞书签名校验

**预估**：30 分钟 | **前置**：T2.5

- [x] **T3.2.1** 校验飞书事件订阅的请求签名（Verification Token / Encrypt Key）

---

### T3.3 速率限制

**预估**：1.5 小时 | **前置**：Phase 1

- [x] **T3.3.1** 实现令牌桶限流中间件
  - 可用 `golang.org/x/time/rate`
  - 按 IP 或 API Key 分桶
  - 超限 → 429 + Retry-After header
- [x] **T3.3.2** 编写测试

---

### T3.4 审计日志

**预估**：1.5 小时 | **前置**：Phase 1

- [x] **T3.4.1** 定义审计事件类型与 logger
  - 独立的 slog handler 输出到审计文件
  - 事件：tool_executed、file_written、command_run、auth_failed
- [x] **T3.4.2** 在工具执行和鉴权失败时写入审计日志
- [x] **T3.4.3** 敏感内容脱敏（API Key 前 4 位，长内容截断）

---

### T3.5 SQLite 持久化（可选）

**预估**：3 小时 | **前置**：T1.8

- [x] **T3.5.1** 添加依赖：`go get modernc.org/sqlite`
- [x] **T3.5.2** 创建 `internal/gateway/session_sqlite.go`
  - 实现 `SessionStore` 接口
  - 自动建表
  - TTL 清理
- [x] **T3.5.3** 编写测试
- [x] **T3.5.4** `memory_*` 工具复用 SQLite 持久化，跨重启保留按 session 隔离的记忆

---

### T3.6 Docker 化

**预估**：1 小时 | **前置**：Phase 1

- [x] **T3.6.1** 创建 `Dockerfile`（多阶段构建）
  ```dockerfile
  FROM golang:1.22-alpine AS builder
  WORKDIR /app
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o claw ./cmd/claw

  FROM alpine:3.19
  RUN apk add --no-cache ca-certificates tzdata
  COPY --from=builder /app/claw /usr/local/bin/claw
  EXPOSE 8080
  ENTRYPOINT ["claw"]
  CMD ["-config", "/etc/claw/config.yaml"]
  ```

- [x] **T3.6.2** 创建 `docker-compose.yml`
  ```yaml
  services:
    claw:
      build: .
      ports:
        - "8080:8080"
      volumes:
        - ./configs/config.yaml:/etc/claw/config.yaml:ro
      environment:
        - OPENAI_API_KEY=${OPENAI_API_KEY}
  ```

- [x] **T3.6.3** 创建 `deploy/claw.service`（systemd unit 文件）
- [ ] **T3.6.4** 验证：`docker build -t claw . && docker run --rm claw -version`
  - 当前代码与配置文件已就绪，是否通过取决于本地 Docker daemon 可用性

---

### T3.7 Prometheus Metrics（可选）

**预估**：2 小时 | **前置**：Phase 1

- [x] **T3.7.1** 添加依赖：`go get github.com/prometheus/client_golang`
- [x] **T3.7.2** 定义指标并在各层埋点
- [x] **T3.7.3** 暴露 `GET /metrics` endpoint

---

### T3.8 Readiness 检查

**预估**：30 分钟 | **前置**：Phase 1

- [x] **T3.8.1** 实现 `GET /ready`
  - 当前实现提供进程级 readiness endpoint
  - 200 = ready，关闭过程中切换为 not ready

---

### Phase 3 验收检查

```bash
docker build -t claw .                          # ✅ 构建成功
docker run --rm claw -version                    # ✅ 输出版本
# 无 API Key 请求 → 401                           # ✅
# 审计日志含工具调用记录                              # ✅
curl localhost:8080/metrics                      # ✅ Prometheus 格式
curl localhost:8080/ready                        # ✅ 200 或 503
```

---

## 任务进度总览

| 任务 ID | 描述 | 预估 | 状态 |
|---------|------|------|------|
| T0.1 | 初始化项目结构 | 30m | ✅ |
| T0.2 | 配置模块 | 2h | ✅ |
| T0.3 | 统一数据结构 | 1h | ✅ |
| T0.4 | 入口串联与 README | 1h | ✅ |
| T1.1 | Provider 接口定义 | 30m | ✅ |
| T1.2 | OpenAI Provider 实现 | 4h | ✅ |
| T1.3 | Token 计数 | 1h | ✅ |
| T1.4 | 工具接口与注册表 | 1h | ✅ |
| T1.5 | 基础工具实现 | 3h | ✅ |
| T1.6 | Agent 循环 | 3h | ✅ |
| T1.7 | Session 排队 | 1h | ✅ |
| T1.8 | Session 存储 | 1.5h | ✅ |
| T1.9 | Gateway 核心 | 2h | ✅ |
| T1.10 | HTTP Channel | 3h | ✅ |
| T1.11 | Main 串联与集成 | 2h | ✅ |
| T2.1 | 多 Provider 管理 | 2h | ✅ |
| T2.2 | Anthropic Provider | 3h | ✅ |
| T2.3 | Channel 接口抽象 | 1h | ✅ |
| T2.4 | WebSocket Channel | 3h | ✅ |
| T2.5 | 飞书 Channel | 3h | ✅ |
| T2.6 | Web Search 工具 | 2h | ⬜ |
| T2.7 | Memory 工具 | 1.5h | ⬜ |
| T2.8 | 工具权限 Profile | 1h | ⬜ |
| T2.9 | 技能系统 | 2h | ⬜ |
| T3.1 | API Key 鉴权 | 1h | ⬜ |
| T3.2 | 飞书签名校验 | 30m | ⬜ |
| T3.3 | 速率限制 | 1.5h | ⬜ |
| T3.4 | 审计日志 | 1.5h | ⬜ |
| T3.5 | SQLite 持久化 | 3h | ⬜ |
| T3.6 | Docker 化 | 1h | ⬜ |
| T3.7 | Prometheus Metrics | 2h | ⬜ |
| T3.8 | Readiness 检查 | 30m | ⬜ |

**总计预估**：~52 小时

| Phase | 预估 | 任务数 |
|-------|------|--------|
| Phase 0 | ~4.5h | 4 任务（13 子任务）|
| Phase 1 | ~21.5h | 11 任务（30 子任务）|
| Phase 2 | ~18.5h | 9 任务（15 子任务）|
| Phase 3 | ~11h | 8 任务（14 子任务）|

---

*任务清单版本：v0.1 | 与 plan.md v0.2、spec.md v0.2 对应*
