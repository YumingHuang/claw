# Claw 前端 — 可执行任务清单

> 为 Claw 后端构建一个现代化的 Chat Web UI。
> 采用**嵌入式单页应用**方案：前端构建产物通过 Go `embed` 打包进二进制，保持单文件部署。

---

## 技术选型

| 领域 | 选型 | 理由 |
|------|------|------|
| 框架 | React 18 + TypeScript | 组件化、生态成熟、类型安全 |
| 构建 | Vite | 零配置快、HMR 秒级、产物小 |
| 样式 | Tailwind CSS 4 | utility-first，无需写自定义 CSS 文件 |
| Markdown | `react-markdown` + `remark-gfm` | 渲染 LLM 回复中的 Markdown / 代码块 |
| 代码高亮 | `highlight.js` | 轻量、语言覆盖全 |
| 图标 | `lucide-react` | 统一风格、tree-shakable |
| HTTP | `fetch` API | 标准库，无需 axios |
| SSE | `EventSource` / `fetch` + `ReadableStream` | 原生支持流式 |
| 状态 | React `useState` + `useReducer` | 项目规模不需要外部状态库 |
| 持久化 | `localStorage` | 会话列表本地缓存 |
| 部署 | Go `embed` → 静态文件嵌入二进制 | 单文件部署 |

---

## 目录结构

```
claw/
├── web/                        # 前端源码（独立于 Go 代码）
│   ├── src/
│   │   ├── main.tsx            # 入口
│   │   ├── App.tsx             # 根组件：布局 + 路由
│   │   ├── api/
│   │   │   └── client.ts       # 后端 API 封装（chat / session / health）
│   │   ├── components/
│   │   │   ├── ChatView.tsx    # 聊天主区域
│   │   │   ├── MessageBubble.tsx   # 单条消息（支持 Markdown）
│   │   │   ├── InputBar.tsx    # 底部输入框 + 发送按钮
│   │   │   ├── Sidebar.tsx     # 左侧会话列表
│   │   │   ├── SessionItem.tsx # 单条会话项
│   │   │   ├── StatusBar.tsx   # 顶部连接状态 / 模型信息
│   │   │   └── ToolCallCard.tsx # 工具调用展示卡片
│   │   ├── hooks/
│   │   │   ├── useChat.ts      # 聊天逻辑：发送、流式接收、状态
│   │   │   └── useSessions.ts  # 会话 CRUD + localStorage 同步
│   │   ├── types/
│   │   │   └── index.ts        # TypeScript 类型定义
│   │   └── utils/
│   │       └── markdown.ts     # Markdown 渲染配置
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   ├── vite.config.ts
│   └── tailwind.config.ts
├── internal/
│   └── channels/
│       └── static.go           # Go embed 静态文件服务
└── ...
```

---

## Phase F0：项目骨架（约 1 小时）

### TF0.1 初始化前端项目

**预估**：30 分钟 | **前置**：无 | **产出**：可运行的空 Vite + React 项目

- [ ] **TF0.1.1** 在项目根目录创建 `web/` 子目录，使用 Vite 初始化
  ```bash
  cd claw
  npm create vite@latest web -- --template react-ts
  cd web && npm install
  ```

- [ ] **TF0.1.2** 安装依赖
  ```bash
  npm install react-markdown remark-gfm highlight.js lucide-react
  npm install -D tailwindcss @tailwindcss/vite
  ```

- [ ] **TF0.1.3** 配置 Tailwind CSS
  - `vite.config.ts` 中添加 `@tailwindcss/vite` 插件
  - `src/index.css` 中添加 `@import "tailwindcss";`

- [ ] **TF0.1.4** 配置 Vite 开发代理
  - `vite.config.ts` 中添加 proxy，将 `/v1/*`、`/health`、`/status` 转发到 `http://localhost:8080`
  - 使开发时前后端可同时运行

- [ ] **TF0.1.5** 验证
  ```bash
  npm run dev   # 浏览器打开能看到默认页面
  npm run build # dist/ 产物正常生成
  ```

---

### TF0.2 TypeScript 类型定义

**预估**：20 分钟 | **前置**：TF0.1 | **产出**：`src/types/index.ts`

- [ ] **TF0.2.1** 定义与后端 API 对应的类型
  ```typescript
  // 消息
  interface Message {
    role: "system" | "user" | "assistant" | "tool";
    content: string;
    tool_calls?: ToolCall[];
    tool_call_id?: string;
    timestamp: string;
  }

  interface ToolCall {
    id: string;
    name: string;
    arguments: string;  // JSON string
  }

  // 请求 / 响应
  interface ChatRequest {
    session_id?: string;
    message: string;
    stream: boolean;
  }

  interface ChatResponse {
    session_id: string;
    request_id: string;
    message: Message;
    usage?: Usage;
    tool_calls_count: number;
  }

  interface Usage {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
  }

  // SSE chunk
  interface StreamChunk {
    delta: string;
    done: boolean;
  }

  // 会话
  interface Session {
    id: string;
    title: string;          // 前端生成：取首条 user 消息的前 30 字
    messages: Message[];
    created_at: string;
    updated_at: string;
  }

  // 状态
  interface HealthResponse {
    status: string;
    version: string;
  }

  interface StatusResponse {
    active_sessions: number;
    tools: string[];
  }
  ```
  **完成标准**：`npm run build` 无类型错误

---

## Phase F1：核心聊天界面（约 4～5 小时）

### TF1.1 API 客户端

**预估**：1 小时 | **前置**：TF0.2 | **产出**：`src/api/client.ts`

- [ ] **TF1.1.1** 实现非流式聊天
  ```typescript
  async function sendMessage(sessionId: string, message: string): Promise<ChatResponse>
  ```
  - POST `/v1/chat`，`stream: false`
  - 错误处理：解析 `error.code`，抛出类型化异常

- [ ] **TF1.1.2** 实现流式聊天
  ```typescript
  async function sendMessageStream(
    sessionId: string,
    message: string,
    onChunk: (delta: string) => void,
    onDone: () => void,
    onError: (err: string) => void
  ): Promise<void>
  ```
  - POST `/v1/chat`，`stream: true`
  - 使用 `fetch` + `ReadableStream` 解析 SSE（不用 EventSource，因为需要 POST）
  - 逐行解析 `event:` 和 `data:` 行
  - `event: chunk` → 调用 `onChunk(delta)`
  - `event: done` → 调用 `onDone()`
  - `event: error` → 调用 `onError(msg)`

- [ ] **TF1.1.3** 实现会话相关 API
  ```typescript
  async function getSession(id: string): Promise<SessionDetail>
  async function deleteSession(id: string): Promise<void>
  async function getHealth(): Promise<HealthResponse>
  async function getStatus(): Promise<StatusResponse>
  ```

---

### TF1.2 聊天 Hook

**预估**：1 小时 | **前置**：TF1.1 | **产出**：`src/hooks/useChat.ts`

- [ ] **TF1.2.1** 实现 `useChat` hook
  ```typescript
  function useChat(sessionId: string) {
    // 状态
    messages: Message[]
    isLoading: boolean
    streamingContent: string   // 正在流式输出的内容

    // 操作
    sendMessage(content: string): Promise<void>
    cancelStream(): void
  }
  ```
  - 发送消息时：
    1. 追加 user message 到 `messages`
    2. 设 `isLoading = true`
    3. 调用 `sendMessageStream`，每个 chunk 追加到 `streamingContent`
    4. 流结束后：将 `streamingContent` 转为完整的 assistant message 追加到 `messages`，清空 `streamingContent`
  - 支持通过 `AbortController` 取消正在进行的流

- [ ] **TF1.2.2** 实现 `useSessions` hook
  ```typescript
  function useSessions() {
    sessions: Session[]          // 会话列表
    currentSessionId: string
    createSession(): string      // 创建新会话，返回 ID
    selectSession(id: string): void
    deleteSession(id: string): Promise<void>
    renameSession(id: string, title: string): void
  }
  ```
  - 会话列表持久化到 `localStorage`
  - 新建会话自动生成 UUID
  - 删除会话同时调用后端 `DELETE /v1/sessions/:id`

---

### TF1.3 消息气泡组件

**预估**：1.5 小时 | **前置**：TF0.2 | **产出**：`MessageBubble.tsx` + `ToolCallCard.tsx`

- [ ] **TF1.3.1** 实现 `MessageBubble` 组件
  - Props：`message: Message`、`isStreaming?: boolean`
  - user 消息：右对齐，蓝色背景，圆角
  - assistant 消息：左对齐，灰色背景，支持 Markdown 渲染
  - 流式输出时显示闪烁光标 `▊`
  - Markdown 渲染：
    - 代码块 → `highlight.js` 语法高亮 + 复制按钮
    - 表格 → 带边框样式
    - 链接 → 新窗口打开
  - 显示时间戳（相对时间：刚刚 / 1 分钟前 / 10:30）

- [ ] **TF1.3.2** 实现 `ToolCallCard` 组件
  - 当 assistant message 含 `tool_calls` 时，渲染为折叠卡片
  - 显示：工具名称图标 + 工具名 + 参数（JSON 格式化）
  - tool result message 显示在卡片下方
  - 可折叠/展开

---

### TF1.4 输入栏组件

**预估**：30 分钟 | **前置**：TF0.1 | **产出**：`InputBar.tsx`

- [ ] **TF1.4.1** 实现 `InputBar` 组件
  - 多行 `<textarea>`，自动增高（最大 6 行）
  - 发送按钮（图标 + 快捷键 `Ctrl/Cmd + Enter`）
  - 发送中显示停止按钮（点击取消流式）
  - 空内容时发送按钮禁用
  - 发送后自动聚焦输入框

---

### TF1.5 聊天主视图

**预估**：30 分钟 | **前置**：TF1.2, TF1.3, TF1.4 | **产出**：`ChatView.tsx`

- [ ] **TF1.5.1** 实现 `ChatView` 组件
  - 消息列表：`MessageBubble` 渲染每条消息
  - 自动滚动到底部（新消息/流式输出时）
  - 空会话时显示欢迎页（Logo + 快捷提问建议）
  - 底部固定 `InputBar`
  - 加载状态：assistant 区域显示「思考中...」动画

---

## Phase F2：会话管理与布局（约 2～3 小时）

### TF2.1 侧边栏

**预估**：1.5 小时 | **前置**：TF1.2 | **产出**：`Sidebar.tsx` + `SessionItem.tsx`

- [ ] **TF2.1.1** 实现 `Sidebar` 组件
  - 顶部：Logo + 「新建对话」按钮
  - 中部：会话列表（按最近更新排序）
  - 底部：连接状态指示灯 + 版本号
  - 移动端可折叠（汉堡菜单）

- [ ] **TF2.1.2** 实现 `SessionItem` 组件
  - 显示会话标题（首条用户消息前 30 字）
  - 高亮当前选中的会话
  - 悬浮时显示删除按钮
  - 点击切换会话
  - 删除时有确认提示

---

### TF2.2 整体布局

**预估**：30 分钟 | **前置**：TF2.1, TF1.5 | **产出**：`App.tsx`

- [ ] **TF2.2.1** 实现 `App` 根组件
  - 响应式两栏布局：左侧 Sidebar（280px）+ 右侧 ChatView
  - 移动端（<768px）：Sidebar 以抽屉形式覆盖
  - 深色/浅色主题支持（跟随系统 `prefers-color-scheme`）
  - 全局错误 Toast（网络错误、API 错误）

---

### TF2.3 状态栏

**预估**：30 分钟 | **前置**：TF1.1 | **产出**：`StatusBar.tsx`

- [ ] **TF2.3.1** 实现 `StatusBar` 组件
  - 定时（每 30s）轮询 `GET /health`
  - 显示连接状态：🟢 已连接 / 🔴 断开
  - 显示后端版本号
  - 显示当前可用工具数量

---

## Phase F3：Go 嵌入与集成（约 1～2 小时）

### TF3.1 前端构建产物嵌入

**预估**：1 小时 | **前置**：Phase F1 | **产出**：Go 静态文件服务

- [ ] **TF3.1.1** 创建 `internal/channels/static.go`
  ```go
  //go:embed all:dist
  var distFS embed.FS

  func StaticFileHandler() http.Handler {
      sub, _ := fs.Sub(distFS, "dist")
      fileServer := http.FileServer(http.FS(sub))
      return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
          // 尝试提供静态文件，404 时回退到 index.html（SPA 路由）
      })
  }
  ```

- [ ] **TF3.1.2** 更新 HTTP 路由
  - API 路由（`/v1/*`、`/health`、`/status`）优先
  - 其余路径回退到静态文件服务
  - 根路径 `/` 返回 `index.html`

- [ ] **TF3.1.3** 更新 Makefile
  ```makefile
  build-web:
  	cd web && npm run build
  	cp -r web/dist internal/channels/dist

  build: build-web
  	go build -o bin/claw ./cmd/claw
  ```

- [ ] **TF3.1.4** 验证
  - `make build` 成功
  - `./bin/claw -config configs/config.example.yaml`
  - 浏览器打开 `http://localhost:8080` 看到前端界面

---

### TF3.2 CORS 与开发体验

**预估**：30 分钟 | **前置**：TF3.1 | **产出**：开发时前后端联调无障碍

- [ ] **TF3.2.1** 添加 CORS 中间件（仅开发模式）
  - 通过配置 `server.cors_enabled: true` 控制
  - 允许 `http://localhost:5173`（Vite 默认端口）
  - 生产模式关闭（前端已嵌入，同源，无需 CORS）

- [ ] **TF3.2.2** 更新 README
  - 前端开发流程：`cd web && npm run dev`
  - 构建流程：`make build-web && make build`
  - 生产部署：单二进制，访问根路径即可

---

## Phase F4：体验优化（约 2～3 小时）

### TF4.1 Markdown 与代码块

**预估**：1 小时 | **前置**：TF1.3

- [ ] **TF4.1.1** 代码块增强
  - 语法高亮（highlight.js，支持常用语言）
  - 代码块右上角显示语言标签 + 复制按钮
  - 复制后按钮文字变为「已复制 ✓」，2 秒后恢复

- [ ] **TF4.1.2** Markdown 样式
  - 行内代码 `code` 样式（背景色 + 圆角）
  - 有序/无序列表缩进
  - 引用块左侧竖线样式
  - 表格带斑马条纹

---

### TF4.2 深色模式

**预估**：30 分钟 | **前置**：TF2.2

- [ ] **TF4.2.1** 实现深色/浅色主题切换
  - 默认跟随系统 `prefers-color-scheme`
  - Sidebar 底部添加主题切换按钮（太阳/月亮图标）
  - 选择持久化到 `localStorage`
  - Tailwind `dark:` 变体覆盖所有组件

---

### TF4.3 快捷操作

**预估**：30 分钟 | **前置**：TF1.5

- [ ] **TF4.3.1** 欢迎页快捷提问
  - 空会话时显示 4 个建议卡片（如「现在几点？」「帮我读取文件」...）
  - 点击卡片自动发送对应消息

- [ ] **TF4.3.2** 消息操作
  - 鼠标悬浮在 assistant 消息上显示「复制」按钮
  - 复制整条 Markdown 原文到剪贴板

---

### TF4.4 响应式与移动端

**预估**：30 分钟 | **前置**：TF2.2

- [ ] **TF4.4.1** 移动端适配
  - `<768px`：Sidebar 隐藏，顶部显示汉堡按钮
  - 点击汉堡按钮展开 Sidebar（覆盖层 + 半透明背景）
  - 选择会话后自动关闭 Sidebar
  - 输入框适配软键盘弹出

---

## 任务进度总览

| 任务 ID | 描述 | 预估 | 状态 |
|---------|------|------|------|
| TF0.1 | 初始化前端项目 | 30m | ⬜ |
| TF0.2 | TypeScript 类型定义 | 20m | ⬜ |
| TF1.1 | API 客户端 | 1h | ⬜ |
| TF1.2 | 聊天 Hook | 1h | ⬜ |
| TF1.3 | 消息气泡组件 | 1.5h | ⬜ |
| TF1.4 | 输入栏组件 | 30m | ⬜ |
| TF1.5 | 聊天主视图 | 30m | ⬜ |
| TF2.1 | 侧边栏 | 1.5h | ⬜ |
| TF2.2 | 整体布局 | 30m | ⬜ |
| TF2.3 | 状态栏 | 30m | ⬜ |
| TF3.1 | 前端构建嵌入 Go | 1h | ⬜ |
| TF3.2 | CORS 与开发体验 | 30m | ⬜ |
| TF4.1 | Markdown 与代码块 | 1h | ⬜ |
| TF4.2 | 深色模式 | 30m | ⬜ |
| TF4.3 | 快捷操作 | 30m | ⬜ |
| TF4.4 | 响应式与移动端 | 30m | ⬜ |

**总计预估**：~12 小时

| Phase | 预估 | 任务数 |
|-------|------|--------|
| Phase F0 | ~1h | 2 任务 |
| Phase F1 | ~4.5h | 5 任务 |
| Phase F2 | ~2.5h | 3 任务 |
| Phase F3 | ~1.5h | 2 任务 |
| Phase F4 | ~2.5h | 4 任务 |

---

## 依赖关系图

```
Phase F0:  TF0.1 ──→ TF0.2

Phase F1:  TF0.2 ──→ TF1.1 ──→ TF1.2 ──→ TF1.5
           TF0.2 ──→ TF1.3 ──────────────┘
           TF0.1 ──→ TF1.4 ──────────────┘

Phase F2:  TF1.2 ──→ TF2.1 ──→ TF2.2
           TF1.5 ──────────────┘
           TF1.1 ──→ TF2.3

Phase F3:  F1.* ──→ TF3.1 ──→ TF3.2

Phase F4:  TF1.3 ──→ TF4.1
           TF2.2 ──→ TF4.2, TF4.4
           TF1.5 ──→ TF4.3
```

---

## 设计参考

### 视觉风格

- **整体**：简洁克制，参考 ChatGPT / Claude 对话界面
- **配色**：浅色 — 白色背景 + 灰色消息区；深色 — #1a1a2e 背景 + #16213e 消息区
- **字体**：系统默认字体栈（`-apple-system, BlinkMacSystemFont, "Segoe UI", ...`）
- **代码字体**：`"JetBrains Mono", "Fira Code", monospace`
- **圆角**：消息气泡 12px，按钮 8px，卡片 8px
- **动画**：流式文字逐字显示；消息入场 fade-in；按钮 hover 微缩放

### 关键交互

1. **发送消息**：Enter 换行，Cmd/Ctrl+Enter 发送
2. **流式输出**：文字逐字出现 + 闪烁光标 `▊`
3. **工具调用**：先显示「🔧 正在调用 get_current_time...」→ 折叠卡片展示结果
4. **错误处理**：红色 Toast 自动消失（3 秒）；网络断开时输入框禁用 + 提示
5. **新建对话**：侧边栏顶部按钮 + 快捷键 `Cmd/Ctrl+N`

---

*任务清单版本：v0.1 | 与 spec.md v0.2、后端 Phase 1 MVP 对应*
