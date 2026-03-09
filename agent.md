# Claw — Agent 开发规范

> 本文件为 AI Agent（如 Cursor、Copilot）在本项目中的行为规范。
> 所有代码生成、重构、审查均应遵循以下约定。

---

## 1. 项目概要

- **项目名称**：Claw — Go 版个人 AI 助手
- **语言**：Go 1.22+
- **架构**：多入口、单内核分层架构（入口层 → 控制面 → 执行面 → 能力层 → 数据层）
- **核心文档**：`spec.md`（产品与技术规格）、`plan.md`（实施计划）、`task.md`（可执行任务清单）
- **模块入口**：`github.com/mingminliu/claw`

---

## 2. 目录结构约定

```
claw/
├── cmd/claw/main.go          # 唯一入口，依赖注入组装点
├── internal/
│   ├── config/                # 配置加载与校验
│   ├── gateway/               # 控制面：路由、会话管理、Gateway
│   ├── agent/                 # 执行面：Agent 循环、排队
│   ├── llm/                   # 能力层：Provider 抽象与实现
│   ├── tools/                 # 能力层：工具接口、注册表、实现
│   ├── channels/              # 入口层：HTTP/WebSocket/Feishu 适配器
│   └── models/                # 内部数据结构（Message、Error、Request）
├── configs/                   # 配置文件示例
├── scripts/                   # 脚本（冒烟测试等）
├── spec.md / plan.md / task.md
└── go.mod / go.sum
```

**规则**：
- 所有业务代码放在 `internal/` 下，禁止对外暴露
- 每个 `internal/` 子包职责单一，禁止跨层直接调用（入口层不直接调用能力层）
- `models/` 为共享数据结构包，各层均可引用
- 新增子包前需确认归属层级，不得随意创建顶层包

---

## 3. 代码风格与约定

### 3.1 通用规则

- 遵循 `gofmt` / `goimports` 格式化
- 变量和函数命名使用 camelCase，导出标识符使用 PascalCase
- 错误消息小写开头，不带句号：`"provider not found"` 而非 `"Provider not found."`
- 不使用 `init()` 函数，不使用全局可变状态
- 所有依赖通过构造函数注入（显式优于隐式）
- 构造函数命名为 `New<Type>(...)` ，返回具体类型或接口

### 3.2 错误处理

- 使用 `fmt.Errorf("operation: %w", err)` 包装错误，保留错误链
- 对外暴露的错误使用 `models.APIError`（含 HTTP 状态码和错误码）
- 不吞掉错误：要么处理，要么向上传播
- 可恢复错误返回 `error`，不可恢复的配置缺失在启动阶段 `panic`（快速失败）

### 3.3 并发

- 所有 goroutine 必须可通过 `context.Context` 取消
- 禁止裸 goroutine（必须有退出机制和 panic recovery）
- 使用 `sync.Map` 或带 `sync.Mutex` 的 map，禁止无保护的并发 map 访问
- 按 session 串行处理请求（`agent.SessionQueue`），不同 session 可并发

### 3.4 日志

- 统一使用 `log/slog`，禁止 `fmt.Println` 或 `log.Printf`
- 每条日志必须包含结构化字段，优先使用 `slog.With` 注入 `request_id`、`session_id`
- 日志级别：
  - `Debug`：详细调试信息（请求体、LLM 响应体）
  - `Info`：正常操作（请求开始/结束、session 创建/销毁）
  - `Warn`：可恢复异常（重试、降级）
  - `Error`：不可恢复错误（Provider 全部失败、工具执行崩溃）

### 3.5 注释

- 所有导出的类型、函数、方法必须有 GoDoc 注释
- 非导出代码仅在逻辑不直观处添加注释，解释 **why** 而非 **what**
- 禁止注释掉代码提交，删除不需要的代码
- 禁止在注释中写 TODO 而不附带 issue 编号或具体计划

---

## 4. 接口与抽象

### 4.1 核心接口（已定义，不可随意修改签名）

| 接口 | 包 | 关键方法 |
|------|----|----------|
| `llm.Provider` | `internal/llm` | `Name()`, `Chat(ctx, *ChatRequest)`, `ChatStream(ctx, *ChatRequest)` |
| `tools.Tool` | `internal/tools` | `Name()`, `Description()`, `Parameters()`, `Execute(ctx, json.RawMessage)` |
| `gateway.SessionStore` | `internal/gateway` | `Get(id)`, `GetOrCreate(id, channel)`, `Delete(id)`, `List()` |
| `channels.Channel` | `internal/channels` | `Name()`, `Start(ctx)`, `Stop(ctx)` |

### 4.2 新增接口规则

- 接口定义在**消费方**所在的包中（Go 惯例），而非实现方
- 接口应尽可能小（1-3 个方法），遵循接口隔离原则
- 不为只有一个实现的类型过早抽象接口（除非用于测试 mock）

---

## 5. 配置约定

- 配置文件格式：YAML（`gopkg.in/yaml.v3`）
- 配置结构体定义在 `internal/config/config.go`
- 支持 `${ENV_VAR}` 语法引用环境变量
- 所有可选字段必须提供合理默认值（在 `setDefaults()` 中设置）
- 必填字段在 `validate()` 中校验，缺失时返回清晰的错误消息
- 敏感信息（API Key、Token）仅通过环境变量注入，禁止写死在代码或配置文件中
- 配置 schema 变更时同步更新 `configs/config.example.yaml`

---

## 6. 测试规范

### 6.1 测试文件命名

- 单元测试：`<file>_test.go`，与被测文件同目录
- 测试数据：放在对应包的 `testdata/` 目录下
- 集成测试：使用 `//go:build integration` 标签隔离

### 6.2 测试编写规则

- 使用 `testing` 标准库 + `github.com/stretchr/testify`（assert / require）
- 测试函数命名：`Test<Function>_<Scenario>`，如 `TestLoad_MissingProvider`
- 表驱动测试优先于多个独立测试函数
- Mock 外部依赖（HTTP 调用用 `httptest`，Provider 用接口 mock）
- 文件系统操作使用 `t.TempDir()`，禁止操作真实系统路径
- 所有测试必须可并行运行（`t.Parallel()`），除非有特殊原因
- 测试必须通过 `-race` 标志：`go test ./... -race`

### 6.3 覆盖率要求

- 核心模块（`agent`、`llm`、`tools`、`config`）：≥ 70%
- 整体项目：≥ 50%

---

## 7. 安全约定

### 7.1 工具安全

- `run_command` 使用 `exec.CommandContext`，**不经过 shell**（禁止 `sh -c`）
- 命令白名单精确匹配，不支持通配符
- 文件操作限制在 `tools.workdir` 沙箱内
- 路径安全检查：`filepath.Clean` → `filepath.Join(workdir, path)` → `filepath.Rel` 验证不含 `..`
- 工具输出截断至 `max_output_chars`（默认 10000）
- 每个工具调用有独立超时（默认 30s）

### 7.2 认证安全

- API Key 比对使用 `crypto/subtle.ConstantTimeCompare`，防止时序攻击
- 日志中 API Key 仅显示前 4 位，其余以 `****` 替代
- 禁止在错误消息中暴露内部实现细节

---

## 8. 依赖管理

### 8.1 已批准的外部依赖

| 用途 | 依赖 |
|------|------|
| HTTP 路由 | `github.com/go-chi/chi/v5` |
| WebSocket | `github.com/coder/websocket` |
| YAML | `gopkg.in/yaml.v3` |
| 测试 | `github.com/stretchr/testify` |
| UUID | `github.com/google/uuid` |
| Feishu | `github.com/larksuite/oapi-sdk-go/v3` |
| SQLite | `modernc.org/sqlite` |
| Metrics | `github.com/prometheus/client_golang` |

### 8.2 依赖引入规则

- 优先使用标准库（`net/http`、`log/slog`、`encoding/json`、`sync`）
- 引入新的第三方依赖前需评估：是否有标准库替代、维护状态、许可证兼容性
- 禁止引入 CGO 依赖（保持纯 Go 编译，单二进制部署）
- 使用 `go get <package>@latest` 添加依赖，不手动编辑 `go.mod` 版本号

---

## 9. Git 与提交规范

### 9.1 提交消息格式

```
<type>: <description>

[optional body]
```

**type 取值**：
- `init` — 项目初始化
- `feat` — 新功能
- `fix` — 修复 Bug
- `refactor` — 重构（不改变外部行为）
- `test` — 测试
- `docs` — 文档
- `chore` — 构建、CI、依赖等杂项

示例：
```
feat: implement OpenAI provider with streaming support
fix: prevent path traversal in read_file tool
test: add agent loop tests with mock provider
```

### 9.2 分支规则

- 主分支：`main`
- 功能分支：`feat/<short-description>`
- 修复分支：`fix/<short-description>`

---

## 10. 构建与运行

```bash
make build          # 编译到 bin/claw
make run            # 以 configs/config.yaml 启动
make test           # 运行全部测试（含 -race）
make lint           # golangci-lint 检查
make coverage       # 生成覆盖率报告
```

**环境变量**（运行时必需）：
- `OPENAI_API_KEY` — 默认 Provider 的 API Key
- 可选：`CLAW_SERVER_PORT`、`CLAW_LOG_LEVEL` 等覆盖配置项

---

## 11. 任务执行指引

### 11.1 实现新功能时

1. 查阅 `spec.md` 确认功能规格
2. 查阅 `plan.md` 确认所属阶段和依赖关系
3. 查阅 `task.md` 找到对应子任务的详细要求
4. 按任务描述实现，包括单元测试
5. 确保 `go build ./...` 和 `go test ./... -race` 通过

### 11.2 修改现有代码时

1. 先阅读相关文件，理解当前实现
2. 修改时保持接口兼容，除非明确要求破坏性变更
3. 更新受影响的测试用例
4. 若涉及配置变更，同步更新 `configs/config.example.yaml`

### 11.3 添加新工具时

1. 在 `internal/tools/` 下创建实现文件
2. 实现 `tools.Tool` 接口的四个方法
3. 在 `Parameters()` 中返回符合 JSON Schema 的参数定义
4. 在 `Execute()` 中实现逻辑，遵循安全约定（超时、输出截断）
5. 在 `cmd/claw/main.go` 中注册工具
6. 编写完整测试（正常路径 + 安全边界）

### 11.4 添加新 Provider 时

1. 在 `internal/llm/` 下创建实现文件
2. 实现 `llm.Provider` 接口的三个方法
3. 处理消息格式转换（内部格式 ↔ 提供商 API 格式）
4. 实现流式响应解析（SSE → `StreamChunk` channel）
5. HTTP 错误码映射到 `models.APIError`
6. 使用 `httptest` 编写 mock 测试

### 11.5 添加新 Channel 时

1. 在 `internal/channels/` 下创建实现文件
2. 实现 `channels.Channel` 接口
3. 将外部消息转为 `gateway.HandleMessage` / `HandleMessageStream` 调用
4. 在 `cmd/claw/main.go` 中注册并管理生命周期

---

## 12. 关键设计决策（不可违背）

| 决策 | 理由 |
|------|------|
| 不使用 `init()` 和全局可变状态 | 显式依赖注入，便于测试 |
| `cmd/claw/main.go` 是唯一组装点 | 所有依赖在此创建并注入，内部包不做自初始化 |
| 工具默认全部关闭，需显式注册 | 最小权限原则 |
| `run_command` 不使用 shell 执行 | 避免命令注入漏洞 |
| Provider 接口返回 channel 实现流式 | 与 Go 并发模型契合，消费方自行控制节奏 |
| Session 按 ID 串行处理 | 避免消息乱序和上下文污染 |
| Token 估算用简单算法（非 tiktoken） | 第一期简化实现，保持零外部依赖 |
| 优雅关闭有超时上限 | 防止资源泄漏和进程悬挂 |

---

*规范版本：v0.1 | 与 spec.md v0.2、plan.md v0.2、task.md v0.1 对应*
