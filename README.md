# Claw

Claw 是一个用 Go 语言实现的个人 AI 助手。本地优先、单二进制部署，支持多通道接入（HTTP、WebSocket、飞书）和多 LLM Provider 切换，通过可配置的工具系统和 MCP 协议扩展 Agent 能力。

## 特性

- **本地优先** — 核心逻辑与数据可在本机运行，隐私可控
- **模型无关** — 支持 OpenAI-compatible、Anthropic 等多种 LLM Provider
- **多通道** — HTTP、WebSocket、飞书 Webhook、飞书长连接
- **工具系统** — 内置文件读写、命令执行、时间查询、记忆、搜索工具，沙箱隔离
- **MCP 支持** — 通过 Model Context Protocol 接入外部工具（浏览器控制、数据库等），自动注册到 Agent
- **定时任务** — 内置 Cron 调度器，定时触发 Agent 执行任务并推送结果到飞书
- **技能系统** — 通过 `skills/` 目录加载自定义技能，将 instructions 注入系统提示
- **持久化** — 会话历史与 `memory_*` 在配置 SQLite 后可跨重启保留
- **流式响应** — SSE / WebSocket 流式输出，支持流式 tool call 循环，降低首字等待时间
- **飞书增强** — 消息回复模式、思考中 Emoji Reaction（可配置）、Markdown 卡片、长消息分段
- **可观测** — 提供 `/health`、`/ready`、`/metrics`
- **配置即行为** — 一个 YAML 控制人设、工具权限、模型、MCP、定时任务、通道
- **单二进制** — Go 编译，部署简单，无运行时依赖

## 快速开始

### 前置条件

- Go 1.22+
- Node.js 22+（仅 MCP 浏览器工具需要，可选）

### 构建与运行

```bash
# 克隆项目
git clone https://github.com/YumingHuang/claw.git
cd claw

# 复制并编辑配置
cp configs/config.example.yaml configs/config.yaml
# 编辑 configs/config.yaml，填入你的 API Key

# 构建
make build

# 运行
./bin/claw -config configs/config.yaml
```

### 验证

```bash
# 健康检查
curl http://localhost:8080/health

# 发送消息
curl -X POST http://localhost:8080/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "你好"}'
```

## 配置

完整配置示例见 [`configs/config.example.yaml`](configs/config.example.yaml)，支持 `${ENV_VAR}` 语法引用环境变量。

配置文件按功能分区：

| 分区 | 说明 |
|---|---|
| `server` | 监听地址、端口、超时 |
| `system_prompt` | AI 助手的行为和人设 |
| `session` | 会话 TTL、历史条数、SQLite 持久化 |
| `providers` | LLM Provider 列表、故障转移、重试 |
| `tools` | 内置工具配置、命令白名单、Tavily 搜索 |
| `mcp` | MCP 外部工具服务器（stdio / sse） |
| `cron` | 定时任务，支持推送到飞书 |
| `channels` | 通道配置（HTTP / WebSocket / 飞书） |
| `auth` / `rate_limit` | API Key 鉴权和限流 |

## MCP 工具

通过 [Model Context Protocol](https://modelcontextprotocol.io/) 接入外部工具服务器，MCP 工具会自动注册到 Agent 的工具列表中。

```yaml
mcp:
  servers:
    # 浏览器控制（需要 Node.js）
    - name: "browser"
      type: "stdio"
      command: "npx"
      args: ["-y", "@playwright/mcp@latest"]

    # 文件系统
    - name: "filesystem"
      type: "stdio"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/your/workspace"]

    # 远程 SSE 服务器
    - name: "my-server"
      type: "sse"
      url: "http://localhost:3000/sse"
```

支持 `stdio`（本地进程）和 `sse`（远程 HTTP）两种传输方式。

## 定时任务

内置 Cron 调度器，按 cron 表达式定时向 Agent 发送消息，执行结果可推送到飞书。

```yaml
cron:
  jobs:
    - name: "daily_stock"
      schedule: "0 18 * * 1-5"          # 每个工作日 18:00
      message: "帮我分析一下今天的 A 股大盘走势"
      notify: "feishu:oc_xxxxxxxxxxxxx"  # 推送到飞书群
```

`notify` 格式为 `通道:目标ID`，目前支持 `feishu:<chat_id>`。

## 飞书

支持两种接入方式：

- **Webhook 模式**：配置事件订阅地址为 `/v1/feishu/webhook`
- **长连接模式**：设置 `channels.feishu.long_connection: true`，无需公网回调地址

功能特性：

- 直接回复用户消息（Reply 模式，而非发新消息）
- 收到消息后在原消息上标记思考中 Emoji Reaction（可配置 `thinking_emoji`）
- 回复支持 Markdown 富文本（飞书卡片消息），失败时自动降级为纯文本
- 长消息自动分段发送
- 事件去重带 TTL 自动清理

## 项目结构

```
cmd/claw/           程序入口
internal/
  config/           配置加载与校验
  models/           内部数据结构
  llm/              LLM Provider 抽象与实现
  tools/            工具接口与实现
  mcp/              MCP 客户端，连接外部工具服务器
  cron/             定时任务调度器
  agent/            Agent 循环与排队
  gateway/          会话管理与协调
  channels/         通道适配器（HTTP/WebSocket/Feishu）
  skills/           技能加载器
  metrics/          Prometheus 指标
  audit/            审计日志
  requestctx/       请求上下文工具
skills/             自定义技能目录（可选）
```

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

## License

MIT
