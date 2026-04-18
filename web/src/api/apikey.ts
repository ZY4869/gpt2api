import { http } from './http'

export interface ApiKey {
  id: number
  user_id: number
  name: string
  key_prefix: string
  quota_limit: number
  quota_used: number
  rpm: number
  tpm: number
  enabled: boolean
  last_used_at?: { Time: string; Valid: boolean } | string | null
  last_used_ip?: string
  expires_at?: { Time: string; Valid: boolean } | string | null
  created_at: string
  updated_at: string
  allowed_models?: { String: string; Valid: boolean } | null
  allowed_ips?: { String: string; Valid: boolean } | null
}

export interface CreatedKey {
  key: string           // 明文 key,仅此一次
  record: ApiKey
}

export interface ListPage {
  list: ApiKey[]
  total: number
  page: number
  page_size: number
}

export function listKeys(page = 1, size = 20): Promise<ListPage> {
  return http.get('/api/keys', { params: { page, page_size: size } })
}

export function createKey(req: Partial<Pick<ApiKey, 'name' | 'quota_limit' | 'rpm' | 'tpm'>> & {
  expires_at?: string
  allowed_models?: string[]
  allowed_ips?: string[]
}): Promise<CreatedKey> {
  // 后端 ExpiresAt 是 time.Time,Go 对 zero time 的 JSON 是 "0001-01-01T00:00:00Z"
  // 前端空值时传 0001-01-01T00:00:00Z 更稳;此处直接不传让后端零值
  return http.post('/api/keys', req)
}

export function updateKey(id: number, req: Record<string, unknown>): Promise<ApiKey> {
  return http.patch(`/api/keys/${id}`, req)
}

export function deleteKey(id: number): Promise<{ deleted: number }> {
  return http.delete(`/api/keys/${id}`)
}
