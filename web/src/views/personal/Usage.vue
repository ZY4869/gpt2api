<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import * as meApi from '@/api/me'
import { formatCredit, formatDateTime, formatErrorCode } from '@/utils/format'
import { ENABLE_CHAT_MODEL } from '@/config/feature'

// ==================== 概览 + 每日 + 模型 TOP ====================

const statsLoading = ref(false)
const stats = ref<meApi.MyStatsResp | null>(null)

const statsFilter = reactive({
  days: 14,
  type: '' as '' | 'chat' | 'image',
})

async function loadStats() {
  statsLoading.value = true
  try {
    stats.value = await meApi.getMyUsageStats({
      days: statsFilter.days,
      top_n: 8,
      type: statsFilter.type || undefined,
    })
  } finally {
    statsLoading.value = false
  }
}

const overall = computed(() => stats.value?.overall)
const daily = computed(() => stats.value?.daily || [])
const byModel = computed(() => stats.value?.by_model || [])

// ============ 每日请求图表(SVG)============
// 自适应宽度:随容器大小重绘
const chartWrap = ref<HTMLElement | null>(null)
const chartW = ref(720)
const chartH = 220               // 总高度
const padT = 16                  // 上 padding
const padB = 36                  // 下 padding(放日期标签)
const padL = 40                  // 左 padding(放 y 轴刻度)
const padR = 16                  // 右 padding
const hoverIdx = ref(-1)

// 监听容器尺寸
let ro: ResizeObserver | null = null
onMounted(() => {
  if (chartWrap.value && typeof ResizeObserver !== 'undefined') {
    ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        const w = e.contentRect.width
        if (w > 0) chartW.value = Math.floor(w)
      }
    })
    ro.observe(chartWrap.value)
  }
})
onBeforeUnmount(() => { ro?.disconnect() })

// y 轴"好看"的最大值 —— 向上取整到 1/2/5/10 系列
function niceMax(v: number): number {
  if (v <= 1) return 1
  const exp = Math.pow(10, Math.floor(Math.log10(v)))
  const n = v / exp
  let m = 10
  if (n <= 1) m = 1
  else if (n <= 2) m = 2
  else if (n <= 5) m = 5
  return m * exp
}

const maxRaw = computed(() => daily.value.reduce((x, r) => Math.max(x, r.requests), 0))
const yMax = computed(() => niceMax(maxRaw.value || 1))
const yTicks = computed(() => {
  const m = yMax.value
  const step = m / 4
  return [0, step, step * 2, step * 3, m].map((v) => Math.round(v))
})

// x 轴每列中心点
const cellW = computed(() => {
  const n = Math.max(daily.value.length, 1)
  return (chartW.value - padL - padR) / n
})
function xCenter(i: number) { return padL + cellW.value * (i + 0.5) }
function barH(v: number) {
  const innerH = chartH - padT - padB
  return (v / yMax.value) * innerH
}
function barY(v: number) { return chartH - padB - barH(v) }

// 柱条宽度:最多 22px,避免过宽;数据点少时也不至于巨粗
const barW = computed(() => Math.min(22, Math.max(6, cellW.value * 0.55)))

// x 轴日期标签显隔:超过 18 个点时每隔 2 个显示
const labelStep = computed(() => (daily.value.length > 18 ? 2 : 1))
function shouldShowLabel(i: number) {
  const n = daily.value.length
  if (i === n - 1) return true
  return i % labelStep.value === 0
}

// 悬停 tooltip 坐标(跟随柱条,不跟鼠标,稳定)
const tipX = computed(() => (hoverIdx.value >= 0 ? xCenter(hoverIdx.value) : 0))
const tipY = computed(() => (hoverIdx.value >= 0 ? barY(daily.value[hoverIdx.value]?.requests || 0) : 0))
const tipSide = computed<'left' | 'right'>(() => (tipX.value > chartW.value / 2 ? 'left' : 'right'))

function onCellEnter(i: number) { hoverIdx.value = i }
function onChartLeave() { hoverIdx.value = -1 }

function successRate(s?: meApi.UsageOverall) {
  if (!s || s.requests === 0) return '—'
  return `${(((s.requests - s.failures) / s.requests) * 100).toFixed(2)}%`
}

// ==================== 明细 Tab(请求日志 / 积分流水) ====================

const activeTab = ref<'logs' | 'credits'>('logs')

// ---------- 请求日志 ----------
const logLoading = ref(false)
const logItems = ref<meApi.UsageItem[]>([])
const logTotal = ref(0)
const logFilter = reactive({
  type: '' as '' | 'chat' | 'image',
  status: '' as '' | 'success' | 'failed',
  limit: 20,
  offset: 0,
})

async function loadLogs() {
  logLoading.value = true
  try {
    const d = await meApi.listMyUsageLogs({
      type: logFilter.type || undefined,
      status: logFilter.status || undefined,
      limit: logFilter.limit,
      offset: logFilter.offset,
    })
    logItems.value = d.items
    logTotal.value = d.total
  } finally {
    logLoading.value = false
  }
}

const logPage = computed<number>({
  get: () => Math.floor(logFilter.offset / logFilter.limit) + 1,
  set: (v) => {
    logFilter.offset = (v - 1) * logFilter.limit
    loadLogs()
  },
})

function refreshLogs() {
  logFilter.offset = 0
  loadLogs()
}

const statusMap: Record<string, { tag: 'success' | 'danger' | 'warning' | 'info'; label: string }> = {
  success: { tag: 'success', label: '成功' },
  failed: { tag: 'danger', label: '失败' },
  partial: { tag: 'warning', label: '部分' },
}
function statusTag(s: string) {
  return statusMap[s]?.tag || 'info'
}
function statusLabel(s: string) {
  return statusMap[s]?.label || s || '-'
}

// ---------- 积分流水 ----------
const creditLoading = ref(false)
const creditItems = ref<meApi.MyCreditLog[]>([])
const creditTotal = ref(0)
const creditFilter = reactive({ limit: 20, offset: 0 })

async function loadCredits() {
  creditLoading.value = true
  try {
    const d = await meApi.listMyCreditLogs({
      limit: creditFilter.limit,
      offset: creditFilter.offset,
    })
    creditItems.value = d.items
    creditTotal.value = d.total
  } finally {
    creditLoading.value = false
  }
}

const creditPage = computed<number>({
  get: () => Math.floor(creditFilter.offset / creditFilter.limit) + 1,
  set: (v) => {
    creditFilter.offset = (v - 1) * creditFilter.limit
    loadCredits()
  },
})

const typeLabel: Record<string, string> = {
  recharge: '充值',
  consume: '消费',
  refund: '退款',
  adjust: '调账',
  bonus: '赠送',
  freeze: '冻结',
  unfreeze: '解冻',
}

function onTabChange(v: string | number) {
  if (v === 'credits' && creditItems.value.length === 0) loadCredits()
}

onMounted(() => {
  loadStats()
  loadLogs()
})
</script>

<template>
  <div class="page-container">
    <!-- 顶部概览 -->
    <div class="card-block">
      <div class="flex-between" style="margin-bottom:12px">
        <div>
          <h2 class="page-title" style="margin:0">使用记录</h2>
          <div class="page-sub">
            展示当前账号的请求量、生图张数、积分账变与调用明细,支持按天 / 类型 / 状态切片,仅本人可见。
          </div>
        </div>
        <div class="flex-wrap-gap">
          <el-select v-model="statsFilter.days" style="width:110px" @change="loadStats">
            <el-option :value="7"  label="近 7 天" />
            <el-option :value="14" label="近 14 天" />
            <el-option :value="30" label="近 30 天" />
            <el-option :value="60" label="近 60 天" />
          </el-select>
          <el-select v-model="statsFilter.type" style="width:120px" clearable placeholder="类型"
                     @change="loadStats">
            <el-option label="全部" value="" />
            <el-option v-if="ENABLE_CHAT_MODEL" label="对话" value="chat" />
            <el-option label="生图" value="image" />
          </el-select>
          <el-button :loading="statsLoading" type="primary" @click="loadStats">
            <el-icon><Refresh /></el-icon> 刷新
          </el-button>
        </div>
      </div>

      <el-row :gutter="16" v-loading="statsLoading">
        <el-col :md="6" :sm="12">
          <div class="sum-card requests">
            <div class="sum-title">请求数</div>
            <div class="sum-value">{{ overall?.requests ?? 0 }}</div>
            <div class="sum-sub">
              失败 {{ overall?.failures ?? 0 }} · 成功率 {{ successRate(overall) }}
            </div>
          </div>
        </el-col>
        <el-col v-if="ENABLE_CHAT_MODEL" :md="6" :sm="12">
          <div class="sum-card chat">
            <div class="sum-title">对话请求</div>
            <div class="sum-value">{{ overall?.chat_requests ?? 0 }}</div>
            <div class="sum-sub">生图 {{ overall?.image_images ?? 0 }} 张</div>
          </div>
        </el-col>
        <el-col v-else :md="6" :sm="12">
          <div class="sum-card chat">
            <div class="sum-title">生图张数</div>
            <div class="sum-value">{{ overall?.image_images ?? 0 }}</div>
            <div class="sum-sub">成功率 {{ successRate(overall) }}</div>
          </div>
        </el-col>
        <el-col :md="6" :sm="12">
          <div class="sum-card token">
            <div class="sum-title">Token 合计</div>
            <div class="sum-value">
              {{ (overall?.input_tokens ?? 0) + (overall?.output_tokens ?? 0) }}
            </div>
            <div class="sum-sub">
              输入 {{ overall?.input_tokens ?? 0 }} / 输出 {{ overall?.output_tokens ?? 0 }}
            </div>
          </div>
        </el-col>
        <el-col :md="6" :sm="12">
          <div class="sum-card cost">
            <div class="sum-title">累计扣费</div>
            <div class="sum-value">{{ formatCredit(overall?.credit_cost) }}</div>
            <div class="sum-sub">单位:积分</div>
          </div>
        </el-col>
      </el-row>
    </div>

    <!-- 每日柱状 + 模型 TOP -->
    <div class="card-block">
      <div class="flex-between" style="margin-bottom:12px">
        <div>
          <h3 class="page-title" style="margin:0;font-size:16px">每日请求</h3>
          <div class="muted" style="margin-top:4px">
            近 {{ statsFilter.days }} 天趋势 · 悬停柱条查看详情
          </div>
        </div>
        <div class="legend">
          <span class="legend-dot requests"></span><span class="legend-label">请求总数</span>
          <span class="legend-dot failures"></span><span class="legend-label">失败</span>
        </div>
      </div>

      <div ref="chartWrap" class="chart-wrap" v-loading="statsLoading">
        <el-empty
          v-if="!statsLoading && daily.length === 0"
          description="暂无数据"
          :image-size="80"
          style="padding:24px 0"
        />
        <svg
          v-else
          class="chart-svg"
          :viewBox="`0 0 ${chartW} ${chartH}`"
          :style="{ height: chartH + 'px' }"
          @mouseleave="onChartLeave"
        >
          <!-- 渐变定义 -->
          <defs>
            <linearGradient id="barGradPrimary" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stop-color="var(--el-color-primary)" stop-opacity="1" />
              <stop offset="100%" stop-color="var(--el-color-primary)" stop-opacity="0.55" />
            </linearGradient>
            <linearGradient id="barGradDanger" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stop-color="var(--el-color-danger)" stop-opacity="0.95" />
              <stop offset="100%" stop-color="var(--el-color-danger)" stop-opacity="0.65" />
            </linearGradient>
          </defs>

          <!-- y 轴网格 + 刻度 -->
          <g class="chart-axis">
            <line
              v-for="(t, ti) in yTicks" :key="'ty' + ti"
              :x1="padL" :x2="chartW - padR"
              :y1="padT + (chartH - padT - padB) * (1 - t / yMax)"
              :y2="padT + (chartH - padT - padB) * (1 - t / yMax)"
              class="grid-line"
              :class="{ 'grid-zero': ti === 0 }"
            />
            <text
              v-for="(t, ti) in yTicks" :key="'tl' + ti"
              :x="padL - 8"
              :y="padT + (chartH - padT - padB) * (1 - t / yMax) + 4"
              text-anchor="end"
              class="axis-tick"
            >{{ t }}</text>
          </g>

          <!-- 柱条 + 交互区 -->
          <g
            v-for="(p, i) in daily" :key="p.day"
            class="bar-group"
            :class="{ hover: hoverIdx === i }"
            @mouseenter="onCellEnter(i)"
          >
            <!-- 整列透明交互矩形(扩大悬停命中区) -->
            <rect
              :x="padL + cellW * i" :y="padT"
              :width="cellW"
              :height="chartH - padT - padB"
              class="bar-hit"
            />
            <!-- 成功柱 -->
            <rect
              :x="xCenter(i) - barW / 2"
              :y="barY(p.requests)"
              :width="barW"
              :height="barH(p.requests)"
              rx="3"
              fill="url(#barGradPrimary)"
              class="bar-rect"
            />
            <!-- 失败覆盖 -->
            <rect
              v-if="p.failures"
              :x="xCenter(i) - barW / 2"
              :y="barY(p.failures)"
              :width="barW"
              :height="barH(p.failures)"
              rx="3"
              fill="url(#barGradDanger)"
              class="bar-rect bar-rect-fail"
            />
            <!-- x 轴日期 -->
            <text
              v-if="shouldShowLabel(i)"
              :x="xCenter(i)"
              :y="chartH - padB + 16"
              text-anchor="middle"
              class="axis-date"
            >{{ p.day.slice(5) }}</text>
          </g>

          <!-- 悬停指示线 -->
          <line
            v-if="hoverIdx >= 0"
            :x1="tipX" :x2="tipX"
            :y1="padT" :y2="chartH - padB"
            class="hover-guide"
          />

          <!-- 悬停 tooltip -->
          <g v-if="hoverIdx >= 0" class="tip-group">
            <foreignObject
              :x="tipSide === 'right' ? tipX + 10 : tipX - 170"
              :y="Math.max(padT, tipY - 58)"
              width="160" height="70"
            >
              <div class="chart-tip">
                <div class="tip-day">{{ daily[hoverIdx]?.day }}</div>
                <div class="tip-row">
                  <span class="tip-dot requests"></span>请求
                  <b>{{ daily[hoverIdx]?.requests || 0 }}</b>
                </div>
                <div class="tip-row">
                  <span class="tip-dot failures"></span>失败
                  <b>{{ daily[hoverIdx]?.failures || 0 }}</b>
                </div>
              </div>
            </foreignObject>
          </g>
        </svg>
      </div>

      <div style="margin-top:18px">
        <h3 class="page-title" style="margin:0 0 10px;font-size:16px">模型 TOP</h3>
        <el-table :data="byModel" stripe size="small" v-loading="statsLoading" empty-text="暂无数据">
          <el-table-column label="模型" min-width="180">
            <template #default="{ row }">
              <code>{{ row.model_slug || `#${row.model_id}` }}</code>
            </template>
          </el-table-column>
          <el-table-column prop="type" label="类型" width="80">
            <template #default="{ row }">
              <el-tag size="small" :type="row.type === 'image' ? 'warning' : 'primary'">
                {{ row.type || '-' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="requests" label="请求数" width="100" />
          <el-table-column prop="failures" label="失败" width="80" />
          <el-table-column prop="input_tokens" label="输入 tok" width="110" />
          <el-table-column prop="output_tokens" label="输出 tok" width="110" />
          <el-table-column prop="image_count" label="图数" width="80" />
          <el-table-column label="扣费" width="120">
            <template #default="{ row }">{{ formatCredit(row.credit_cost) }}</template>
          </el-table-column>
          <el-table-column prop="avg_dur_ms" label="平均耗时(ms)" width="130" />
        </el-table>
      </div>
    </div>

    <!-- 明细 Tabs -->
    <div class="card-block">
      <el-tabs v-model="activeTab" @tab-change="onTabChange">
        <!-- ----- 请求日志 ----- -->
        <el-tab-pane name="logs" label="请求日志">
          <div class="flex-between" style="margin-bottom:12px">
            <div class="flex-wrap-gap">
              <el-select v-model="logFilter.type" style="width:120px" clearable placeholder="类型"
                         @change="refreshLogs">
                <el-option label="全部" value="" />
                <el-option v-if="ENABLE_CHAT_MODEL" label="对话" value="chat" />
                <el-option label="生图" value="image" />
              </el-select>
              <el-select v-model="logFilter.status" style="width:120px" clearable placeholder="状态"
                         @change="refreshLogs">
                <el-option label="全部" value="" />
                <el-option label="成功" value="success" />
                <el-option label="失败" value="failed" />
              </el-select>
            </div>
            <el-button :loading="logLoading" @click="refreshLogs">
              <el-icon><Refresh /></el-icon> 刷新
            </el-button>
          </div>

          <el-table :data="logItems" stripe size="small" v-loading="logLoading" empty-text="暂无记录">
            <el-table-column prop="created_at" label="时间" width="170">
              <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="模型" min-width="150">
              <template #default="{ row }">
                <code>{{ row.model_slug || `#${row.model_id}` }}</code>
              </template>
            </el-table-column>
            <el-table-column label="类型" width="80">
              <template #default="{ row }">
                <el-tag size="small" :type="row.type === 'image' ? 'warning' : 'primary'">
                  {{ row.type || '-' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="90">
              <template #default="{ row }">
                <el-tag size="small" :type="statusTag(row.status)">
                  {{ statusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="input_tokens" label="输入 tok" width="95" />
            <el-table-column prop="output_tokens" label="输出 tok" width="95" />
            <el-table-column label="图数" width="70">
              <template #default="{ row }">{{ row.image_count || 0 }}</template>
            </el-table-column>
            <el-table-column label="扣费" width="110">
              <template #default="{ row }">
                <span class="cost">{{ formatCredit(row.credit_cost) }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="duration_ms" label="耗时(ms)" width="100" />
            <el-table-column label="Request ID" min-width="180">
              <template #default="{ row }">
                <span class="req-id" :title="row.request_id">{{ row.request_id || '-' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="错误" min-width="160">
              <template #default="{ row }">
                <el-tooltip v-if="row.error_code" :content="row.error_code" placement="top">
                  <span class="err">{{ formatErrorCode(row.error_code) }}</span>
                </el-tooltip>
                <span v-else class="muted">-</span>
              </template>
            </el-table-column>
          </el-table>

          <div class="flex-end" style="margin-top:12px">
            <el-pagination background layout="total, prev, pager, next, sizes"
              :total="logTotal"
              v-model:current-page="logPage"
              v-model:page-size="logFilter.limit"
              :page-sizes="[10, 20, 50, 100]"
              @size-change="refreshLogs" />
          </div>
        </el-tab-pane>

        <!-- ----- 积分流水 ----- -->
        <el-tab-pane name="credits" label="积分流水">
          <div class="flex-between" style="margin-bottom:12px">
            <div class="muted">
              展示账户所有账变:充值、消费、退款、管理员调账等。金额为正表示收入,负表示支出。
            </div>
            <el-button :loading="creditLoading" @click="loadCredits">
              <el-icon><Refresh /></el-icon> 刷新
            </el-button>
          </div>

          <el-table :data="creditItems" stripe size="small" v-loading="creditLoading" empty-text="暂无账变">
            <el-table-column prop="created_at" label="时间" width="170">
              <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="类型" width="100">
              <template #default="{ row }">
                <el-tag size="small">{{ typeLabel[row.type] || row.type }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="金额" width="140">
              <template #default="{ row }">
                <span :class="row.amount >= 0 ? 'amount-in' : 'amount-out'">
                  {{ row.amount >= 0 ? '+' : '' }}{{ formatCredit(row.amount) }}
                </span>
              </template>
            </el-table-column>
            <el-table-column label="余额" width="140">
              <template #default="{ row }">{{ formatCredit(row.balance_after) }}</template>
            </el-table-column>
            <el-table-column label="Key" width="90">
              <template #default="{ row }">
                <span v-if="row.key_id">#{{ row.key_id }}</span>
                <span v-else class="muted">-</span>
              </template>
            </el-table-column>
            <el-table-column label="关联" min-width="160">
              <template #default="{ row }">
                <span v-if="row.ref_id" class="ref">{{ row.ref_id }}</span>
                <span v-else class="muted">-</span>
              </template>
            </el-table-column>
            <el-table-column prop="remark" label="备注" min-width="200" show-overflow-tooltip />
          </el-table>

          <div class="flex-end" style="margin-top:12px">
            <el-pagination background layout="total, prev, pager, next, sizes"
              :total="creditTotal"
              v-model:current-page="creditPage"
              v-model:page-size="creditFilter.limit"
              :page-sizes="[10, 20, 50, 100]"
              @size-change="loadCredits" />
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<style scoped lang="scss">
.page-title { font-size: 18px; font-weight: 600; }
.page-sub {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin-top: 4px;
}
.muted { color: var(--el-text-color-secondary); font-size: 13px; }

.flex-end { display: flex; justify-content: flex-end; }

/* ---- 概览小卡 ---- */
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
.sum-card.requests::before { background: var(--el-color-primary); }
.sum-card.chat::before     { background: var(--el-color-success); }
.sum-card.token::before    { background: var(--el-color-warning); }
.sum-card.cost::before     { background: var(--el-color-danger); }

.sum-title { font-size: 13px; color: var(--el-text-color-secondary); }
.sum-value {
  font-size: 26px;
  font-weight: 700;
  margin: 8px 0 4px;
  color: var(--el-text-color-primary);
  line-height: 1.15;
}
.sum-sub { font-size: 12px; color: var(--el-text-color-secondary); }

/* ---- 图表图例 ---- */
.legend {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  flex-wrap: wrap;
}
.legend-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  display: inline-block;
  margin-left: 8px;
}
.legend-dot.requests { background: var(--el-color-primary); }
.legend-dot.failures { background: var(--el-color-danger); }
.legend-label { margin-right: 4px; }

/* ---- 每日请求 SVG 图表 ---- */
.chart-wrap {
  width: 100%;
  min-height: 220px;
  position: relative;
}
.chart-svg {
  width: 100%;
  display: block;
  font-family: ui-sans-serif, system-ui, -apple-system, 'Segoe UI', sans-serif;
  user-select: none;
}

/* 坐标轴刻度与网格 */
.chart-svg .grid-line {
  stroke: var(--el-border-color-lighter);
  stroke-width: 1;
  stroke-dasharray: 3 4;
}
.chart-svg .grid-line.grid-zero {
  stroke: var(--el-border-color);
  stroke-dasharray: none;
}
.chart-svg .axis-tick {
  fill: var(--el-text-color-placeholder);
  font-size: 11px;
}
.chart-svg .axis-date {
  fill: var(--el-text-color-secondary);
  font-size: 11px;
}

/* 柱条 */
.chart-svg .bar-group { cursor: pointer; }
.chart-svg .bar-hit { fill: transparent; }
.chart-svg .bar-rect {
  transition: opacity .15s;
}
.chart-svg .bar-group:hover .bar-hit {
  fill: var(--el-fill-color-light);
  opacity: 0.6;
}
.chart-svg .bar-group .bar-rect {
  filter: drop-shadow(0 1px 1px rgba(0, 0, 0, 0.06));
}
.chart-svg .bar-group:not(.hover) .bar-rect {
  opacity: 0.92;
}

/* hover 指示线 */
.chart-svg .hover-guide {
  stroke: var(--el-color-primary);
  stroke-width: 1;
  stroke-dasharray: 3 3;
  opacity: 0.45;
  pointer-events: none;
}

/* tooltip */
.chart-tip {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.08);
  padding: 8px 10px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-primary);
  pointer-events: none;
}
.chart-tip .tip-day {
  font-weight: 600;
  margin-bottom: 2px;
  color: var(--el-text-color-regular);
}
.chart-tip .tip-row { display: flex; align-items: center; }
.chart-tip .tip-row b { margin-left: auto; font-weight: 600; }
.chart-tip .tip-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  display: inline-block;
  margin-right: 6px;
}
.chart-tip .tip-dot.requests { background: var(--el-color-primary); }
.chart-tip .tip-dot.failures { background: var(--el-color-danger); }

/* ---- 表格内细节 ---- */
code {
  background: var(--el-fill-color-light);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 12px;
}
.req-id {
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 12px;
  color: var(--el-text-color-regular);
  display: inline-block;
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: middle;
}
.err { color: var(--el-color-danger); font-size: 12px; }
.cost { font-weight: 600; color: var(--el-color-danger); }
.amount-in  { color: var(--el-color-success); font-weight: 600; }
.amount-out { color: var(--el-color-danger);  font-weight: 600; }
.ref {
  background: var(--el-fill-color-light);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 12px;
}
</style>
