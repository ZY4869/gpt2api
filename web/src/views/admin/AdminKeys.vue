<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as statsApi from '@/api/stats'
import { nullVal } from '@/utils/format'

const loading = ref(false)
const rows = ref<statsApi.AdminKeyRow[]>([])
const total = ref(0)

const filter = reactive({
  q: '',
  user_id: undefined as number | undefined,
  enabled: '' as '' | '1' | '0',
  limit: 20,
  offset: 0,
})

async function load() {
  loading.value = true
  try {
    const d = await statsApi.listAdminKeys({
      q: filter.q || undefined,
      user_id: filter.user_id || undefined,
      enabled: filter.enabled || undefined,
      limit: filter.limit,
      offset: filter.offset,
    })
    rows.value = d.items
    total.value = d.total
  } finally { loading.value = false }
}

function reset() {
  filter.q = ''
  filter.user_id = undefined
  filter.enabled = ''
  filter.offset = 0
  load()
}

async function toggle(row: statsApi.AdminKeyRow) {
  const next = !row.enabled
  await ElMessageBox.confirm(
    `确认${next ? '启用' : '禁用'} #${row.id}${row.name}(用户 ${row.user_email})?`,
    '操作确认',
    { type: 'warning' },
  )
  await statsApi.setAdminKeyEnabled(row.id, next)
  ElMessage.success('已更新')
  load()
}

function usagePercent(r: statsApi.AdminKeyRow) {
  if (!r.quota_limit) return 0
  return Math.min(100, Math.round((r.quota_used / r.quota_limit) * 100))
}

const currentPage = computed<number>({
  get() { return Math.floor(filter.offset / filter.limit) + 1 },
  set(v) { filter.offset = (v - 1) * filter.limit },
})

onMounted(load)
</script>

<template>
  <div class="page-container">
    <div class="card-block">
      <h2 class="page-title" style="margin:0">全局 API Key</h2>
      <div style="color:var(--el-text-color-secondary);font-size:13px;margin:4px 0 10px">
        跨用户查看和管控所有下游 Key,按名称 / 前缀 / 用户筛选,可直接禁用异常 Key。
      </div>
      <div class="flex-wrap-gap" style="margin:10px 0">
        <el-input v-model="filter.q" placeholder="按名称 / prefix / 邮箱" style="width:240px" clearable
                  @keyup.enter="load" />
        <el-input-number v-model="filter.user_id" :min="1" placeholder="用户 ID" style="width:140px" />
        <el-select v-model="filter.enabled" placeholder="状态" style="width:120px" clearable>
          <el-option label="全部" value="" />
          <el-option label="启用" value="1" />
          <el-option label="禁用" value="0" />
        </el-select>
        <el-button type="primary" @click="load" :loading="loading">
          <el-icon><Search /></el-icon> 查询
        </el-button>
        <el-button @click="reset">重置</el-button>
      </div>

      <el-table :data="rows" stripe v-loading="loading">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column label="归属用户" min-width="200">
          <template #default="{ row }">
            <div><b>#{{ row.user_id }}</b></div>
            <div style="font-size:12px;color:var(--el-text-color-secondary)">
              {{ row.user_email || '—' }}
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="名称" min-width="140" show-overflow-tooltip />
        <el-table-column label="Prefix" width="130">
          <template #default="{ row }"><code>{{ row.key_prefix }}</code></template>
        </el-table-column>
        <el-table-column label="额度" width="220">
          <template #default="{ row }">
            <div v-if="row.quota_limit > 0">
              <el-progress :percentage="usagePercent(row)"
                           :status="usagePercent(row) >= 90 ? 'exception' : undefined"
                           :stroke-width="8" />
              <div style="font-size:12px;color:var(--el-text-color-secondary);margin-top:2px">
                {{ row.quota_used }} / {{ row.quota_limit }}
              </div>
            </div>
            <span v-else style="color:var(--el-text-color-secondary)">不限</span>
          </template>
        </el-table-column>
        <el-table-column label="限速" width="120">
          <template #default="{ row }">
            <div style="font-size:12px">rpm: {{ row.rpm || '∞' }}</div>
            <div style="font-size:12px">tpm: {{ row.tpm || '∞' }}</div>
          </template>
        </el-table-column>
        <el-table-column label="最近使用" width="170">
          <template #default="{ row }">
            <div style="font-size:12px">{{ nullVal(row.last_used_at) || '—' }}</div>
            <div style="font-size:11px;color:var(--el-text-color-secondary)">
              {{ row.last_used_ip || '' }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="110" fixed="right">
          <template #default="{ row }">
            <el-button size="small" link :type="row.enabled ? 'danger' : 'primary'"
                       @click="toggle(row)">
              {{ row.enabled ? '禁用' : '启用' }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        style="margin-top:12px"
        background
        layout="total, prev, pager, next, sizes"
        :total="total"
        v-model:current-page="currentPage"
        :page-sizes="[20, 50, 100]"
        v-model:page-size="filter.limit"
        @size-change="() => { filter.offset = 0; load() }"
        @current-change="(p: number) => { filter.offset = (p - 1) * filter.limit; load() }"
      />
    </div>
  </div>
</template>

<style scoped>
code {
  background: #f2f3f5;
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 12px;
  font-family: ui-monospace, Menlo, Consolas, monospace;
}
:global(html.dark) code { background: #1d2026; }
</style>
