import { http } from './http'

export interface Model {
  id: number
  slug: string
  type: string                          // chat | image
  upstream_model_slug: string
  input_price_per_1m: number
  output_price_per_1m: number
  cache_read_price_per_1m: number
  image_price_per_call: number
  description: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export function listModels(): Promise<{ items: Model[]; total: number }> {
  return http.get('/api/admin/models')
}

export interface ModelUpsert {
  slug?: string
  type: 'chat' | 'image'
  upstream_model_slug: string
  input_price_per_1m: number
  output_price_per_1m: number
  cache_read_price_per_1m: number
  image_price_per_call: number
  description: string
  enabled?: boolean
}

export function createModel(body: ModelUpsert): Promise<Model> {
  return http.post('/api/admin/models', body)
}
export function updateModel(id: number, body: ModelUpsert): Promise<Model> {
  return http.put(`/api/admin/models/${id}`, body)
}
export function setModelEnabled(id: number, enabled: boolean) {
  return http.patch(`/api/admin/models/${id}/enabled`, { enabled })
}
export function deleteModel(id: number) {
  return http.delete(`/api/admin/models/${id}`)
}

// ---------- usage stats ----------

export interface Overall {
  requests: number
  failures: number
  chat_requests: number
  image_images: number
  input_tokens: number
  output_tokens: number
  credit_cost: number
}

export interface DailyPoint {
  day: string
  requests: number
  failures: number
  input_tokens: number
  output_tokens: number
  image_count: number
  credit_cost: number
}

export interface ModelStat {
  model_id: number
  model_slug: string
  type: string
  requests: number
  failures: number
  input_tokens: number
  output_tokens: number
  image_count: number
  credit_cost: number
  avg_dur_ms: number
}

export interface UserStat {
  user_id: number
  email: string
  requests: number
  failures: number
  credit_cost: number
}

export interface StatsResp {
  overall: Overall
  daily: DailyPoint[]
  by_model: ModelStat[]
  by_user: UserStat[]
}

export function getUsageStats(params: {
  days?: number; top_n?: number;
  user_id?: number; model_id?: number;
  type?: string; status?: string;
  since?: string; until?: string;
} = {}): Promise<StatsResp> {
  return http.get('/api/admin/usage/stats', { params })
}

// ---------- admin keys ----------

export interface AdminKeyRow {
  id: number
  user_id: number
  user_email: string
  name: string
  key_prefix: string
  quota_limit: number
  quota_used: number
  rpm: number
  tpm: number
  enabled: boolean
  last_used_at?: any
  last_used_ip?: string
  expires_at?: any
  created_at: string
}

export function listAdminKeys(params: {
  q?: string; user_id?: number; enabled?: '1' | '0' | '';
  limit?: number; offset?: number;
} = {}): Promise<{ items: AdminKeyRow[]; total: number; limit: number; offset: number }> {
  return http.get('/api/admin/keys', { params })
}

export function setAdminKeyEnabled(id: number, enabled: boolean) {
  return http.patch(`/api/admin/keys/${id}`, { enabled })
}
