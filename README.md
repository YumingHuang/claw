# Claw

Claw 是一个用 Go 语言实现的个人 AI 助手。本地优先、单二进制部署，支持多通道接入（HTTP、WebSocket、飞书）和多 LLM Provider 切换，通过可配置的工具系统扩展 Agent 能力。

## 特性

- **本地优先** — 核心逻辑与数据可在本机运行，隐私可控
- **模型无关** — 支持 OpenAI-compatible、Anthropic 等多种 LLM Provider
- **工具系统** — 内置文件读写、命令执行、时间查询等工具，沙箱隔离
- **流式响应** — SSE / WebSocket 流式输出，降低首字等待时间
- **配置即行为** — 通过 YAML 配置控制人设、工具权限、模型选择
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

# 发送消息
curl -X POST http://localhost:8080/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "你好"}'
```

## 配置

完整配置示例见 [`configs/config.example.yaml`](configs/config.example.yaml)，支持 `${ENV_VAR}` 语法引用环境变量。

## 开发

```bash
make test       # 运行测试（含 race 检测）
make lint       # 代码静态检查（需安装 golangci-lint）
make coverage   # 生成测试覆盖率报告
make clean      # 清理构建产物
```

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
```

## License

MIT
