# gpt2api

> 基于逆向 **chatgpt.com** 的 OpenAI 兼容 SaaS 网关 —— 多账号池 / 代理池 / **IMG2 灰度命中** / **批量出图** / **高并发调度** / 积分计费 / 管理后台一体化。

<p align="center">
  <a href="https://github.com/432539/gpt2api/stargazers"><img alt="stars" src="https://img.shields.io/github/stars/432539/gpt2api?style=flat-square"></a>
  <a href="https://github.com/432539/gpt2api/releases"><img alt="release" src="https://img.shields.io/github/v/release/432539/gpt2api?style=flat-square"></a>
  <a href="https://golang.org/"><img alt="go" src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=flat-square&logo=go"></a>
  <a href="https://vuejs.org/"><img alt="vue" src="https://img.shields.io/badge/Vue-3-4FC08D?style=flat-square&logo=vue.js"></a>
  <a href="https://github.com/432539/gpt2api/blob/main/LICENSE"><img alt="license" src="https://img.shields.io/badge/License-MIT-green?style=flat-square"></a>
</p>

- **仓库地址**:<https://github.com/432539/gpt2api>
- **技术交流 QQ 群**:`382446`(入群请注明「gpt2api」)

---

## 目录

- [一、项目定位](#一项目定位)
- [📸 界面预览](#-界面预览)
- [二、核心特性](#二核心特性)
- [三、技术栈](#三技术栈)
- [四、架构概览](#四架构概览)
- [五、快速开始(Docker 一键部署)](#五快速开始docker-一键部署)
- [六、配置说明](#六配置说明)
- [七、API 使用示例](#七api-使用示例)
- [八、重点能力详解](#八重点能力详解)
  - [8.1 IMG2 灰度命中测试](#81-img2-灰度命中测试)
  - [8.2 批量出图 / 多张聚合](#82-批量出图--多张聚合)
  - [8.3 高性能高并发调度](#83-高性能高并发调度)
- [九、管理后台功能概览](#九管理后台功能概览)
- [十、目录结构](#十目录结构)
- [十一、二次开发 / 定制](#十一二次开发--定制)
- [十二、FAQ](#十二faq)
- [十三、Roadmap](#十三roadmap)
- [十四、参与贡献](#十四参与贡献)
- [十五、免责声明与风险提示](#十五免责声明与风险提示)
- [十六、License](#十六license)

---

## 一、项目定位

`gpt2api` 是一个**自建的 ChatGPT → OpenAI 兼容网关**,把 `chatgpt.com` Plus / Team / Codex 账号的能力,以 **OpenAI 兼容 API** 的形式(`/v1/chat/completions` / `/v1/images/generations` / `/v1/responses`)开放给下游调用方,同时配套一整套 SaaS 运营后台。

适合的场景:

- 你手头有一批 ChatGPT Plus / Team / Codex 账号,想对外提供稳定的 **GPT Image / DALL·E 3 / IMG2 高清大图**服务;
- 想给公司 / 团队内部开通 OpenAI 风格的代理网关,把所有调用统计、计费、审计集中管理;
- 想低成本搭一个带积分 / 套餐 / 易支付的 AI API 中台,面向 C 端或 B 端开发者售卖。

> 本项目当前版本依然**优先优化图片链路**(详见 [8.1 IMG2 灰度命中测试](#81-img2-灰度命中测试) 与 [8.2 批量出图](#82-批量出图--多张聚合)),同时已经重新开放文字通路与 mixed-mode Playground 入口。thinking 请求会在调度阶段严格排除 free 账号,并在运行期遇到 `chatgpt-freeaccount` 时立即切号重试;`/v1/images/generations` 与 `/v1/images/edits` 仍保持独立图片协议,不受本轮 thinking 规则影响。

---

## 📸 界面预览

> 截图来自 **在线体验(Playground)** 页 · `gpt-image-2` / `picture_v2`(IMG2 灰度)· `9:16` 比例 · 单次调用一张 prompt 批量出图。

### 在线体验 · 文生图 / 批量出图

<p align="center">
  <img src="docs/screenshots/playground-batch.png" alt="gpt2api 在线体验 · 文生图 · IMG2 批量出图" width="960">
</p>

- 左侧:模型选择、画面比例(1:1 / 16:9 / **9:16**)、张数、PROMPT 输入、prompt 预设库;
- 右侧:**同一个任务聚合返回多张高清终稿**(IMG2 命中时单次 `/f/conversation` 一次性产出 2 张,再配合"张数 N"即可成批扩图);
- 点击任意一张可进入全屏放大预览 ↓。

### 管理后台 · 单图放大预览

<p align="center">
  <img src="docs/screenshots/playground-preview.png" alt="gpt2api 管理后台 · 图片全屏预览 · 左侧完整菜单" width="960">
</p>

- 左侧:**个人中心 / 后台管理** 双分区菜单 —— API Keys、使用记录、账单与充值、在线体验、接口文档、用户管理、GPT 账号池、代理管理、模型配置、用户分组、用量统计、全局 Keys、审计日志、数据备份、系统设置,一个台子全搞定;
- 中间:全屏放大查看终稿,直接右键"图片另存为"。所有图片 URL 都是内置 `/p/img/:task/:idx` **HMAC 签名代理**,绕过 `chatgpt.com` `estuary/content` 的 403 防盗链。

---

## 二、核心特性

| 分类 | 能力 |
|------|------|
| **上游协议** | 完整逆向 `chatgpt.com` `f/conversation` 两步 sentinel(`/prepare` + `/finalize`)、PoW、`conduit_token`、全套 `oai-*` / `Sec-Ch-Ua-*` 指纹头 |
| **图片生成** | 文生图、**图生图 / 多图参考**、**IMG2 灰度命中**、`n>1` **并发聚合出图**、**preview_only 自动重试**、轮询 + SSE 直出双通道 |
| **账号池** | JSON / AT / RT / ST 四种方式批量导入,**自动刷新**、**额度探测**、**风控熔断**、按账号稳定绑定 `oai-device-id` / `oai-session-id` |
| **代理池** | 支持 HTTP / SOCKS5,健康分自动探测,按账号强绑定代理,避免 IP 指纹混用 |
| **调度器** | 串行 lease + Redis 分布式锁,`min_interval_sec` 单号最小间隔、`daily_usage_ratio` 日熔断、`cooldown_429_sec` 限速退避 |
| **OpenAI 兼容** | `/v1/chat/completions`、`/v1/responses`(mixed-mode 已支持 `stream=true` + 实时 reasoning)、`/v1/images/generations`、`/v1/images/edits`、`/v1/images/tasks/:id`、`/v1/models` |
| **下游 Key** | 独立于用户账号的 `sk-` Key,支持 **RPM / TPM / 日配额 / IP 白名单 / 模型白名单** |
| **计费** | 积分钱包 + 预扣结算、分组倍率(VIP / 内部 / 渠道)、充值套餐、**易支付(EPay)**接入 |
| **安全** | AES-256-GCM 加密 AT / cookies、JWT 登录、RBAC 权限、**管理员写操作全链路审计**、高危操作 `X-Admin-Confirm` 二次确认 |
| **运维** | 数据库一键备份 / 恢复(`mysqldump` + gzip)、上传单文件限额、备份保留策略 |
| **图片防盗链** | 内置签名代理 `/p/img/:task/:idx`,HMAC 签名 + 过期时间,绕过 `chatgpt.com` `estuary/content` 的 403 |
| **前端** | Vue 3 + Element Plus 单页控制台,账户池 / 代理池 / 模型 / 用户 / 积分 / 审计 / 备份 / 系统设置全覆盖 |

---

## 三、技术栈

**后端**

- Go 1.22+
- Gin(HTTP 框架) / sqlx(MySQL 访问) / Viper(配置) / Zap(日志)
- MySQL 8.0(业务数据 + 审计 + 账变) / Redis 7(分布式锁 / 限流 / 缓存)
- `refraction-networking/utls`(TLS 指纹,用于规避 `chatgpt.com` JA3 检测)
- `golang-jwt/jwt` / `golang.org/x/crypto`(鉴权 + 密码学)
- Goose(数据库迁移)

**前端**

- Vue 3 + Vite 5 + TypeScript 5
- Element Plus 2.7(组件库)
- Pinia(状态管理) / `pinia-plugin-persistedstate`
- Vue Router 4
- Axios

**部署**

- Docker Compose(MySQL + Redis + server,可选 nginx)
- 默认单机;水平扩展见 [`deploy/README.md`](deploy/README.md)

---

## 四、架构概览

```mermaid
flowchart LR
  subgraph Client["下游调用方"]
    SDK["OpenAI SDK / curl / 自家业务"]
  end

  subgraph Gateway["gpt2api Server :8080"]
    direction TB
    API["Gin Router<br/>/v1/* · /api/*"]
    Auth["APIKey / JWT<br/>RPM · TPM · IP 白名单"]
    Billing["积分预扣<br/>分组倍率"]
    Scheduler["账号调度器<br/>Redis 锁 · lease · 熔断"]
    Upstream["ChatGPT Client<br/>utls · sentinel v2 · 多 header 指纹"]
    ImgProxy["图片签名代理<br/>/p/img/:id/:n"]
  end

  subgraph Pool["资源池"]
    Accounts[("GPT 账号池<br/>AT / RT / ST")]
    Proxies[("代理池<br/>HTTP / SOCKS5")]
    Models[("模型配置<br/>对外 slug ⇄ 上游 slug")]
  end

  subgraph Storage["持久化"]
    MySQL[(MySQL 8.0<br/>用户 · 账号 · 账变 · 审计)]
    Redis[(Redis 7<br/>分布式锁 · 限流)]
  end

  subgraph UpstreamAPI["chatgpt.com"]
    Sentinel[/chat-requirements prepare + finalize/]
    FConv[/f/conversation/]
    Estuary[/estuary/content/]
  end

  SDK -- "Bearer sk-xxx" --> API
  API --> Auth --> Billing --> Scheduler
  Scheduler --> Accounts
  Scheduler --> Proxies
  Scheduler --> Models
  Scheduler --> Upstream
  Upstream --> Sentinel
  Upstream --> FConv
  Upstream --> Estuary
  Estuary -. "fid / sid" .-> ImgProxy
  ImgProxy --> SDK
  Gateway <--> MySQL
  Gateway <--> Redis
```

**数据流(一次文生图调用)**:

1. 下游 `POST /v1/images/generations` 携带 `Authorization: Bearer sk-xxx`;
2. Gateway 校验 Key → 查下游限流(RPM/TPM/日配额)→ 预扣积分;
3. Scheduler 从账号池挑一个 `idle` 且满足 `min_interval_sec` 的账号,拿 Redis 锁建立 lease;
4. 通过账号绑定的代理,走 `utls` TLS 指纹,按真实 Edge 143 浏览器的 header/payload 访问 `chatgpt.com`;
5. 两步 sentinel 换 chat-requirements token → `/f/conversation/prepare` 拿 `conduit_token` → SSE 上游生图;
6. 解析 tool message 拿 `fids` / `sids`;单张请求若是 `preview_only` 则在同一对话里重试最多 3 次争取 IMG2 灰度,`n>1` 会在同一账号 lease 下拆成多个独立子任务并聚合;
7. 所有图片 URL 经 HMAC 签名,返回 `https://<your-domain>/p/img/<task>/<idx>?exp=…&sig=…`;
8. 扣费结算 + 写 usage_logs + 释放 lease + 更新账号状态。

---

## 五、快速开始(Docker 一键部署)

### 1. 准备环境

- Docker 24+
- docker compose 插件
- 一个能直连 `chatgpt.com` 的 VPS,或者至少一个可用的 HTTP / SOCKS5 代理
- 至少 1 个 ChatGPT Plus / Team / Codex 账号(能导出 AT / RT / ST 或 JSON 会话信息)

### 2. 克隆 + 启动

```bash
git clone https://github.com/432539/gpt2api.git
cd gpt2api/deploy
cp .env.example .env
```

**必改** `.env` 中的三项:

```env
JWT_SECRET=请改成 >=32 位随机串
CRYPTO_AES_KEY=请改成严格 64 位 hex(32 字节 AES-256)
MYSQL_ROOT_PASSWORD=你自己的强密码
MYSQL_PASSWORD=你自己的强密码
```

生成两个随机值的快捷命令:

```bash
openssl rand -hex 32   # CRYPTO_AES_KEY(64 位 hex)
openssl rand -base64 48 | tr -d '=/+' | cut -c1-48   # JWT_SECRET
```

启动:

```bash
docker compose up -d --build
docker compose logs -f server
```

启动过程里 `server` 会自动:

1. 等 `mysql` 健康;
2. 跑 `goose up` 应用全部迁移(用户 / 账号 / 审计 / 备份元数据等十余张表);
3. 启动 HTTP 服务 `:8080`。

### 3. 首次登录

- 管理后台地址:`http://<服务器IP>:8080/`
- 默认超管:`admin@example.com` / `admin123`(**请务必第一时间登入后台修改密码**)

### 4. 五分钟跑通第一次生图

1. **管理后台 → 代理管理** → 新建一个代理(或批量导入),确认健康分为绿色;
2. **管理后台 → GPT账号** → 批量导入 JSON / AT / RT / ST,绑定上一步的代理;
3. **管理后台 → 模型配置** → 确认有 `gpt-image-2` 等图像模型且已启用;
4. **管理后台 → 用户管理** → 给自己(或业务账号)加点积分;
5. **个人中心 → 在线体验** → 文生图 tab → 输入 prompt → 点生成;
6. 观察 `docker compose logs -f server` 里 `image runner` 系列日志,看到 `image runner result summary turns_used=1 is_preview=false signed_count=N` 就是成功命中 IMG2 / IMG1。

---

## 六、配置说明

**核心配置文件:`configs/config.yaml`**(Docker 部署时通过环境变量 `GPT2API_*` 覆盖)。完整字段见 [`configs/config.example.yaml`](configs/config.example.yaml)。

| 段落 | 关键字段 | 说明 |
|------|---------|------|
| `app` | `listen`, `base_url` | HTTP 监听地址 / 对外 base URL(签名图片代理用) |
| `mysql` | `dsn`, `max_open_conns` | MySQL 连接,生产推荐 500 + |
| `redis` | `addr`, `pool_size` | Redis,生产推荐 pool=500(锁 / 限流 / 令牌桶) |
| `jwt` | `secret`, `*_ttl_sec` | **生产必须覆盖** `secret` |
| `crypto` | `aes_key` | **生产必须覆盖**,32 字节 hex,用于加密账号 AT / cookies |
| `scheduler` | `min_interval_sec` | **单账号最小间隔秒**,对抗风控核心参数 |
| `scheduler` | `daily_usage_ratio` | 单号日消耗熔断阈值(0~1,0.6 = 消耗超过日额度 60% 自动下线) |
| `scheduler` | `cooldown_429_sec` | 连续 429 冷却时间 |
| `upstream` | `request_timeout_sec` | 上游 chatgpt.com 请求超时(图片建议 60+) |
| `upstream` | `sse_read_timeout_sec` | SSE 读超时,批量出图场景建议 300+ |
| `epay` | `gateway_url`, `pid`, `key` | 易支付网关,用于积分充值 |

**环境变量覆盖规则**:任何 `configs/config.yaml` 字段都可以用 `GPT2API_XXX_YYY` 覆盖。例如 `app.listen` → `GPT2API_APP_LISTEN`。

---

## 七、API 使用示例

> 所有 API 以 OpenAI 官方 SDK 兼容为主,把 `base_url` 换成你的部署地址即可。需要注意:`/v1/chat/completions` 与 `/v1/responses` 的 mixed-mode 生图首版会在成功响应顶层额外返回一个 `images` 扩展数组,便于前端与 SDK 统一取图。

### 7.1 生图(同步,单张)

```bash
curl https://your-domain.com/v1/images/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "a cute orange cat playing with yarn, studio ghibli style",
    "n": 1,
    "size": "1024x1024"
  }'
```

**返回**(已经是 HMAC 签名的图片代理地址,可直接 `<img src>` 嵌入):

```json
{
  "created": 1776582860,
  "data": [
    {
      "url": "https://your-domain.com/p/img/img_2631ffad.../0?exp=...&sig=..."
    }
  ]
}
```

### 7.2 图生图 / 多图参考(项目扩展字段)

```bash
curl https://your-domain.com/v1/images/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "将这两张图合成为赛博朋克风格的海报",
    "n": 2,
    "size": "1792x1024",
    "reference_images": [
      "https://example.com/ref1.jpg",
      "data:image/png;base64,iVBORw0KG..."
    ]
  }'
```

### 7.3 Python(OpenAI SDK)

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://your-domain.com/v1",
    api_key="sk-xxx",
)

resp = client.images.generate(
    model="gpt-image-2",
    prompt="cyberpunk alley in the rain, cinematic lighting",
    n=2,
    size="1792x1024",
)
for img in resp.data:
    print(img.url)
```

### 7.4 异步(适合慢 prompt / 批量场景)

```bash
# 提交任务
curl -X POST https://your-domain.com/v1/images/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-image-2","prompt":"...", "async":true}'
# 返回 {"task_id":"img_xxx","status":"queued"}

# 轮询结果
curl https://your-domain.com/v1/images/tasks/img_xxx \
  -H "Authorization: Bearer sk-xxx"
```

### 7.5 对话框生图(mixed-mode, 首版)

启用条件:

- 后端系统设置打开 `gateway.chat_image_mixed_enabled=true`
- 前端默认已开放 Playground / 接口文档入口;若需要临时隐藏,可把 `web/src/config/feature.ts` 里的 `ENABLE_CHAT_IMAGE_MIXED` 改回 `false`

`/v1/chat/completions` 触发方式:

```bash
curl https://your-domain.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5",
    "image_generation": true,
    "stream": false,
    "messages": [
      {"role": "user", "content": "请画一张雨夜霓虹街头的赛博朋克海报"}
    ]
  }'
```

`/v1/responses` 触发方式:

```bash
curl https://your-domain.com/v1/responses \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5",
    "input": "请生成一张极简主义太空旅行海报",
    "tools": [{"type": "image_generation"}],
    "stream": false
  }'
```

**成功返回(JSON 示例)**:

```json
{
  "id": "resp_xxx",
  "object": "response",
  "created_at": 1710806400,
  "model": "gpt-5",
  "status": "completed",
  "output": [
    {
      "id": "igc_xxx",
      "type": "image_generation_call",
      "status": "completed",
      "result": [
        {
          "type": "output_image",
          "url": "https://your-domain.com/p/img/img_xxx/0?exp=...&sig=...",
          "file_id": "file-xxx",
          "content_type": "image/png",
          "task_id": "img_xxx"
        }
      ]
    }
  ],
  "images": [
    {
      "url": "https://your-domain.com/p/img/img_xxx/0?exp=...&sig=...",
      "file_id": "file-xxx",
      "content_type": "image/png",
      "task_id": "img_xxx"
    }
  ]
}
```

**流式返回要点**:

`/v1/chat/completions stream=true`:

```text
data: {"choices":[{"delta":{"role":"assistant"}}]}
data: {"choices":[{"delta":{"reasoning":"先拆分镜头与角色动作。"}}]}
data: {"choices":[{"delta":{"content":"我会生成一张透明背景故事图。"}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}],"images":[{"url":"https://your-domain.com/p/img/img_xxx/0?exp=...&sig=..."}]}
data: [DONE]
```

`/v1/responses stream=true`:

```text
event: response.created
event: response.reasoning.delta
event: response.output_text.delta
event: response.image_generation_call.completed | response.image_generation_call.in_progress
event: response.completed | response.in_progress
```

协议与约束:

- mixed-mode 只在 `image_generation=true` 或 `tools:[{type:"image_generation"}]` 时触发
- 如需稳定触发思考,请显式使用 thinking 模型名;推荐 `gpt-5-thinking` 这个稳定别名(当前映射到 `gpt-5-4-thinking`)
- `chat/completions stream=true` 已支持实时 `delta.reasoning` 与 `delta.content`; mixed-mode 结束 chunk 会在顶层附带 `images`,若上游已受理但图片仍在补齐,会改为返回 `image_task`
- `/v1/responses stream=true` 会发送 `response.created`、`response.reasoning.delta`、`response.output_text.delta`、`response.image_generation_call.completed/in_progress`、`response.completed/in_progress`、`response.failed`
- thinking 普通对话非流式会返回 `choices[0].reasoning`; mixed-mode 成功时也会把 reasoning 聚合到文本返回里,便于前端展示
- thinking mixed-mode 继续走显式 thinking 模型 + `thinking_effort` 路径;若首响未拿满套图,服务端会返回 HTTP 200 + `image_task` 并在后台继续补齐,调用方可通过任务接口刷新结果
- 只有在短阻塞补拉和后台补拉都失败后,才会最终落成 `upstream_image_not_returned`;不会自动降级到旧 `/v1/images/generations`
- mixed-mode 仍保留对话模型语义,对外 `model` 不变,内部只用默认图片模型配置做计费和 `image_tasks` 落库映射
- thinking mixed-mode 默认使用更长的执行/轮询超时,并可通过 `gateway.chat_image_run_timeout_sec`、`gateway.chat_image_poll_max_wait_sec`、`gateway.chat_image_thinking_run_timeout_sec`、`gateway.chat_image_thinking_poll_max_wait_sec` 单独调优

**当前参数兼容矩阵(截至 2026-04-23)**:

`/v1/chat/completions`

| 参数 | 当前状态 | 说明 |
|---|---|---|
| `model` | 已支持 | 必填;会做模型白名单、模型表校验,必要时映射为上游真实 slug |
| `messages` | 已支持(文本子集) | 必填;当前 `messages[].content` 仅支持字符串,不支持 OpenAI 官方多段 `content[]` / 多模态对象 |
| `stream` | 已支持 | `true` 时走 SSE 转发 |
| `image_generation` | 已支持 | `true` 时进入 mixed-mode 生图链路 |
| `n` | 条件支持 | 仅允许 `image_generation=true`;否则返回 400 |
| `thinking_effort` | 条件支持 | 仅允许 `image_generation=true`;在 thinking mixed-mode 链路透传到上游 |
| `max_tokens` | 部分支持 | 当前只用于预估计费和 TPM 额度,不作为上游硬输出上限 |
| `temperature` / `top_p` / `user` | 已接收,暂未生效 | 当前不会透传到 chatgpt.com 上游 |
| 其他未声明字段 | 未支持 | 例如 `response_format`、`presence_penalty`、`frequency_penalty`、`seed`、`logprobs` 等,当前不会生效 |

`/v1/responses`

| 参数 | 当前状态 | 说明 |
|---|---|---|
| `model` | 已支持 | 必填 |
| `input` | 已支持(文本子集) | 必填;支持字符串 / 单消息对象 / 消息数组,但当前只提取文本内容 |
| `instructions` | 已支持 | 会转成首条 `system` 消息 |
| `stream` | 已支持 | 当前仅 mixed-mode SSE 事件流 |
| `image_generation` | 已支持 | 开启 mixed-mode 生图 |
| `tools` | 部分支持 | 当前只识别 `[{"type":"image_generation"}]`;其他 tools 不会生效 |
| `n` | 条件支持 | 仅允许 `image_generation=true` 或 `tools:[{"type":"image_generation"}]`;否则返回 400 |
| `thinking_effort` | 条件支持 | 仅允许 image mixed-mode;会透传到 thinking mixed-mode 上游 |
| `temperature` / `user` | 已接收,暂未生效 | 当前不会透传到上游 |
| 其他 responses 参数 | 未支持 | 例如 `max_output_tokens`、`tool_choice`、`parallel_tool_calls`、`metadata`、`modalities`、`response_format` 等,当前不会生效 |

兼容性备注:

- `/v1/responses` 首版目前只支持生图子集;如果既没有 `image_generation=true`,也没有 `tools:[{"type":"image_generation"}]`,会直接返回 400
- `/v1/chat/completions` 目前还是文本消息子集,如果下游直接发送官方多模态 `messages[].content[]`,大概率会在参数绑定阶段报错
- `/v1/responses input` 里的结构化内容当前只提取 `text`;`input_image`、`image_url`、音频等非文本 part 不会参与上游请求
- 如果业务需要“严格透传 OpenAI 参数”,需要继续补齐请求体绑定、参数校验和上游 payload 组装,不能只改文档

发布与观测建议:

- 先只打开后端 `gateway.chat_image_mixed_enabled`,用 API Key 直接验证 `/v1/chat/completions` 与 `/v1/responses`
- 前端入口默认已开放;如果你只想灰度给 API 用户,可临时关闭 `ENABLE_CHAT_IMAGE_MIXED`
- 发布后至少同时检查三处:`usage_logs.type=image`、`image_tasks.conversation_id/file_ids/result_urls`、`/p/img/...` 代理下载是否正常

---

## 八、重点能力详解

### 8.1 IMG2 灰度命中测试

`chatgpt.com` 自 2026 年起对 Plus / Team 账号灰度推出 **IMG2 管线**:**单次调用直接返回 2 张高清终稿**,跳过 IMG1 的预览中间态;命中 IMG2 的账号体感是**出图速度更快、画质更好、一次给两张**。

#### 如何判断自己命中了 IMG2?

`gpt2api` 在图片生成的每一个关键节点都打了结构化日志,观察 `docker compose logs -f server` 中的 `image runner` 系列:

```text
image runner SSE parsed         sse_fids=[file_xxx,file_yyy] finish_type=stop image_gen_task_id=...
image runner IMG2 direct hit (from SSE)    # ★ 看到这条 = SSE 流里直接返回了 2 张
image runner poll done          poll_status=IMG2 poll_fids=[file_xxx,file_yyy]
image runner IMG2 poll hit      # ★ 或者这条 = 轮询阶段拿到 IMG2
image runner result summary     turns_used=1 is_preview=false refs=[{fid,sid},{fid,sid}] signed_count=2
```

反过来,没命中 IMG2 时典型日志(会自动最多重试 3 轮在同一会话里争取终稿):

```text
image runner preview_only, retry in same conversation   turn=1 max_turns=3
image runner result summary     turns_used=3 is_preview=true signed_count=1
```

#### 测试方法

1. 导入若干 Plus 账号,在「GPT账号」页面「全部探测」一下基础额度;
2. 在「在线体验」用一段**固定、确定能过合规**的 prompt(例如 `a cute orange cat playing with yarn`)连续跑 5 次;
3. 后端统计两类日志:
   - **IMG2 命中率** = `image runner IMG2 *hit*` 的次数 / 总次数;
   - **IMG1 预览率(fallback)** = `result summary is_preview=true` 的次数 / 总次数;
4. 在「管理后台 → GPT账号」里对比不同账号的命中率,把低命中号放到 VIP / 非关键业务池,把高命中号分给关键业务;
5. 批量测试脚本可以直接用 SDK 并发跑(示例见 [8.2 批量出图](#82-批量出图--多张聚合))。

#### 数据库里也能复盘

每张图片都会被持久化到 `image_tasks` 表,带上 `account_id` / `status` / `image_urls`。一条 SQL 就能拉出每个账号的近期 IMG2 命中率:

```sql
SELECT
  account_id,
  COUNT(*)                                                           AS total,
  SUM(JSON_LENGTH(image_urls) >= 2)                                  AS img2_count,
  ROUND(SUM(JSON_LENGTH(image_urls) >= 2) * 100 / COUNT(*), 2)       AS img2_rate_pct
FROM image_tasks
WHERE status = 'success' AND created_at > NOW() - INTERVAL 1 DAY
GROUP BY account_id
ORDER BY img2_rate_pct DESC;
```

### 8.2 批量出图 / 多张聚合

`gpt2api` 支持三种"批量"场景:

| 场景 | 调用方式 | 实际并发 |
|------|---------|---------|
| **单请求 N 张** | `{"n": 4}` | 1 个账号 lease 下并发拆成 N 个独立 picture_v2 子任务,按实际成功张数聚合返回 |
| **多请求并发** | SDK 线程池同时发 K 个请求 | K 个账号 lease 并行,受限于账号池数 × `min_interval_sec` |
| **纯异步任务池** | `{"async": true}` 提交 + 轮询 | 后端 Worker 池消费,适合 1000+ 条 prompt 的大批量场景 |

**单请求多张(`n`)**:后端会在**同一账号 lease** 下拆成 N 个独立子任务并发执行,每个子任务目标 1 张图;最终按实际成功张数聚合返回,避免为单个请求占用多个账号。

**多请求并发(脚本示例)**:

```python
import concurrent.futures
from openai import OpenAI

client = OpenAI(base_url="https://your-domain.com/v1", api_key="sk-xxx")

prompts = ["..." for _ in range(100)]   # 100 条 prompt

def one(p):
    return client.images.generate(model="gpt-image-2", prompt=p, n=1, size="1024x1024")

with concurrent.futures.ThreadPoolExecutor(max_workers=32) as ex:
    for r in ex.map(one, prompts):
        print(r.data[0].url)
```

**账号池够大时,脚本端只需控制 `max_workers`,后端会自动按 `min_interval_sec` 给每个账号排队**,不会因为并发撞风控。

### 8.3 高性能高并发调度

#### 并发目标

| 场景 | 典型配置 | 能力 |
|------|---------|------|
| 图片生成(IMG2,账号池 100+) | `min_interval_sec=60` | **单机 >= 1000 并发图**(受账号池规模线性缩放) |
| 文字 SSE(沉睡中,见第一节) | `min_interval_sec=30` | 单机 >= 2000 并发 SSE |
| 下游 RPM/TPM 限流 | Redis 令牌桶 | 单 Key 5000 RPM 无压力 |

#### 调度核心参数(`configs/config.yaml → scheduler`)

```yaml
scheduler:
  min_interval_sec: 60          # 单账号最小间隔秒(对抗同号高频 → 429)
  daily_usage_ratio: 0.6        # 单号日配额消耗超过 60% 自动熔断下线
  lock_ttl_sec: 1200            # Redis 账号锁 TTL,lease 超时自动释放
  cooldown_429_sec: 600         # 连续 429 时该账号冷却时间
  warned_pause_hours: 24        # 收到"警告页"后的账号强制停用时长
```

#### 为什么能稳住高并发?

1. **串行 lease + Redis 锁**:每个账号同一时刻只有 1 个请求在飞,`min_interval_sec` 保证两次请求之间的最小间隔,风控曲线平滑;
2. **代理强绑定**:每个账号锁死一个代理,IP 指纹不混用,触发风控的只是个别账号,其它账号不受牵连;
3. **熔断自恢复**:账号消耗到阈值 / 收到 429 / 拿到警告页,自动进入冷却,冷却结束自动复活,无需人工干预;
4. **横向扩展**:`docker compose up --scale server=3` 即可多副本;Redis 锁天然跨节点,MySQL + backups 卷共享即可;
5. **观测友好**:`usage_logs` + `image_tasks` 两张表足以做任意维度(账号 / 用户 / 模型 / 时段)的下钻分析;后台「用量统计」已内置可视化。

#### 压测建议

- 用 `vegeta` / `wrk2` 对 `/v1/images/generations` 做恒定 QPS 压测,观察 `usage_logs.status` 分布;
- 对比调节 `min_interval_sec` 在 `30 / 60 / 90` 的成功率曲线,每批至少 500 样本;
- Redis `pool_size` 和 MySQL `max_open_conns` 生产都推荐至少 500,否则会成为瓶颈。

---

## 九、管理后台功能概览

| 页面 | 路径 | 核心能力 |
|------|------|---------|
| 个人总览 | `/personal/dashboard` | 积分余额、14 天请求趋势、热门模型、最近请求/账变 |
| 在线体验 | `/personal/play` | 浏览器内 Playground,文生图 / 图生图,实时扣费 |
| 接口文档 | `/personal/docs` | curl / Python SDK 代码片段、历史任务列表 |
| API Keys | `/personal/keys` | 创建 / 禁用 / 限流 Key |
| 使用记录 | `/personal/usage` | 本人的请求日志 / 积分流水 |
| 账单与充值 | `/personal/billing` | 套餐购买、易支付下单 |
| 用户管理 | `/admin/users` | 用户 CRUD、角色、状态、分组 |
| 积分管理 | `/admin/credits` | 手动调账、账变流水 |
| 充值订单 | `/admin/recharges` | 充值流水、套餐管理 |
| GPT 账号池 | `/admin/accounts` | JSON / AT / RT / ST 批量导入、刷新、探测、熔断 |
| 代理管理 | `/admin/proxies` | HTTP / SOCKS5、健康分探测 |
| 模型配置 | `/admin/models` | 对外 slug → 上游 slug 映射、每张图 / 每 1M token 计费 |
| 用户分组 | `/admin/groups` | 分组倍率(VIP / 内部 / 渠道) |
| 全局 Keys | `/admin/keys` | 跨用户管控所有下游 Key |
| 用量统计 | `/admin/usage` | 全站成功率 / Token / 积分收入 |
| 审计日志 | `/admin/audit` | 管理员所有写操作自动落审计 |
| 数据备份 | `/admin/backup` | `mysqldump` 一键备份 / 恢复 |
| 系统设置 | `/admin/settings` | 站点名 / 邮件 / 易支付 / 网关调度参数 |

---

## 十、目录结构

```text
gpt2api/
├── cmd/server/                 # 主入口
├── configs/                    # 配置示例
├── deploy/                     # Docker / compose / entrypoint / nginx
├── docs/                       # 补充文档
├── internal/                   # 后端核心(所有业务代码,Go 风格私有包)
│   ├── account/                  # 账号池:导入 / 刷新 / 探测 / DAO
│   ├── apikey/                   # 下游 Key
│   ├── audit/                    # 审计日志中间件
│   ├── auth/                     # 登录 / JWT
│   ├── backup/                   # 数据库备份 / 恢复
│   ├── billing/                  # 积分预扣 / 结算
│   ├── gateway/                  # OpenAI 兼容入口(chat / images / images_proxy)
│   ├── image/                    # 图片任务 Runner / 异步任务 / DAO
│   ├── middleware/               # CORS / JWT / Recover / RequestID / RateLimit
│   ├── model/                    # 模型配置(slug 映射 + 价格)
│   ├── proxy/                    # 代理池 + 健康分探测
│   ├── ratelimit/                # Redis 令牌桶
│   ├── rbac/                     # 权限常量
│   ├── recharge/                 # 充值 / 套餐 / EPay 对接
│   ├── scheduler/                # 账号调度器(核心)
│   ├── server/                   # Router 装配
│   ├── settings/                 # 动态系统设置
│   ├── upstream/chatgpt/         # ChatGPT 逆向客户端(sentinel / fchat / image / pow / headers)
│   ├── usage/                    # 请求日志 / 统计
│   └── user/                     # 用户模块
├── pkg/                        # 可被外部复用的纯工具包
├── sql/migrations/             # Goose 迁移
├── web/                        # 前端 Vue 3 源码
│   ├── src/
│   │   ├── api/                  # axios 封装
│   │   ├── config/               # feature flag(含 ENABLE_CHAT_MODEL)
│   │   ├── stores/               # pinia
│   │   ├── views/personal/       # 用户侧页面
│   │   ├── views/admin/          # 管理员页面
│   │   └── router/
│   └── dist/                     # 构建产物(Dockerfile 会 COPY 进镜像)
├── API_NOTES.md                # chatgpt.com 逆向接口备忘
├── RISK_AND_SAAS.md            # 风控 / 防封号原则
└── README.md                   # 当前文档
```

---

## 十一、二次开发 / 定制

### 后端

```bash
# 本机拉依赖
go mod tidy
# 跑迁移(先启 MySQL)
make migrate-up
# 本地热跑
make run
```

加一个新的后端接口:

1. `internal/xxx/handler.go` 加方法;
2. `internal/server/router.go` 注册路由;
3. 如果是写操作,配合 `audit.Middleware` 自动写审计。

### 前端

```bash
cd web
npm install
npm run dev          # http://localhost:5173,自动代理到 :8080
npm run build        # 构建到 web/dist/,供后端 SPA 路由挂载
```

**临时隐藏文字模型 UI**(默认已开启):

```ts
// web/src/config/feature.ts
export const ENABLE_CHAT_MODEL = false   // ← 改这里,重新 build
```

所有涉及文字入口的页面(在线体验 / 接口文档 / 用量统计 / 模型配置 …)会自动一起隐藏。

### 自定义模型 slug 映射

chatgpt.com 的实际上游 slug 会随账号等级微调(例如 Plus 用 `gpt-5-3`,免费用 `gpt-5`),映射表集中在 `internal/gateway/chat.go` 的 `mapUpstreamModelSlug`。图片模型的映射在 `admin/Models.vue` 的「模型配置」页面里可视化编辑。

---

## 十二、FAQ

<details>
<summary><b>Q1. 为什么要强制绑定代理?</b></summary>

`chatgpt.com` 对 IP × 账号 × TLS 指纹 × 设备指纹做联合风控。同一个出口 IP 跑多个账号,会被识别为同一批"多开"用户,整批被封。每个账号锁死一个代理是目前最稳的防封号方案。
</details>

<details>
<summary><b>Q2. IMG2 命中率偏低怎么办?</b></summary>

1. 确保账号是 Plus / Team,Free 账号不会进 IMG2 灰度;
2. 看 `image runner result summary` 的 `turns_used`,如果反复命中 `preview_only, retry`,说明账号本周没被灰度;
3. 把低命中号降级用于非关键业务,高命中号绑定关键客户。
</details>

<details>
<summary><b>Q3. 为什么 thinking 请求会严格排除 free 账号?</b></summary>

因为 `chatgpt-freeaccount` 对 thinking / premium 通路经常表现为"静默拒绝"或拿到 persona 后空转等待。当前版本对 thinking 模型做了两层硬门槛:

1. 调度阶段直接跳过已知 `plan_type=free` / `account_type=free` 的账号;
2. 运行期一旦 `chat-requirements` 返回 `chatgpt-freeaccount`,立刻回写 `plan_type=free`、释放租约、切下一个账号重试,不再走 5 分钟 429 冷却。

这样能显著减少 thinking 请求白等超时,也让 Playground 里的实时思考展示更稳定。
</details>

<details>
<summary><b>Q4. 部署后图片 403 / 图片刷不出来?</b></summary>

`chatgpt.com` 的 `estuary/content` 图片 CDN 有防盗链。本项目已内置带 HMAC 签名的图片代理(`/p/img/...`),所有返回给下游的 `image_urls` 都已经是**代理后的签名 URL**。如果你仍然拿到了 `chatgpt.com` 原始 URL,说明是老版本,拉取 main 分支重建即可。
</details>

<details>
<summary><b>Q5. MySQL / Redis 能用公有云托管吗?</b></summary>

可以。修改 `.env` / `configs/config.yaml` 的 DSN 与 `redis.addr` 即可。Redis 建议至少 Redis 7,且开启 AOF;MySQL 建议 8.0+,`max_connections >= 500`。
</details>

<details>
<summary><b>Q6. 如何横向扩展到多节点?</b></summary>

`docker compose up -d --scale server=3` + 前面挂 Nginx / Traefik 做 L7。Redis 分布式锁天然支持多副本;MySQL 和 JWT / AES 密钥统一即可;`backups` 卷改成共享存储(NFS / S3 fuse)。详见 [`deploy/README.md`](deploy/README.md#单节点-vs-多节点)。
</details>

<details>
<summary><b>Q7. 支持 ChatGPT Codex / GPT-5 / Claude / Gemini 吗?</b></summary>

当前聚焦 `chatgpt.com` 逆向(涵盖 GPT-5 / GPT-5-3 / gpt-image-2 / Codex 等)。Claude / Gemini 需要接入各自原生 API 或其对应的逆向客户端,不在当前仓库范围内。
</details>

---

## 十三、Roadmap

- [x] M1 骨架:配置 / 迁移 / 鉴权 / RBAC
- [x] M2 账号池 + 调度器
- [x] M3 生图 runner + IMG2 支持
- [x] M4 下游 Key 全特性(RPM/TPM/IP/模型白名单)
- [x] M5 积分钱包 + 易支付充值
- [x] M6 管理后台(Vue 3)
- [x] M7 风控熔断 + 图片签名代理
- [x] M8 IMG2 灰度命中 + 多图聚合
- [ ] M9 Turnstile solver 接入 → 恢复文字通路
- [ ] M10 图片任务大批量 Worker 池
- [ ] M11 账号分组(按灰度等级 / 地区分配)
- [ ] M12 Prometheus 指标 + Grafana 大盘

---

## 十四、参与贡献

欢迎 PR / Issue。提交前请:

1. `go vet ./... && go test ./...` 全绿;
2. `cd web && npm run build` 能通过;
3. Commit 用中文或英文均可,但请**明确写清楚动机**(是 fix 还是 feature,涉及哪个模块);
4. 涉及上游协议改动的 PR,请在 PR 描述里附上 HAR / curl 证据,不凭感觉改指纹。

**代码规范**:

- Go:标准 `gofmt`,包名小写;业务代码放 `internal/`,纯工具放 `pkg/`;
- Vue / TS:`vue-tsc --noEmit` 必须通过;文件名 PascalCase,组件 `<script setup lang="ts">`。

---

## 十五、免责声明与风险提示

> 本项目仅用于**技术学习与研究**。使用者应自行承担以下风险并遵守当地法律法规:

1. **账号封禁风险**:逆向 `chatgpt.com` 违反 OpenAI ToS,账号可能被限制 / 封禁,项目不对任何账号损失负责;
2. **法律合规风险**:在某些司法辖区绕过服务商地理限制 / 二次售卖 API 服务可能触犯相关法律,请使用者自行评估;
3. **上游变更风险**:chatgpt.com 协议随时可能调整,本项目无法保证 100% 可用,遇到问题请及时升级或通过 QQ 群反馈;
4. **数据安全**:生产环境务必修改默认密钥、开启 HTTPS、定期备份、限制后台 IP 访问;
5. **禁止用途**:禁止用于生成违法、暴力、色情、未成年人相关内容或以此从事诈骗 / 欺诈活动。

作者不保证项目适合任何特定用途,也不对任何直接或间接损失负责。**使用即视为同意本免责条款。**

---

## 十六、License

[MIT](LICENSE) © 2026 gpt2api contributors.

---

## 技术交流

- **QQ 群**:`382446`(入群请注明「gpt2api」/「github」)
- **Issue**:<https://github.com/432539/gpt2api/issues>
- **Discussions**:<https://github.com/432539/gpt2api/discussions>

如果这个项目对你有帮助,请给个 ⭐ Star;二次开发或商用遇到问题,欢迎进群交流。
