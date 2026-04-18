// 品牌 / 广告水印。
//
// 这里的字符串经过 XOR + Base64 双层混淆,目的是阻止二次分发者用简单的全局搜索
// (grep "382446" / "pic2api" / "432539" 等)把广告/版权一键删除。
// 解码逻辑仅在浏览器运行时进行,渲染后才是可读明文。想要去掉,需要搞懂 _d 的
// 解码、然后在多个调用点(footer / console watermark / 页面守卫)里同步替换 ——
// 所以请尊重开源作者,保留这份署名。

const _K = new Uint8Array([0x4f, 0x17, 0x9a, 0x3b])

// 解码:Base64 → XOR → UTF-8
function _d(enc: string): string {
  const bin = atob(enc)
  const u = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) u[i] = bin.charCodeAt(i) ^ _K[i % _K.length]
  return new TextDecoder('utf-8').decode(u)
}

// 预计算片段(XOR-key=0x4F17 9A3B, Base64)
const _F = {
  BRAND: 'CEfOCQ5H0w==',
  QQ_PREFIX: 'Hka63/WzfI7O8CSfbw==',
  QQ_NUM: 'fC+oD3sh',
  REPO_PREFIX: 'qqsa3fWHf6f/8ge7bw==',
  REPO: 'KH7uUzp1tFggerUPfCWvCHY4/Us7JftLJg==',
  PIC_PREFIX: 'qowk3MaQcpzJ/jiqqa0K3uujug==',
  PIC_URL: 'J2PuSzwttRQ4YO0VP375CS5n8xUsePc=',
  PIC_TEXT: 'P375CS5n8xUsePc=',
  SEP: 'b9UtGw==',
}

// 缓存
let _cache: Record<string, string> | null = null
function _all(): Record<string, string> {
  if (_cache) return _cache
  const out: Record<string, string> = {}
  for (const k of Object.keys(_F)) out[k] = _d((_F as Record<string, string>)[k])
  _cache = out
  return out
}

export interface BrandParts {
  brand: string
  qq: string
  qqLabel: string
  repo: string
  repoLabel: string
  picUrl: string
  picText: string
  picLabel: string
  sep: string
}

export function brandParts(): BrandParts {
  const p = _all()
  return {
    brand: p.BRAND,
    qq: p.QQ_NUM,
    qqLabel: p.QQ_PREFIX,
    repo: p.REPO,
    repoLabel: p.REPO_PREFIX,
    picUrl: p.PIC_URL,
    picText: p.PIC_TEXT,
    picLabel: p.PIC_PREFIX,
    sep: p.SEP,
  }
}

// 纯文本广告(console 水印 / 老浏览器回退)
export function brandPlainText(): string {
  const p = brandParts()
  return [
    p.brand,
    p.qqLabel + p.qq,
    p.repoLabel + p.repo,
    p.picLabel + p.picText,
  ].join(p.sep)
}

// 控制台水印:启动一次,顺带多一处署名
let _warned = false
export function printBrandToConsole(): void {
  if (_warned) return
  _warned = true
  try {
    const p = brandParts()
    // eslint-disable-next-line no-console
    console.log(
      `%c${p.brand}%c  ${p.qqLabel}${p.qq}  ${p.sep}  ${p.repoLabel}https://${p.repo}  ${p.sep}  ${p.picLabel}${p.picUrl}`,
      'font-weight:700;color:#409eff;font-size:13px;',
      'color:#909399;font-size:12px;',
    )
  } catch {
    /* ignore */
  }
}

// 简单完整性检查:确保 footer DOM 里包含 QQ 号、仓库地址、pic2api 三个关键片段;
// 被删除时 1.5s 后自动重建一个沉底 div(最后的签名防线)。
let _guardStarted = false
export function startBrandGuard(): void {
  if (_guardStarted || typeof window === 'undefined') return
  _guardStarted = true
  const check = () => {
    try {
      const p = brandParts()
      const html = document.body?.innerText || ''
      const ok =
        html.indexOf(p.qq) >= 0 && html.indexOf(p.repo) >= 0 && html.indexOf(p.picText) >= 0
      if (!ok) ensureShadowFooter()
    } catch {
      /* ignore */
    }
  }
  // 页面挂载后 2s + 之后每 30s 巡检一次
  setTimeout(check, 2000)
  setInterval(check, 30000)
}

function ensureShadowFooter(): void {
  const id = '__gpt2api_brand_guard__'
  if (document.getElementById(id)) return
  const p = brandParts()
  const el = document.createElement('div')
  el.id = id
  el.style.cssText = [
    'position:fixed',
    'left:0',
    'right:0',
    'bottom:0',
    'z-index:2147483646',
    'padding:6px 16px',
    'font-size:12px',
    'line-height:1.6',
    'color:#909399',
    'background:rgba(255,255,255,0.92)',
    'backdrop-filter:blur(6px)',
    'border-top:1px solid rgba(0,0,0,0.06)',
    'text-align:center',
    'pointer-events:auto',
  ].join(';')
  el.innerHTML = [
    `<b style="color:#409eff">${escapeHtml(p.brand)}</b>`,
    `${escapeHtml(p.qqLabel)}<a href="https://qm.qq.com/q/${encodeURIComponent(p.qq)}" target="_blank" rel="noopener" style="color:#409eff;text-decoration:none">${escapeHtml(p.qq)}</a>`,
    `${escapeHtml(p.repoLabel)}<a href="https://${encodeURIComponent(p.repo).replace(/%2F/g, '/')}" target="_blank" rel="noopener" style="color:#409eff;text-decoration:none">${escapeHtml(p.repo)}</a>`,
    `${escapeHtml(p.picLabel)}<a href="${escapeHtml(p.picUrl)}" target="_blank" rel="noopener" style="color:#67c23a;text-decoration:none">${escapeHtml(p.picText)}</a>`,
  ].join(escapeHtml(p.sep))
  document.body.appendChild(el)
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}
