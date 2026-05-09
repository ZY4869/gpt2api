<div align="center">

# gpt2api · KleinAI

**面向 GPT / GROK 双账号池的 OpenAI 兼容 AIGC 网关**

一站式覆盖文字、图片、视频生成，多账号池调度 · OpenAI 协议兼容 · 积分计费 · Docker 一键部署

[![Stars](https://img.shields.io/github/stars/432539/gpt2api?style=flat-square&logo=github&color=orange)](https://github.com/432539/gpt2api/stargazers)
[![Forks](https://img.shields.io/github/forks/432539/gpt2api?style=flat-square&logo=github&color=blue)](https://github.com/432539/gpt2api/network/members)
[![Issues](https://img.shields.io/github/issues/432539/gpt2api?style=flat-square&logo=github)](https://github.com/432539/gpt2api/issues)
[![Last Commit](https://img.shields.io/github/last-commit/432539/gpt2api?style=flat-square&logo=git&color=success)](https://github.com/432539/gpt2api/commits/main)
[![Release](https://img.shields.io/badge/release-v0.5.32-brightgreen?style=flat-square)](https://github.com/432539/gpt2api/releases)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![React](https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react)](https://react.dev/)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat-square&logo=docker)](https://docs.docker.com/compose/)

[功能特性](#-功能特性) · [快速部署](#-快速部署) · [API 兼容性](#-openai-兼容-api) · [配置说明](#-配置说明) · [更新日志](#-更新日志) · [Star 趋势](#-star-趋势)

</div>

---

## 项目简介

`gpt2api`（项目代号 KleinAI）是一个生产级的 **AIGC 聚合网关**，把 GPT / GROK 这类账号 + Cookie 体系的能力，整体封装成 **OpenAI 兼容协议**，让任何按 OpenAI SDK 编写的程序都可以无缝接入。

平台同时提供：

- 一套面向终端用户的创作前台（图 / 文 / 视频）
- 一套面向运营的管理后台（账号池、代理、计费、CDK、日志）
- 一套对外暴露的 OpenAI 兼容 HTTP 接口

适用场景：私有化 AIGC 服务、白标 SaaS、多账号合规分发、内部团队调用聚合。

> 当前默认版本：`v0.5.32`，作为当前 fork 的发布线；上游 `v2.x` 仍保留为新架构参考基线。

## ✨ 功能特性

### 创作能力

| 能力 | OpenAI 兼容路由 | 说明 |
|------|----------------|------|
| 文字对话 | `POST /v1/chat/completions` | 支持流式 / 非流式输出 |
| 文生图 | `POST /v1/images/generations` | 支持批量出图、`gpt-image-2` 稳定通道 |
| 图生图 | `POST /v1/images/edits` | 支持参考图、Mask |
| 图片任务查询 | `GET /v1/images/generations/:task_id` | 异步任务进度 / 结果回查 |
| 文 / 图生视频 | `POST /v1/video/generations` | 支持 `quality=standard\|hd`，对应 720p / 1080p |
| 视频任务查询 | `GET /v1/video/generations/:task_id` | 异步任务进度 / 结果回查 |
| 模型列表 | `GET /v1/models` | 由后端模型表统一暴露，可在管理后台维护 |

### 调度与稳定性

- **多账号池**：GPT / GROK 账号批量导入、健康检测、自动刷新、熔断、轮换
- **代理池**：批量导入（`scheme://user:pass@host:port#name`）、批量删除、批量测试
- **代理策略**：账号级绑定优先，全局回落支持「固定代理」与「随机代理」两种模式
- **请求观测**：上游全链路日志可追踪，失败任务可看到完整 provider 报文
- **统一计费**：积分制，按模型 / 分辨率 / 时长可配置

### 运营能力（管理后台）

- 仪表盘、Token（账号）管理、代理管理、用户管理、充值消费
- 优惠码、CDK 兑换、模型价格、系统配置、请求日志、上游日志
- 所有配置尽量表单化，避免裸 JSON 手填

## 🏗️ 技术栈

| 层级 | 选型 |
|------|------|
| 后端 | Go 1.24 · Gin · GORM · MySQL · Redis |
| 前端 | React 18 · Vite · TypeScript · TailwindCSS · pnpm Workspace |
| 部署 | Docker · Docker Compose · Nginx · Caddy（可选） |
| 外部依赖 | FlareSolverr · 代理池 · 对象存储（可选） |

```
┌────────────┐    ┌────────────┐    ┌────────────────────┐
│  用户前台  │    │  管理后台  │    │ OpenAI 兼容 SDK 客户端 │
└─────┬──────┘    └─────┬──────┘    └──────────┬─────────┘
      │ :17080          │ :17088               │ :17200
      ▼                 ▼                      ▼
┌────────────────────────────────────────────────────────┐
│   Nginx / Caddy    （SSL · 反代 · 限流 · 静态资源）       │
└──────┬──────────────┬──────────────────┬───────────────┘
       │              │                  │
   ┌───▼────┐    ┌────▼────┐         ┌───▼─────┐
   │ user-api│    │admin-api│         │openai-api│   ← Go 多服务
   └───┬────┘    └────┬────┘         └───┬─────┘
       └──────┬───────┴──────┬───────────┘
              │              │
        ┌─────▼─────┐  ┌─────▼─────┐
        │   MySQL   │  │   Redis   │
        └───────────┘  └───────────┘
```

## 🚀 快速部署

### 环境要求

- Linux / macOS / Windows（推荐 Linux）
- Docker 24+ 与 Docker Compose v2
- 1 个域名或 3 个子域名（生产部署推荐）
- 80 / 443 端口可用

### 1. 拉取代码

```bash
git clone https://github.com/432539/gpt2api.git
cd gpt2api
```

### 2. 配置环境变量

```bash
cp deploy/env/.env.example deploy/env/.env.prod
# 编辑 .env.prod，重点检查：
#   - 数据库 / Redis 连接
#   - JWT_SECRET / AES_KEY（务必修改！）
#   - 域名 / CORS 来源
#   - GPT / GROK 上游基础地址
#   - 代理 / FlareSolverr 地址
```

### 3. 启动服务

```bash
cd deploy
docker compose -f docker-compose.server.yml up -d --build
```

### 4. 检查状态

```bash
docker compose -f docker-compose.server.yml ps
docker logs -f klein-api
docker logs -f klein-admin
docker logs -f klein-openai
docker logs -f klein-worker
```

### 5. 默认入口

| 入口 | 地址 |
|------|------|
| 用户前台 | `http(s)://your-domain:17080` |
| 管理后台 | `http(s)://your-domain:17088` |
| OpenAI 兼容 API | `http(s)://your-domain:17200/v1` |

## 🧩 OpenAI 兼容 API

直接把 OpenAI SDK 的 `base_url` 指向本服务即可：

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://your-domain:17200/v1",
    api_key="sk-xxxxxxxx",  # 在用户前台「密钥」页生成
)

# 文字对话
resp = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "你好"}],
)

# 图片生成
img = client.images.generate(
    model="gpt-image-2",
    prompt="一只在京都樱花树下的赛博狐狸，电影质感",
    size="1024x1024",
    n=4,
)

# 视频生成（v0.5.6 起默认 720p / 1080p）
import httpx
r = httpx.post(
    "https://your-domain:17200/v1/video/generations",
    headers={"Authorization": "Bearer sk-xxxxxxxx"},
    json={
        "model": "grok-video",
        "prompt": "在雨夜霓虹中漫步的猫",
        "aspect_ratio": "16:9",
        "quality": "hd",      # standard=720p, hd=1080p
        "duration": 6,
    },
    timeout=600,
)
print(r.json())
```

## 📦 默认端口

### 对外端口

| 端口 | 用途 |
|------|------|
| `17080` | 用户前台 |
| `17088` | 管理后台 |
| `17200` | OpenAI 兼容 API |

### 本机调试端口

| 端口 | 用途 |
|------|------|
| `17180` | 用户后端 API |
| `17188` | 管理后台 API |
| `17200` | OpenAI 兼容 API |
| `23306` | MySQL（避开 Hyper-V 占用） |
| `16379` | Redis |
| `18191` | FlareSolverr |

## ⚙️ 配置说明

### 全局代理策略

在「管理后台 → 系统配置」中：

- `proxy.global_enabled`：是否启用全局代理回落
- `proxy.selection_mode`：`fixed`（固定代理） / `random`（随机代理）
  - `fixed` 模式下需要在 `proxy.global_id` 指定一个代理
  - `random` 模式下每次任务从启用代理中随机挑一个
- 账号级绑定的 `proxy_id` 始终优先于全局策略

### Token 账号管理

- 支持 GPT / GROK 双 Provider，导入时按行解析
- 导入后会自动针对 GROK Cookie 账号触发探测，识别账号类型（`basic / super / heavy`）并回填到列表
- 列表支持按「账号类型」过滤
- 支持批量绑定代理：`single`（多账号绑同一代理） / `cycle`（多账号轮询绑定多个代理）

## 🛠️ 本地开发

### 方式 A：全容器（零本地依赖）

```bash
cd deploy
docker compose -f docker-compose.dev-full.yml up -d --build
```

### 方式 B：半容器（热更新）

```powershell
# 仅 MySQL / Redis 走容器
pwsh ./scripts/dev-up.ps1

# 前端
cd frontend
pnpm install
pnpm --filter @kleinai/user  dev    # http://localhost:5173
pnpm --filter @kleinai/admin dev    # http://localhost:5174
```

> 默认 `KLEIN_PROVIDER_GPT/GROK=mock`，无需真实凭证即可走通生成全流程。
> 切真实通道：编辑 `deploy/docker-compose.dev-full.yml` 或 `backend/.env.local`，把 `KLEIN_PROVIDER_*=real`。
> 真实凭证一律通过「管理后台 → Token 管理」入库，AES-256-GCM 落盘。

## 🏭 生产建议

- 前台 / 后台 / OpenAI API 分子域名部署，结构更清晰
- 管理后台建议在 Nginx 层加 IP 白名单
- OpenAI 兼容接口建议独立子域并启用限流
- 80 / 443 由 Caddy / Nginx 统一接管 SSL
- 图片 / 视频素材建议落 OSS 或本地缓存，避免直接暴露上游地址
- 定期清理 `storage.history_retention_days` 与 `storage.result_retention_days` 控制磁盘

## 📚 文档

- [开发规范 - 总览](docs/01-开发规范-总览.md)
- [后端规范](docs/02-后端规范.md)
- [数据库设计](docs/03-数据库设计.md)
- [API 规范](docs/04-API规范.md)
- [前端规范](docs/05-前端规范.md)
- [部署与运维规范](docs/06-部署与运维规范.md)

## 📝 更新日志

详见 [PROGRESS.md](./PROGRESS.md)。

### v0.5.32（2026-05-09）

- 修复 GPT Web 图片下载链路在遇到 `download_url` 包装 JSON 时误把元信息当成图片内容缓存的问题
- 现在会继续追包装里的下一跳下载地址，直到拿到真实图片字节，避免 `/v1/files/:task_id/:seq` 拉回 412B 伪 `.png`
- 新增 provider 单测，覆盖包装下载地址提取和相对下载地址归一化，便于继续验证 10 张套图真实回收

### v0.5.31（2026-05-09）

- 修复 `wait_all_then_download` 严格模式轮询退出条件过严的问题，避免 `10/10` 候选图已收齐时仍无限停留在 `web.poll.*`
- 现在保持“必须整套收齐”不变，但允许在 authoritative 顺序迟迟不稳定时，按“当前稳定顺序”或“首次完整顺序”进入统一下载
- 新增严格模式终止判定单测，覆盖“不足整套不可退出”“当前顺序稳定可退出”“首次完整顺序超额等待后可退出”三条主链路

### v0.5.30（2026-05-09）

- 保留 `wait_all_then_download` 的“必须收齐整套图后再统一下载”规则，不放宽为部分成功
- 当 `10/10` 候选图已收齐、但 authoritative final order 未稳定时，允许按“当前稳定顺序”或“首次完整顺序”继续统一回收，不再直接失败
- 新增对应 provider 单测，覆盖当前稳定顺序回退和首次完整顺序回退，便于继续验证 10 张儿童数字绘本套图链路

### v0.5.29（2026-05-09）

- 修复生产 `docker-compose.yml` 中 `healthcheck.test` 的数组语法不兼容问题
- 将 API 健康检查改为标准 `["CMD", "/app/healthcheck", "http://127.0.0.1:17180/healthz"]` 写法，兼容当前 Docker Compose 解析要求
- 保持 distroless 原生 healthcheck 方案不变，仅修正 compose 编排语法

### v0.5.28（2026-05-09）

- 修复生产 `klein-api` 容器在服务实际已启动时仍被 Docker 标记为 `unhealthy` 的问题
- 根因是 backend 生产镜像采用 `distroless`，原 healthcheck 依赖 `sh / wget / grep`，在镜像内不存在
- 新增原生 Go `healthcheck` 二进制并打包进 backend 镜像，生产 compose 直接调用它检查 `/healthz`

### v0.5.27（2026-05-09）

- 修复 `gpt-image-2` Web 测试模式任务在上游失败、后台 panic 或异常退出后仍可能长期停留在 `running` 的问题
- 后台执行链路新增 panic recover，并统一走任务失败收口与退款逻辑，避免 OpenAI 查询、用户历史、管理后台看到不一致终态
- 新增全局 stale task reaper：服务启动即 sweep 一次，之后每分钟自动回收超时未终态任务；任务查询前也会做一次即时自愈校正

### v0.5.26（2026-05-08）

- 为 GPT Web 多图测试模式轮询链路补齐子请求短超时，避免单个 ChatGPT Web 接口长时间卡住导致任务一直停在 `running`
- 新增 `web.poll.conversation`、`web.poll.library`、`web.poll.resolve` 细粒度 upstream log，便于直接定位卡在 conversation、library 还是下载地址解析阶段
- 配套新增 provider 单测，锁定轮询子步骤的短超时行为，便于继续追踪 `wait_all_then_download` 单对话套图链路

### v0.5.25（2026-05-08）

- 修复 `wait_all_then_download` 测试模式的 provider 内部轮询窗口仍固定为 `9 分钟` 的问题，现已与外层超时策略对齐为 `30 分钟`
- 新增 provider 侧超时单测，确保 GPT Web 多图测试模式确实会持续等待更长时间，再判断是否收齐整套图和最终顺序
- 解决“外层 30 分钟、内层 9 分钟提前失败”这一超时不一致问题，便于继续验证单对话套图最终排序链路

### v0.5.24（2026-05-08）

- 修复 `Release Build` 在 GitHub Actions 上的 Go 单测顺序漂移问题，避免 `content_references_by_file` 等 metadata 字段因 map 遍历顺序不同导致 CI 偶发失败
- `gpt-image-2` Web 图片 metadata 提取改为按固定字段顺序收集资产 ID，减少不同运行环境下的顺序抖动
- 保持多图测试模式与正式链路行为一致，同时让 tag 构建和发布流程重新稳定

### v0.5.23（2026-05-08）

- `gpt-image-2` ChatGPT Web 多图链路新增隐藏测试模式：`params.web_test_mode = "wait_all_then_download"`，用于严格验证“整套完成后再统一下载”的回收流程
- 测试模式下，同一 conversation 必须先收齐候选图、拿到完整且连续稳定 2 轮的上游最终顺序，之后才按最终 `seq` 统一下载、统一落库、统一返回
- 测试模式超时统一提升到 `30 分钟`，若最终顺序始终不完整或最终下载后图片数不足，则任务直接失败，不再提前返回部分结果或回退旧逻辑

### v0.5.22（2026-05-08）

- `gpt-image-2` ChatGPT Web 多图链路改为严格单对话模式：一套图只允许创建 1 个上游 conversation，并且只从这 1 个 conversation 拉回整套图片
- Web 路由默认强制使用 thinking 模型，忽略非 thinking 的 `web_model` 覆盖，避免套图任务退回非思考模型导致稳定性下降
- 多图 prompt 明确追加“同一对话一次性生成整套图”的约束；当单个 conversation 最终不足额时，任务明确失败，不再偷偷开第二个对话拼图

### v0.5.21（2026-05-08）

- 继续修复 `gpt-image-2` ChatGPT Web 多图顺序：不再在“收齐图片后立刻定序”，而是进入额外 settle 阶段，尽可能等待上游 Web 最终展示顺序稳定后再落库
- 候选图聚合从“按 URL”升级为按 `file_id / sediment_id / normalized download url / data hash` 归并，减少同一张图被不同来源重复识别导致的顺序漂移
- 新增 authoritative order 提取与对应 Go 单测，优先采用会话消息中 `attachments / citations / conversation_context_citation_metadata / content.parts` 的数组顺序，拿不到时再回退到当前稳定顺序

### v0.5.20（2026-05-08）

- 重构 `gpt-image-2` ChatGPT Web 多图聚合流程：从“边下载边定序”改为“先收集候选结果，再统一排序后一次性生成最终结果”
- 明确多图排序优先级：`direct output` 顺序优先，其次 `file_id` / `sediment_id` 首次出现顺序，最后才兜底到下载成功顺序
- 新增对应 Go 单测，覆盖 direct output 优先、resolved fallback、去重和下载先后不影响最终返回顺序

### v0.5.19（2026-05-08）

- 优化 `gpt-image-2` ChatGPT Web 多图结果的保存顺序：优先按模型原始输出里出现的图片顺序落库，而不是按后续轮询或下载完成先后排序
- 保持 OpenAI 兼容 `/v1/files/:task_id/:seq` 与 `data[]` 返回顺序一致，更接近真正的连续套图顺序
- 新增对应 Go 单测，覆盖 direct output 顺序优先和结果合并顺序

### v0.5.18（2026-05-08）

- 修复 OpenAI 兼容图片结果 URL 丢失端口的问题：反代补齐 `X-Forwarded-Host`，`/v1/files/*` 返回地址现在能正确带上 `:17200`
- 修复 `gpt-image-2` ChatGPT Web 多图结果重复累计的问题，避免同一张图被重复写成一组 `10` 张
- 补齐对应 Go 单测，覆盖 OpenAI 结果地址组装和 Web 图片去重逻辑

### v0.5.7（2026-05-07）

- 图片生成 / 图片编辑数量上限从 `4` 提升到 `10`，覆盖用户前台、站内 API 与 OpenAI 兼容 `/v1/images/*`
- `gpt-image-2` 大数量任务补齐超时策略：`count > 4` 时总超时提升到 `30 分钟`，Web 轮询受上下文剩余时间约束
- 用户前台补齐多图浏览体验：数量选择统一 `1..10`，历史卡片显示张数，预览弹层支持整组图片左右切换
- OpenAI 兼容文档补充 `n / count` 支持 `1-10`，并注明 `5-10` 张建议使用 `async=true`

### v0.5.6（2026-05-06）

- 修复 视频生成默认仍走 `480p` 的问题，默认改为 `1080p`，并补齐 `quality = standard | hd` 入参（720p / 1080p），保留更高分辨率扩展位
- 代理管理 补齐批量导入（按行解析）、批量删除、批量并发测试（信号量并发 4）
- Token 管理 新增账号类型展示与按类型过滤（`basic / super / heavy`），导入后自动并发探测识别并回填
- Token 管理 新增批量代理分配：`single`（多对一）与 `cycle`（多对多轮询）
- 系统配置 新增「随机代理」模式，每次任务从启用代理中随机挑选
- 上游日志、生成链路保持兼容，无破坏性 schema 变更

## 🌟 Star 趋势

[![Star History Chart](https://api.star-history.com/svg?repos=432539/gpt2api&type=Date)](https://star-history.com/#432539/gpt2api&Date)

如果这个项目对你有帮助，欢迎点一个 ⭐ 支持持续更新。

## 💬 社区与交流

- GitHub Issues：[https://github.com/432539/gpt2api/issues](https://github.com/432539/gpt2api/issues)
- 技术交流 QQ 群：`382446`
- 官网：[www.gpt2api.com](https://www.gpt2api.com/)

## 📜 开源协议

仓库代码用于 **学习与研究**。商用前请自行评估上游服务条款与所在地区的合规风险，作者不对使用本项目造成的任何后果负责。

---

<div align="center">
  <sub>Built with ❤️ for the AIGC community.</sub>
</div>
