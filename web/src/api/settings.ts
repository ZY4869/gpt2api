import { http } from './http'

// 系统设置 KV 条目(管理端用,带 schema)。
export interface SettingItem {
  key: string
  value: string
  type: 'string' | 'bool' | 'int' | 'email' | 'url' | string
  category: 'site' | 'auth' | 'limit' | 'mail' | string
  label: string
  desc: string
}

export function listSettings(): Promise<{ items: SettingItem[] }> {
  return http.get('/api/admin/settings')
}

export function updateSettings(items: Record<string, string>): Promise<{ updated: number }> {
  return http.put('/api/admin/settings', { items })
}

export function reloadSettings(): Promise<{ reloaded: boolean }> {
  return http.post('/api/admin/settings/reload')
}

export function sendTestEmail(to: string): Promise<{ sent: boolean; to: string }> {
  return http.post('/api/admin/settings/test-email', { to })
}

// 匿名公开接口:返回登录页需要的站点元信息(site.name 等)。
export function fetchSiteInfo(): Promise<Record<string, string>> {
  return http.get('/api/public/site-info')
}
