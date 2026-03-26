# Claw

Claw 是一个用 Go 语言实现的个人 AI 助手。本地优先、单二进制部署，支持多通道接入（HTTP、WebSocket、飞书）和多 LLM Provider 切换，通过可配置的工具系统扩展 Agent 能力。

## 特性

- **本地优先** — 核心逻辑与数据可在本机运行，隐私可控
- **模型无关** — 支持 OpenAI-compatible、Anthropic 等多种 LLM Provider
- **多通道** — HTTP、WebSocket、飞书 Webhook、飞书长连接
- **工具系统** — 内置文件读写、命令执行、时间查询、记忆、搜索工具，沙箱隔离
- **技能系统** — 通过 `skills/` 目录加载自定义技能，将 instructions 注入系统提示
- **持久化** — 会话历史与 `memory_*` 在配置 SQLite 后可跨重启保留
- **流式响应** — SSE / WebSocket 流式输出，支持流式 tool call 循环，降低首字等待时间
- **飞书增强** — Markdown 富文本回复、思考中提示、非文本消息友好提示
- **可观测** — 提供 `/health`、`/ready`、`/metrics`
- **配置即行为** — 通过 YAML 配置控制人设、工具权限、模型选择，temperature/max_tokens 直达 LLM
- **单二进制** — Go 编译，部署简单，无运行时依赖

## 快速开始

### 前置条件

- Go 1.22+

### 构建与运行

```bash
# 克隆项目
git clone https://github.com/YumingHuang/claw.git
cd claw

# 复制并编辑配置
cp configs/config.example.yaml configs/config.yaml
# 编辑 configs/config.yaml，填入你的 API Key

# 或通过环境变量设置
export OPENAI_API_KEY="your-api-key"

# 如需持久化 session 和 memory_*，建议配置 SQLite 路径
# session:
#   sqlite_path: "/absolute/path/to/claw.sqlite"

# 构建
make build

# 运行
./bin/claw -config configs/config.yaml

# 或直接
make run
```

### 验证

```bash
# 健康检查
curl http://localhost:8080/health

# 就绪检查
curl http://localhost:8080/ready

# Prometheus 指标
curl http://localhost:8080/metrics

# 发送消息
curl -X POST http://localhost:8080/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "你好"}'
```

## 配置

完整配置示例见 [`configs/config.example.yaml`](configs/config.example.yaml)，支持 `${ENV_VAR}` 语法引用环境变量。

几个关键配置项：

- `system_prompt`：定义 AI 助手的行为和人设，直接影响回复质量
- `session.sqlite_path`：配置后会同时持久化会话历史和 `memory_*` 工具记忆
- `providers.list[].temperature` / `max_tokens`：直接传递给 LLM，控制回复风格和长度
- `tools.default_profile` / `tools.profiles`：控制不同入口可见的工具集合
- `channels.feishu.long_connection`：`true` 时通过飞书长连接收消息，不需要回调地址；`false` 时使用 `/v1/feishu/webhook`
- `auth` 与 `rate_limit`：分别控制 API Key 鉴权和限流

## 开发

```bash
make test       # 运行测试（含 race 检测）
make lint       # 代码静态检查（需安装 golangci-lint）
make coverage   # 生成测试覆盖率报告
make clean      # 清理构建产物
```

## 部署

```bash
docker build -t claw .
docker compose up --build
```

`deploy/claw.service` 提供了 systemd unit 示例，可用于 Linux 主机常驻运行。

## 飞书

飞书支持两种接入方式：

- Webhook 模式：配置事件订阅地址为 `/v1/feishu/webhook`
- 长连接模式：设置 `channels.feishu.long_connection: true`，无需公网回调地址

功能特性：

- 处理 `im.message.receive_v1` 文本消息事件
- 非文本消息（图片、文件等）会收到友好提示
- 收到消息后自动发送"🤔 思考中..."提示
- 回复支持 Markdown 富文本（通过飞书卡片消息），失败时自动降级为纯文本
- 长消息自动分段发送
- 事件去重带 TTL 自动清理，防止内存泄漏

## 项目结构

```
cmd/claw/           程序入口
internal/
  config/           配置加载与校验
  models/           内部数据结构
  llm/              LLM Provider 抽象与实现
  tools/            工具接口与实现
  agent/            Agent 循环与排队
  gateway/          会话管理与协调
  channels/         通道适配器（HTTP/WebSocket/Feishu）
  skills/           技能加载器
  metrics/          Prometheus 指标
  audit/            审计日志
  requestctx/       请求上下文工具
skills/             自定义技能目录（可选）
```

## License

MIT
