<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as keyApi from '@/api/apikey'
import { formatCredit, formatDateTime, nullVal } from '@/utils/format'

const loading = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const list = ref<keyApi.ApiKey[]>([])

const dialogVisible = ref(false)
const submitting = ref(false)
const form = reactive({
  name: '',
  quota_limit: 0,      // 0 = 无限
  rpm: 0,
  tpm: 0,
  allowed_models: '' as string,   // 逗号分隔
  allowed_ips: '' as string,
})

const showKeyDlg = ref(false)
const createdKey = ref('')

async function fetchList() {
  loading.value = true
  try {
    const data = await keyApi.listKeys(page.value, pageSize.value)
    list.value = data.list
    total.value = data.total
  } finally {
    loading.value = false
  }
}

function openCreate() {
  Object.assign(form, { name: '', quota_limit: 0, rpm: 0, tpm: 0, allowed_models: '', allowed_ips: '' })
  dialogVisible.value = true
}

async function onCreate() {
  if (!form.name) {
    ElMessage.warning('请输入 key 名称')
    return
  }
  submitting.value = true
  try {
    const res = await keyApi.createKey({
      name: form.name,
      quota_limit: Number(form.quota_limit) || 0,
      rpm: Number(form.rpm) || 0,
      tpm: Number(form.tpm) || 0,
      allowed_models: form.allowed_models
        .split(',').map((s) => s.trim()).filter(Boolean),
      allowed_ips: form.allowed_ips
        .split(',').map((s) => s.trim()).filter(Boolean),
    })
    dialogVisible.value = false
    createdKey.value = res.key
    showKeyDlg.value = true
    fetchList()
  } finally {
    submitting.value = false
  }
}

async function onToggle(row: keyApi.ApiKey) {
  await keyApi.updateKey(row.id, { enabled: !row.enabled })
  ElMessage.success(row.enabled ? '已禁用' : '已启用')
  fetchList()
}

async function onDelete(row: keyApi.ApiKey) {
  await ElMessageBox.confirm(
    `确认删除 key "${row.name}"?此操作不可撤销。`,
    '删除 Key',
    { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' },
  )
  await keyApi.deleteKey(row.id)
  ElMessage.success('已删除')
  fetchList()
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(createdKey.value)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('请手动复制')
  }
}

const remainingPct = (row: keyApi.ApiKey) => {
  if (row.quota_limit <= 0) return 100
  const left = Math.max(row.quota_limit - row.quota_used, 0)
  return Math.min(Math.round((left / row.quota_limit) * 100), 100)
}

const hasData = computed(() => list.value.length > 0)

onMounted(fetchList)
</script>

<template>
  <div class="page-container">
    <div class="card-block">
      <div class="flex-between" style="margin-bottom:16px">
        <div>
          <h2 class="page-title" style="margin:0">API Keys</h2>
          <div style="color:var(--el-text-color-secondary);font-size:13px;margin-top:4px">
            管理你的下游调用 Key,支持 RPM / TPM 限流、IP 白名单、模型白名单;Key 只在创建时完整展示一次,请妥善保存。
          </div>
        </div>
        <el-button type="primary" @click="openCreate">
          <el-icon><Plus /></el-icon> 新建 Key
        </el-button>
      </div>

      <el-table v-loading="loading" :data="list" stripe>
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column label="前缀" width="120">
          <template #default="{ row }">
            <el-tag size="small">{{ row.key_prefix }}***</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="额度(积分)" min-width="180">
          <template #default="{ row }">
            <template v-if="row.quota_limit > 0">
              <div>{{ formatCredit(row.quota_used) }} / {{ formatCredit(row.quota_limit) }}</div>
              <el-progress :percentage="remainingPct(row)"
                           :status="remainingPct(row) < 20 ? 'exception' : remainingPct(row) < 50 ? 'warning' : 'success'"
                           :show-text="false" style="margin-top:4px" />
            </template>
            <el-tag v-else type="info" size="small">无限</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="RPM / TPM" width="130">
          <template #default="{ row }">
            <div style="font-size:12px">
              <div>RPM: {{ row.rpm || '分组默认' }}</div>
              <div>TPM: {{ row.tpm || '分组默认' }}</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="允许模型" min-width="140">
          <template #default="{ row }">
            <el-tag v-if="!nullVal(row.allowed_models)" type="info" size="small">全部</el-tag>
            <span v-else>{{ nullVal(row.allowed_models) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最近使用" min-width="160">
          <template #default="{ row }">
            <div style="font-size:12px">
              <div>{{ formatDateTime(row.last_used_at) }}</div>
              <div v-if="row.last_used_ip" style="color:var(--el-text-color-secondary)">{{ row.last_used_ip }}</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="onToggle(row)">
              {{ row.enabled ? '禁用' : '启用' }}
            </el-button>
            <el-button link type="danger" @click="onDelete(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="还没有 Key,点右上角新建一把吧" />
        </template>
      </el-table>

      <el-pagination v-if="hasData"
        style="margin-top:16px;justify-content:flex-end;display:flex"
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :page-sizes="[10, 20, 50]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        @current-change="fetchList"
        @size-change="fetchList"
      />
    </div>

    <el-dialog v-model="dialogVisible" title="新建 API Key" width="520px">
      <el-form :model="form" label-width="110px" label-position="right">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="例如 prod-server-01" maxlength="64" show-word-limit />
        </el-form-item>
        <el-form-item label="额度(积分)">
          <el-input-number v-model="form.quota_limit" :min="0" :step="100" />
          <span style="margin-left:12px;color:var(--el-text-color-secondary);font-size:12px">0 表示无限</span>
        </el-form-item>
        <el-form-item label="RPM">
          <el-input-number v-model="form.rpm" :min="0" :step="10" />
          <span style="margin-left:12px;color:var(--el-text-color-secondary);font-size:12px">0 使用分组默认</span>
        </el-form-item>
        <el-form-item label="TPM">
          <el-input-number v-model="form.tpm" :min="0" :step="1000" />
        </el-form-item>
        <el-form-item label="允许模型">
          <el-input v-model="form.allowed_models" placeholder="逗号分隔,留空表示全部" />
        </el-form-item>
        <el-form-item label="IP 白名单">
          <el-input v-model="form.allowed_ips" placeholder="逗号分隔,留空表示不限" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="onCreate">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showKeyDlg" title="请保存你的 Key" width="560px" :close-on-click-modal="false"
               :close-on-press-escape="false">
      <el-alert type="warning" :closable="false" show-icon
                title="明文 Key 仅此一次展示,关闭后无法再次查看。" style="margin-bottom:12px" />
      <el-input :model-value="createdKey" readonly class="key-display">
        <template #append>
          <el-button @click="copyKey"><el-icon><CopyDocument /></el-icon> 复制</el-button>
        </template>
      </el-input>
      <template #footer>
        <el-button type="primary" @click="showKeyDlg = false">我已保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.key-display :deep(input) {
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 13px;
}
</style>
