import { http } from './http'

export interface BackupFile {
  id: number
  backup_id: string
  file_name: string
  size_bytes: number
  sha256: string
  trigger: string        // manual | cron | upload
  status: string         // running | ready | failed
  error?: string
  include_data: boolean
  created_by: number
  created_at: string
  finished_at?: string | { Time: string; Valid: boolean }
}

export interface BackupListResp {
  items: BackupFile[]
  total: number
  allow_restore: boolean
  max_upload_mb: number
}

export function listBackups(limit = 50, offset = 0): Promise<BackupListResp> {
  return http.get('/api/admin/system/backup', { params: { limit, offset } })
}

export function createBackup(includeData = true): Promise<BackupFile> {
  return http.post('/api/admin/system/backup', { include_data: includeData })
}

export function deleteBackup(id: string, adminPassword: string): Promise<unknown> {
  return http.delete(`/api/admin/system/backup/${id}`, {
    headers: { 'X-Admin-Confirm': adminPassword },
  })
}

export function restoreBackup(id: string, adminPassword: string): Promise<unknown> {
  return http.post(`/api/admin/system/backup/${id}/restore`, {}, {
    headers: { 'X-Admin-Confirm': adminPassword },
  })
}

export function downloadBackup(id: string, fileName: string) {
  return http.get(`/api/admin/system/backup/${id}/download`, { responseType: 'blob' })
    .then((res: any) => {
      const blob = res.data instanceof Blob ? res.data : new Blob([res])
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = fileName
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
    })
}

export function uploadBackup(file: File, adminPassword: string, onProgress?: (pct: number) => void) {
  const fd = new FormData()
  fd.append('file', file)
  fd.append('admin_password', adminPassword)
  return http.post('/api/admin/system/backup/upload', fd, {
    headers: {
      'Content-Type': 'multipart/form-data',
      'X-Admin-Confirm': adminPassword,
    },
    onUploadProgress: (e) => {
      if (e.total && onProgress) onProgress(Math.round((e.loaded / e.total) * 100))
    },
  })
}
