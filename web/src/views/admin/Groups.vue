<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as adminApi from '@/api/admin'

const loading = ref(false)
const rows = ref<adminApi.Group[]>([])

async function load() {
  loading.value = true
  try {
    const d = await adminApi.listGroups()
    rows.value = d.items
  } finally { loading.value = false }
}

const dlg = ref(false)
const isEdit = ref(false)
const form = reactive<adminApi.Group>({
  id: 0, name: '', ratio: 1, daily_limit_credits: 0,
  rpm_limit: 60, tpm_limit: 60_000, remark: '',
})
function openCreate() {
  isEdit.value = false
  Object.assign(form, { id: 0, name: '', ratio: 1, daily_limit_credits: 0, rpm_limit: 60, tpm_limit: 60_000, remark: '' })
  dlg.value = true
}
function openEdit(row: adminApi.Group) {
  isEdit.value = true
  Object.assign(form, row)
  dlg.value = true
}
async function submit() {
  if (!form.name || form.ratio <= 0) return ElMessage.warning('名称/倍率不合法')
  const payload = {
    name: form.name, ratio: form.ratio,
    daily_limit_credits: form.daily_limit_credits,
    rpm_limit: form.rpm_limit, tpm_limit: form.tpm_limit,
    remark: form.remark,
  }
  if (isEdit.value) await adminApi.updateGroup(form.id, payload)
  else await adminApi.createGroup(payload)
  ElMessage.success('保存成功')
  dlg.value = false
  load()
}
async function onDelete(row: adminApi.Group) {
  await ElMessageBox.confirm(`确认删除分组 "${row.name}"?仅当该分组下无用户时才可删除。`, '删除分组', {
    type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消',
  })
  await adminApi.deleteGroup(row.id)
  ElMessage.success('已删除')
  load()
}

onMounted(load)
</script>

<template>
  <div class="page-container">
    <div class="card-block">
      <div class="flex-between" style="margin-bottom:8px">
        <div>
          <h2 class="page-title" style="margin:0">用户分组</h2>
          <div style="color:var(--el-text-color-secondary);font-size:13px;margin-top:4px">
            给用户分组并设置计费倍率(×1.0 / ×1.5 / ×0.8 …),用于 VIP / 内部 / 渠道等差异化价格场景。
          </div>
        </div>
        <el-button type="primary" @click="openCreate"><el-icon><Plus /></el-icon> 新建分组</el-button>
      </div>
      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="name" label="名称" width="140" />
        <el-table-column label="倍率" width="110">
          <template #default="{ row }">
            <el-tag size="small">×{{ row.ratio }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="日限额(厘)" min-width="140">
          <template #default="{ row }">
            {{ row.daily_limit_credits === 0 ? '不限' : row.daily_limit_credits }}
          </template>
        </el-table-column>
        <el-table-column prop="rpm_limit" label="RPM" width="100" />
        <el-table-column prop="tpm_limit" label="TPM" width="130" />
        <el-table-column prop="remark" label="备注" min-width="200" show-overflow-tooltip />
        <el-table-column label="操作" width="170" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" :disabled="row.id === 1" @click="onDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="dlg" :title="isEdit ? '编辑分组' : '新建分组'" width="500px">
      <el-form :model="form" label-width="110px">
        <el-form-item label="名称" required><el-input v-model="form.name" maxlength="64" /></el-form-item>
        <el-form-item label="倍率">
          <el-input-number v-model="form.ratio" :step="0.1" :min="0.1" :precision="2" />
          <span style="margin-left:8px;color:var(--el-text-color-secondary);font-size:12px">
            VIP 通常 0.8,SVIP 0.6
          </span>
        </el-form-item>
        <el-form-item label="日限额">
          <el-input-number v-model="form.daily_limit_credits" :min="0" :step="10000" style="width:100%" />
          <div style="font-size:12px;color:var(--el-text-color-secondary)">0 表示不限,单位厘</div>
        </el-form-item>
        <el-form-item label="RPM">
          <el-input-number v-model="form.rpm_limit" :min="1" :step="10" style="width:100%" />
        </el-form-item>
        <el-form-item label="TPM">
          <el-input-number v-model="form.tpm_limit" :min="100" :step="1000" style="width:100%" />
        </el-form-item>
        <el-form-item label="备注"><el-input v-model="form.remark" type="textarea" :rows="2" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg = false">取消</el-button>
        <el-button type="primary" @click="submit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>
