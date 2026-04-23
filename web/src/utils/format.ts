/**
 * 后端 credit 单位是"厘"(1 积分 = 10000 厘,方便做 token 等浮点成本)。
 * 展示时统一转成 2 位小数的"积分"。
 */
export const CREDITS_PER_UNIT = 10_000

export function formatCredit(v: number | null | undefined): string {
  if (v == null) return '0'
  const n = Number(v) / CREDITS_PER_UNIT
  if (!Number.isFinite(n)) return '0'
  return n.toFixed(2)
}

/** 单位:bytes → "xx.x MB" */
export function formatBytes(b: number | null | undefined): string {
  if (!b) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = b
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function formatDateTime(v: string | { Time: string; Valid: boolean } | null | undefined): string {
  if (!v) return '-'
  const s = typeof v === 'string' ? v : v.Valid ? v.Time : ''
  if (!s || s.startsWith('0001-')) return '-'
  // 后端通常返回 RFC3339,裁剪成 YYYY-MM-DD HH:mm:ss
  const d = new Date(s)
  if (isNaN(d.getTime())) return s
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/**
 * 紧凑时间格式:MM-DD HH:mm (不显示年份和秒,适合列表类展示)。
 * 跨年的时间仍然按月-日展示,这个在后台审计 / 账号池等场景够用。
 */
export function formatDateShort(v: string | { Time: string; Valid: boolean } | null | undefined): string {
  if (!v) return '-'
  const s = typeof v === 'string' ? v : v.Valid ? v.Time : ''
  if (!s || s.startsWith('0001-')) return '-'
  const d = new Date(s)
  if (isNaN(d.getTime())) return s
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** 后端 NullString/NullTime 的 Peel。兼容已经被序列化成裸字符串的情况。 */
export function nullVal(
  v: string | { Valid: boolean; String?: string; Time?: string } | null | undefined,
): string {
  if (v == null) return ''
  if (typeof v === 'string') return v
  if (!v.Valid) return ''
  return v.String ?? v.Time ?? ''
}

/**
 * 把网关 / 计费 / 上游返回的英文 error_code 翻译成中文提示。
 *
 * 英文 code 是稳定的机器标识(写进 DB / 返回给 OpenAI 客户端,不会改动),
 * 前端在展示时做一次映射,人类可读。
 */
const ERROR_CODE_LABEL: Record<string, string> = {
  // 计费/余额
  insufficient_balance: '积分不足',
  billing_error: '计费异常',

  // 限流
  rate_limit_rpm: '触发每分钟请求数限制 (RPM)',
  rate_limit_tpm: '触发每分钟 Token 限制 (TPM)',
  rate_limited: '请求过于频繁,稍后再试',
  rpm_limit: '触发每分钟请求数限制 (RPM)',
  tpm_limit: '触发每分钟 Token 限制 (TPM)',

  // 模型权限/类型
  model_not_allowed: '当前 Key 无权调用该模型',
  model_not_found: '模型不存在或已下架',
  model_disabled: '该模型已被禁用',
  model_type_mismatch: '模型类型不匹配(如对话模型用于生图)',

  // 请求校验
  invalid_request_error: '请求参数有误',
  invalid_request: '请求参数有误',
  unauthorized: '未授权或 API Key 无效',
  forbidden: '无权访问',
  key_disabled: 'API Key 已被停用',
  key_expired: 'API Key 已过期',
  quota_exceeded: 'API Key 配额已用完',

  // 功能未开启
  image_not_wired: '图片能力未接入,请联系管理员',
  feature_disabled: '该功能已被管理员关闭',

  // 上游
  upstream_error: '上游服务返回错误',
  upstream_timeout: '上游响应超时',
  upstream_unavailable: '上游暂不可用',
  upstream_image_not_returned: '上游本轮对话未返回图片结果',
  upstream_image_download_failed: '上游图片下载链接获取失败',
  thinking_not_triggered: 'thinking 模型本轮未触发思考过程,已判定生成失败,请重试',
  account_exhausted: '账号池暂无可用账号,请稍后重试',
  account_cooldown: '账号冷却中,请稍后重试',
  proxy_unhealthy: '代理不健康',

  // 其它
  internal_error: '服务器内部错误',
  canceled: '请求已取消',
}

/** 已翻译的错误码展示(未命中时原样返回,方便排障)。 */
export function formatErrorCode(code?: string | null): string {
  if (!code) return ''
  return ERROR_CODE_LABEL[code] || code
}
