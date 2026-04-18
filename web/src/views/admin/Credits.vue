<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as adminApi from '@/api/admin'

// -------- 统计摘要 --------
const summary = ref<adminApi.CreditsSummary | null>(null)

async function fetchSummary() {
  try {
    summary.value = await adminApi.creditsSummary()
  } catch (e: any) {
    ElMessage.error(e?.message || '加载摘要失败')
  }
}

/** credit 单位是"厘",10000 厘 = 1 积分。展示时转成积分,保留 2 位小数。 */
function fmtCredits(milli: number | undefined | null) {
  if (!milli) return '0'
  const v = milli / 10000
  return v.toLocaleString('zh-CN', { maximumFractionDigits: 2 })
}

// -------- 流水表格 --------
const loading = ref(false)
const rows = ref<adminApi.CreditLogGlobal[]>([])
const total = ref(0)
const pager = reactive({ limit: 20, offset: 0 })
const page = computed({
  get: () => Math.floor(pager.offset / pager.limit) + 1,
  set: (v: number) => { pager.offset = (v - 1) * pager.limit },
})

const filter = reactive<adminApi.CreditLogFilter>({
  user_id: undefined,
  keyword: '',
  type: '',
  sign: '',
  start_at: '',
  end_at: '',
})

const timeRange = ref<[string, string] | null>(null)

async function fetchLogs() {
  loading.value = true
  try {
    const q: adminApi.CreditLogFilter = { ...filter, ...pager }
    if (timeRange.value && timeRange.value.length === 2) {
      q.start_at = timeRange.value[0]
      q.end_at = timeRange.value[1]
    } else {
      delete q.start_at
      delete q.end_at
    }
    if (!q.keyword) delete q.keyword
    if (!q.user_id) delete q.user_id
    if (!q.type) delete q.type
    if (!q.sign) delete q.sign

    const data = await adminApi.listCreditLogsGlobal(q)
    rows.value = data.items || []
    total.value = data.total || 0
  } catch (e: any) {
    ElMessage.error(e?.message || '加载流水失败')
  } finally { loading.value = false }
}

function onSearch() {
  pager.offset = 0
  fetchLogs()
}
function onReset() {
  filter.user_id = undefined
  filter.keyword = ''
  filter.type = ''
  filter.sign = ''
  timeRange.value = null
  pager.offset = 0
  fetchLogs()
}

// -------- 类型徽标 --------
const TYPE_MAP: Record<string, { label: string; type: 'success' | 'warning' | 'info' | 'danger' | 'primary' }> = {
  recharge:    { label: '充值',     type: 'success' },
  redeem:      { label: '兑换码',   type: 'success' },
  admin_adjust:{ label: '管理员调账', type: 'warning' },
  refund:      { label: '退款',     type: 'info'    },
  consume:     { label: '消费',     type: 'danger'  },
  freeze:      { label: '冻结',     type: 'info'    },
  unfreeze:    { label: '解冻',     type: 'primary' },
}
function typeTag(t: string) { return TYPE_MAP[t] || { label: t, type: 'info' as const } }

// -------- 调账对话框 --------
const adjDlg = ref(false)
const adjLoading = ref(false)
const adjForm = reactive({
  user_id: null as number | null,
  delta_credits: null as number | null, // 注意:前端填"积分",提交时 ×10000
  remark: '',
  ref_id: '',
  admin_password: '',
})

function openAdjust(row?: adminApi.CreditLogGlobal) {
  adjForm.user_id = row?.user_id ?? null
  adjForm.delta_credits = null
  adjForm.remark = ''
  adjForm.ref_id = ''
  adjForm.admin_password = ''
  adjDlg.value = true
}

async function submitAdjust() {
  if (!adjForm.user_id || adjForm.user_id <= 0) return ElMessage.warning('请填写有效的用户 ID')
  if (!adjForm.delta_credits || adjForm.delta_credits === 0) return ElMessage.warning('调账积分不能为 0')
  if (!adjForm.remark.trim()) return ElMessage.warning('请填写备注(便于稽核)')
  if (!adjForm.admin_password) return ElMessage.warning('需要二次输入您当前账号的登录密码')

  const delta = Math.round(adjForm.delta_credits * 10000) // 积分 → 厘
  await ElMessageBox.confirm(
    `确认对用户 #${adjForm.user_id} ${delta > 0 ? '增加' : '扣减'} ${Math.abs(adjForm.delta_credits)} 积分?此操作会写入审计日志。`,
    '调账确认',
    { type: 'warning', confirmButtonText: '确认调账', cancelButtonText: '取消' },
  )

  adjLoading.value = true
  try {
    const r = await adminApi.adjustCreditByUser({
      user_id: adjForm.user_id,
      delta,
      remark: adjForm.remark,
      ref_id: adjForm.ref_id || undefined,
    }, adjForm.admin_password)
    ElMessage.success(`调账成功 · 用户当前余额 ${fmtCredits(r.balance_after)} 积分`)
    adjDlg.value = false
    fetchSummary()
    fetchLogs()
  } catch (e: any) {
    ElMessage.error(e?.message || '调账失败')
  } finally { adjLoading.value = false }
}

onMounted(() => {
  fetchSummary()
  fetchLogs()
})
</script>

<template>
  <div class="page-container">
    <!-- 摘要卡片 -->
    <div class="card-block summary-card">
      <div class="summary-row">
        <div class="sum-card income">
          <div class="sum-title">今日新增</div>
          <div class="sum-value">+ {{ fmtCredits(summary?.in_today) }}</div>
          <div class="sum-sub">今日消费 - {{ fmtCredits(summary?.out_today) }}</div>
        </div>
        <div class="sum-card week">
          <div class="sum-title">近 7 天新增</div>
          <div class="sum-value">+ {{ fmtCredits(summary?.in_7days) }}</div>
          <div class="sum-sub">近 7 天消费 - {{ fmtCredits(summary?.out_7days) }}</div>
        </div>
        <div class="sum-card total">
          <div class="sum-title">累计入账</div>
          <div class="sum-value">{{ fmtCredits(summary?.in_total) }}</div>
          <div class="sum-sub">累计消费 {{ fmtCredits(summary?.out_total) }}</div>
        </div>
        <div class="sum-card balance">
          <div class="sum-title">全站当前余额</div>
          <div class="sum-value">{{ fmtCredits(summary?.total_balance) }}</div>
          <div class="sum-sub">所有用户未消费积分总和</div>
        </div>
      </div>
    </div>

    <!-- 筛选 + 流水表格 -->
    <div class="card-block">
      <div class="flex-between" style="margin-bottom:12px">
        <div>
          <h2 class="page-title" style="margin:0">积分流水</h2>
          <div style="color:var(--el-text-color-secondary);font-size:13px;margin-top:4px">
            展示全站所有积分账变(预扣 / 结算 / 充值 / 调账 / 退款),支持按用户 / 类型 / 方向 / 时间过滤,手动调账会自动写入审计日志。
          </div>
        </div>
        <div>
          <el-button type="primary" @click="openAdjust()">
            <el-icon><Coin /></el-icon> 手动调账
          </el-button>
        </div>
      </div>

      <el-form :inline="true" :model="filter" class="filter-form">
        <el-form-item label="用户 ID">
          <el-input-number v-model="filter.user_id" :min="1" controls-position="right"
            placeholder="用户 ID" style="width:160px" />
        </el-form-item>
        <el-form-item label="邮箱/昵称">
          <el-input v-model="filter.keyword" placeholder="模糊匹配" clearable style="width:200px" />
        </el-form-item>
        <el-form-item label="类型">
          <el-select v-model="filter.type" clearable placeholder="全部" style="width:150px">
            <el-option v-for="(v, k) in TYPE_MAP" :key="k" :label="v.label" :value="k" />
          </el-select>
        </el-form-item>
        <el-form-item label="方向">
          <el-select v-model="filter.sign" clearable placeholder="全部" style="width:120px">
            <el-option label="入账" value="in" />
            <el-option label="出账" value="out" />
          </el-select>
        </el-form-item>
        <el-form-item label="时间范围">
          <el-date-picker v-model="timeRange" type="datetimerange"
            value-format="YYYY-MM-DD HH:mm:ss"
            start-placeholder="开始" end-placeholder="结束" clearable />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="onSearch">
            <el-icon><Search /></el-icon> 查询
          </el-button>
          <el-button @click="onReset">重置</el-button>
        </el-form-item>
      </el-form>

      <el-table v-loading="loading" :data="rows" stripe class="log-table">
        <el-table-column prop="created_at" label="时间" width="170" />
        <el-table-column label="用户" min-width="200">
          <template #default="{ row }">
            <div class="cell-user">
              <span class="uid">#{{ row.user_id }}</span>
              <span class="uemail">{{ row.user_email || '-' }}</span>
            </div>
            <div class="unick">{{ row.user_nickname }}</div>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="110">
          <template #default="{ row }">
            <el-tag :type="typeTag(row.type).type" size="small">{{ typeTag(row.type).label }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="变动" width="150" align="right">
          <template #default="{ row }">
            <span :class="row.amount > 0 ? 'amount-in' : 'amount-out'">
              {{ row.amount > 0 ? '+' : '' }}{{ fmtCredits(row.amount) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="余额" width="150" align="right">
          <template #default="{ row }">{{ fmtCredits(row.balance_after) }}</template>
        </el-table-column>
        <el-table-column label="关联单号" width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <code v-if="row.ref_id" class="ref">{{ row.ref_id }}</code>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="200" show-overflow-tooltip />
        <el-table-column label="操作" width="110" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openAdjust(row)">调账</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination style="margin-top:16px;display:flex;justify-content:flex-end"
        v-model:current-page="page"
        v-model:page-size="pager.limit"
        :total="total"
        :page-sizes="[20, 50, 100, 200]"
        layout="total, sizes, prev, pager, next"
        @current-change="fetchLogs"
        @size-change="() => { pager.offset = 0; fetchLogs() }"
      />
    </div>

    <!-- 调账对话框 -->
    <el-dialog v-model="adjDlg" title="手动调账" width="540px" @closed="() => { adjForm.admin_password = '' }">
      <el-form :model="adjForm" label-width="100px">
        <el-form-item label="目标用户 ID" required>
          <el-input-number v-model="adjForm.user_id" :min="1" controls-position="right" style="width:100%" />
        </el-form-item>
        <el-form-item label="调账积分" required>
          <el-input-number v-model="adjForm.delta_credits" :precision="2"
            :step="100" controls-position="right" style="width:100%"
            placeholder="正数=增加,负数=扣减" />
          <div class="form-hint">单位:积分(1 积分 = 10000 厘,后端精度单位)。</div>
        </el-form-item>
        <el-form-item label="备注" required>
          <el-input v-model="adjForm.remark" maxlength="200" show-word-limit
            placeholder="请简要说明调账原因(写入审计日志)" />
        </el-form-item>
        <el-form-item label="关联单号">
          <el-input v-model="adjForm.ref_id" placeholder="可选,如工单号、退款单号" />
        </el-form-item>
        <el-form-item label="管理员密码" required>
          <el-input v-model="adjForm.admin_password" type="password" show-password
            placeholder="请再次输入您当前账号的登录密码以二次确认" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="adjDlg = false">取消</el-button>
        <el-button type="primary" :loading="adjLoading" @click="submitAdjust">确认调账</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.page-title { font-size: 18px; font-weight: 600; }

/* ---- 摘要卡 ---- */
.summary-card { padding-top: 16px; padding-bottom: 16px; }
.summary-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}
.sum-card {
  background: var(--el-fill-color-light);
  border-radius: 8px;
  padding: 16px 18px;
  position: relative;
  overflow: hidden;
  transition: transform 0.15s, box-shadow 0.15s;
}
.sum-card:hover {
  transform: translateY(-1px);
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.05);
}
.sum-card::before {
  content: '';
  position: absolute;
  top: 0; left: 0; right: 0;
  height: 3px;
}
.sum-card.income::before   { background: var(--el-color-success); }
.sum-card.week::before     { background: var(--el-color-primary); }
.sum-card.total::before    { background: var(--el-color-warning); }
.sum-card.balance::before  { background: var(--el-color-danger); }

.sum-title { font-size: 13px; color: var(--el-text-color-secondary); }
.sum-value { font-size: 24px; font-weight: 600; margin-top: 6px; color: var(--el-text-color-primary); line-height: 1.2; }
.sum-sub   { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; }

@media (max-width: 1100px) {
  .summary-row { grid-template-columns: repeat(2, 1fr); }
}

/* ---- 筛选行 ---- */
.filter-form { margin-bottom: 4px; }
.filter-form :deep(.el-form-item) { margin-bottom: 10px; }

/* ---- 表格 ---- */
.log-table { margin-top: 4px; }
.cell-user { display: flex; gap: 8px; align-items: baseline; }
.uid { font-weight: 600; color: var(--el-text-color-primary); }
.uemail { color: var(--el-text-color-regular); font-size: 13px; }
.unick { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 2px; }
.amount-in  { color: var(--el-color-success); font-weight: 600; }
.amount-out { color: var(--el-color-danger);  font-weight: 600; }
.muted { color: var(--el-text-color-secondary); }
.ref {
  background: var(--el-fill-color-light);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 12px;
}

.form-hint { font-size: 12px; color: var(--el-text-color-secondary); line-height: 1.5; margin-top: 4px; }
</style>
