# 开发进度 / Changelog

> 仅记录对外可见的能力变更与里程碑；项目内部规范文档见 `docs/`。

---

## v0.5.28 — 2026-05-09

### 生产健康检查修复

- 修复生产 `klein-api` 容器在服务正常启动时仍被 Docker 标记为 `unhealthy` 的问题
- 根因是后端生产镜像使用 `distroless`，原有 `healthcheck` 依赖 `sh / wget / grep`，在镜像内不可用
- 新增原生 Go `healthcheck` 二进制并打包进 backend 镜像，生产 compose 改为直接调用它检查 `/healthz`

---

## v0.5.27 — 2026-05-09

### GPT 图片任务终态修复

- 修复 `gpt-image-2` Web 测试模式在上游已失败、后台 panic 或 goroutine 提前退出后，任务仍长期停留在 `queued / running` 的问题
- 为图片 / 视频后台执行入口补齐 panic recover，并统一走失败收口与退款逻辑，避免 OpenAI 查询、用户历史、管理后台看到不一致状态
- 新增全局 stale task reaper，启动即 sweep 一次，之后每分钟自动回收超时未终态任务；同时在任务查询链路加单任务自愈校正

---

## v0.5.26 — 2026-05-08

### GPT Web 轮询可观测性

- 为 GPT Web 多图测试模式轮询子请求增加短超时，避免单个上游接口卡住几分钟把整单吊死在 `running`
- 新增 `web.poll.conversation / web.poll.library / web.poll.resolve` 细粒度日志，直接暴露卡点
- 新增 provider 单测，锁定轮询子步骤的短超时行为

---

## v0.5.25 — 2026-05-08

### GPT Web 测试模式超时

- 修复 `wait_all_then_download` 测试模式 provider 内部仍按 `9 分钟` 轮询的错误，现已对齐到 `30 分钟`
- 新增 provider 侧超时单测，锁定 GPT Web 多图测试模式的实际等待窗口
- 避免“任务总超时 30 分钟，但 provider 9 分钟提前失败”的不一致行为

---

## v0.5.24 — 2026-05-08

### CI 与发布稳定性

- 修复 `Release Build` 在 GitHub Actions 上的 Go 单测顺序漂移问题，避免 metadata map 遍历顺序不同导致发布构建失败
- `gpt-image-2` Web 图片 metadata 资产提取改为固定字段顺序，减少不同运行环境下的结果顺序抖动
- 保持多图测试模式逻辑不变，同时恢复 tag 发布流水线稳定性

---

## v0.5.23 — 2026-05-08

### GPT 套图测试模式

- `gpt-image-2` ChatGPT Web 多图链路新增隐藏测试模式：`params.web_test_mode = "wait_all_then_download"`
- 测试模式下不再在轮询阶段下载普通图片 URL，而是持续等待同一 conversation 中整套图和 authoritative final order 完整且稳定后，再按最终顺序统一下载
- 若 `30` 分钟内最终顺序仍不完整、不稳定，或统一下载后唯一图片数不足请求数量，则任务直接失败，不回退旧逻辑

---

## v0.5.22 — 2026-05-08

### GPT 单对话套图

- `gpt-image-2` ChatGPT Web 多图任务改为严格单对话模式：一套图只创建 1 个上游 conversation，并且只从这同一个 conversation 拉回结果
- Web 路由默认强制使用 thinking 模型，非 thinking 的 `web_model` 覆盖将被忽略，避免套图任务退回普通模型
- 多图 prompt 明确要求“同一对话一次性生成整套图片”；若单个 conversation 最终不足额，任务明确失败，不再自动补开第二个 conversation 拼图

---

## v0.5.21 — 2026-05-08

### GPT 套图顺序

- 继续修复 `gpt-image-2` ChatGPT Web 多图顺序：收齐图片后增加 settle 阶段，尽可能等待上游 Web 最终展示顺序稳定后再定 `seq`
- 候选图聚合从按 URL 升级为按 `file_id / sediment_id / normalized download url / data hash` 归并，减少重复候选造成的顺序漂移
- 新增 authoritative order 提取与对应 Go 单测，优先采用会话消息中 `attachments / citations / conversation_context_citation_metadata / content.parts` 的数组顺序，拿不到时回退到当前稳定顺序

---

## v0.5.20 — 2026-05-08

### GPT 套图顺序

- 重构 `gpt-image-2` ChatGPT Web 多图聚合流程：从“边下载边定序”改为“先收集候选结果，再统一排序后一次性生成最终结果”
- 明确多图排序优先级：`direct output` 顺序优先，其次 `file_id` / `sediment_id` 首次出现顺序，最后才兜底到下载成功顺序
- 新增对应 Go 单测，覆盖 direct output 优先、resolved fallback、去重和下载先后不影响最终返回顺序

---

## v0.5.19 — 2026-05-08

### GPT 套图顺序

- 优化 `gpt-image-2` ChatGPT Web 多图结果的保存顺序：优先按模型原始 direct output 里出现的图片顺序落库
- 避免结果顺序被后续轮询附件 URL 或下载完成先后打乱，让 `/v1/files/:task_id/:seq` 更接近真正的套图顺序
- 新增对应 Go 单测，覆盖 direct output 顺序优先和结果 URL 合并顺序

---

## v0.5.18 — 2026-05-08

### OpenAI 兼容图片结果

- 修复 OpenAI 兼容图片结果 URL 丢失端口的问题：反代补齐 `X-Forwarded-Host`，后端优先使用转发头拼接 `/v1/files/*` 返回地址
- 修复 `gpt-image-2` ChatGPT Web 多图结果重复累计的问题，避免同一张图被轮询重复收集后错误写成一整套
- 新增对应 Go 单测，覆盖 OpenAI 结果地址组装和 Web 图片去重逻辑

---

## v0.5.15 — 2026-05-07

### GPT 图片链路

- 修复 `gpt-image-2` ChatGPT Web 出图结果回收失败的问题：补齐从会话消息 `metadata.attachments / citations` 提取 `file_id`
- 补齐 ChatGPT Web 图片下载地址识别：支持 `/backend-api/estuary/content?id=file_...` 这类新下载链路
- 新增对应 Go 单测，覆盖附件型结果和 `estuary` 下载地址提取

---

## v0.5.7 — 2026-05-07

### 图片生成

- 图片生成 / 图片编辑数量上限从 `4` 提升到 `10`，统一覆盖用户前台、站内 `/api/v1/gen/image` 与 OpenAI 兼容 `/v1/images/generations`、`/v1/images/edits`
- `gpt-image-2` 大数量任务补齐超时策略：`count > 4` 时总超时提升到 `30 分钟`，Web 路径轮询不再固定写死 `9 分钟`

### 前台体验

- 图片数量选择统一为 `1..10`
- 工作台历史卡片新增多图张数标记，图片预览弹层支持整组结果左右切换，而不是只看首图
- 旧生图页可直接展示 `10` 个占位和 `10` 张返回结果

### 文档与回归

- OpenAI 兼容文档补充 `n / count` 支持 `1-10`，并注明 `5-10` 张建议使用 `async=true`
- 新增数量上限与超时 helper 的 Go 单测，覆盖 `10` 放行、`11` 拒绝和大数量任务超时策略

---

## v0.5.6 — 2026-05-06

### 发布与稳定性

- 修复 fork 新架构发布流水线：补齐适配 `backend/ + frontend/` 目录结构的 `Release Build`
- 修复 provider 瞬态网络错误重试：`EOF`、`connection reset`、`broken pipe`、`connection refused` 现在会被当作可重试错误处理

---

## v2.0.1 — 2026-05-04

### 视频生成

- 修复 默认分辨率仍按 `480p` 下发的问题：常量 `defaultVideoResolution` 改为 `1080p`，`defaultVideoSize` 改为 `1920x1080`
- 新增 `quality` 入参，`standard / draft → 720p`、`hd → 1080p`，并按 `aspect_ratio`（`16:9 / 9:16 / 1:1`）正确推导宽高
- 兜底宽高 由硬编码 `1280x720` 改为 `videoConfig` 推导出来的默认宽高，保留后续接入 4K 等更高分辨率的扩展位

### 代理管理

- 新增 `POST /admin/api/v1/proxies/import` 批量导入：按行解析 `scheme://user:pass@host:port#name`，密码 AES-256-GCM 落盘
- 新增 `POST /admin/api/v1/proxies/batch-delete` 批量软删除
- 新增 `POST /admin/api/v1/proxies/batch-test` 批量测试，信号量并发 4，返回 `tested / ok / failed`
- 前端 `ProxiesPage` 重写：批量导入 / 批量删除 / 批量测试 / 多选交互

### Token（账号）管理

- 列表新增 `plan_type` 过滤项（`basic / super / heavy`），通过 `oauth_meta` JSON 字段查询
- 导入后自动并发探测（信号量并发 4）GROK Cookie 账号，识别 plan 类型后回填，导入结果新增 `detected / pending / failed` 字段
- 新增 `POST /admin/api/v1/accounts/batch-assign-proxy` 批量代理分配：
  - `mode = single`：所有选中账号绑定到同一个 `proxy_id`
  - `mode = cycle`：按 `idx % len(proxy_ids)` 轮询绑定到 `proxy_ids` 列表
- 前端 `TokenAccountsPage` 重写：账号类型列、按类型过滤、批量代理分配弹窗、导入结果回显

### 系统配置

- 新增 `proxy.selection_mode` 全局代理选择模式：`fixed`（固定代理） / `random`（随机代理）
- `random` 模式下，每次任务通过 `crypto/rand` 从启用代理列表中随机挑一个
- 账号级 `proxy_id` 仍始终优先于全局策略
- 前端 `ConfigPage` 新增「全局代理模式」下拉与说明文案

### 其他

- 清理界面与文档中暴露给最终用户的源码入口与品牌点
- 修复 `resolveProxyURL` 在 `account_test_service` 与 `generation_service` 中的重复实现，统一走 `proxySvc`

---

## v2.0.0 — 2026-04-27

- 统一文字、图片、视频三条生成链路
- 统一账号池、代理、刷新、熔断、轮换、用量检测
- 统一 OpenAI 兼容 API（`/v1/chat/completions`、`/v1/images/*`、`/v1/video/*`、`/v1/models`）
- 统一管理后台：用户、账单、CDK、优惠码、模型价格、请求日志、上游日志
- 统一部署：Docker Compose 一键拉起，可平滑迁移到 K8s

---

## v1.0.x

历史稳定基线，仅作为对照保留，新需求不再回灌。

---

## 当前已具备模块

| 模块 | 状态 | 备注 |
|------|------|------|
| 后端 API / Admin / OpenAI / Worker | ✅ | 4 个 cmd 二进制 + healthz / readyz |
| GPT / GROK 账号池 | ✅ | 批量导入 · 自动探测 · 熔断 · 轮换 |
| 代理池 | ✅ | 批量导入 · 批量测试 · 固定 / 随机回落 |
| 系统配置中心 | ✅ | OAuth 刷新窗口 · 代理策略 · 数据保留 |
| 用户前台 | ✅ | 文 / 图 / 视频 · 历史 · 密钥 · 账户 |
| 管理后台 | ✅ | Token / 代理 / 用户 / 计费 / CDK / 日志 |
| OpenAI 兼容层 | ✅ | chat / images / video / models |
| 计费体系 | ✅ | 积分 · 充值 · CDK · 模型价格 |
| 部署体系 | ✅ | Docker Compose 单机 · 反代 · SSL |

## 后续路线

- 前端图片 / 视频任务面板暴露 `quality` 选项与 4K 选项预留
- 上游日志按账号 / 代理维度的聚合视图
- 限流策略表单化（当前部分仍在配置文件）
- K8s Helm Chart 预研
