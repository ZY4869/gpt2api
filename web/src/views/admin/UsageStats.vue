<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import * as statsApi from '@/api/stats'
import { formatCredit } from '@/utils/format'
import { ENABLE_CHAT_MODEL } from '@/config/feature'

const loading = ref(false)
const data = ref<statsApi.StatsResp | null>(null)
const filter = reactive({ days: 14, type: '' as '' | 'chat' | 'image', status: '' as '' | 'success' | 'failed' })

async function load() {
  loading.value = true
  try {
    data.value = await statsApi.getUsageStats({
      days: filter.days,
      top_n: 10,
      type: filter.type || undefined,
      status: filter.status || undefined,
    })
  } finally { loading.value = false }
}

const overall = computed(() => data.value?.overall)
const daily = computed(() => data.value?.daily || [])
const byModel = computed(() => data.value?.by_model || [])
const byUser = computed(() => data.value?.by_user || [])

const maxDaily = computed(() => {
  const m = daily.value.reduce((x, r) => Math.max(x, r.requests), 0)
  return m === 0 ? 1 : m
})

function successRate(s?: statsApi.Overall) {
  if (!s || s.requests === 0) return '—'
  return `${(((s.requests - s.failures) / s.requests) * 100).toFixed(2)}%`
}

onMounted(load)
</script>

<template>
  <div class="page-container">
    <div class="card-block">
      <div class="flex-between" style="margin-bottom:12px">
        <div>
          <h2 class="page-title" style="margin:0">用量统计</h2>
          <div style="color:var(--el-text-color-secondary);font-size:13px;margin-top:4px">
            全站请求量 / 成功率 / Token 消耗 / 积分收入的聚合视图,支持按天、类型、状态切片,用于观察趋势与定位异常账号。
          </div>
        </div>
        <div class="flex-wrap-gap">
          <el-select v-model="filter.days" style="width:110px" @change="load">
            <el-option :value="7"  label="近 7 天" />
            <el-option :value="14" label="近 14 天" />
            <el-option :value="30" label="近 30 天" />
            <el-option :value="60" label="近 60 天" />
            <el-option :value="90" label="近 90 天" />
          </el-select>
          <el-select v-model="filter.type" style="width:130px" clearable placeholder="类型" @change="load">
            <el-option label="全部" value="" />
            <el-option v-if="ENABLE_CHAT_MODEL" label="对话" value="chat" />
            <el-option label="生图" value="image" />
          </el-select>
          <el-select v-model="filter.status" style="width:130px" clearable placeholder="状态" @change="load">
            <el-option label="全部" value="" />
            <el-option label="成功" value="success" />
            <el-option label="失败" value="failed" />
          </el-select>
          <el-button @click="load" type="primary" :loading="loading">
            <el-icon><Refresh /></el-icon> 刷新
          </el-button>
        </div>
      </div>

      <el-row :gutter="16" v-loading="loading">
        <el-col :md="6" :sm="12"><el-card shadow="never" class="stat">
          <div class="lbl">请求数</div>
          <div class="val">{{ overall?.requests ?? 0 }}</div>
          <div class="sub">失败 {{ overall?.failures ?? 0 }} · 成功率 {{ successRate(overall!) }}</div>
        </el-card></el-col>
        <el-col v-if="ENABLE_CHAT_MODEL" :md="6" :sm="12"><el-card shadow="never" class="stat">
          <div class="lbl">对话请求</div>
          <div class="val">{{ overall?.chat_requests ?? 0 }}</div>
          <div class="sub">生图张数 {{ overall?.image_images ?? 0 }}</div>
        </el-card></el-col>
        <el-col v-else :md="6" :sm="12"><el-card shadow="never" class="stat">
          <div class="lbl">生图张数</div>
          <div class="val">{{ overall?.image_images ?? 0 }}</div>
          <div class="sub">成功率 {{ successRate(overall!) }}</div>
        </el-card></el-col>
        <el-col :md="6" :sm="12"><el-card shadow="never" class="stat">
          <div class="lbl">Token 消耗</div>
          <div class="val">{{ (overall?.input_tokens ?? 0) + (overall?.output_tokens ?? 0) }}</div>
          <div class="sub">输入 {{ overall?.input_tokens ?? 0 }} / 输出 {{ overall?.output_tokens ?? 0 }}</div>
        </el-card></el-col>
        <el-col :md="6" :sm="12"><el-card shadow="never" class="stat">
          <div class="lbl">累计扣费</div>
          <div class="val primary">{{ formatCredit(overall?.credit_cost) }} 分</div>
          <div class="sub">单位:积分</div>
        </el-card></el-col>
      </el-row>
    </div>

    <div class="card-block">
      <h3 style="margin:0 0 10px;font-size:14px">每日请求(纯 CSS 柱状图)</h3>
      <div class="bars">
        <div v-for="p in daily" :key="p.day" class="bar-cell">
          <div class="bar-wrap">
            <div class="bar" :style="{ height: ((p.requests / maxDaily) * 100) + '%' }" />
            <div v-if="p.failures" class="bar-fail"
                 :style="{ height: ((p.failures / maxDaily) * 100) + '%' }" />
          </div>
          <div class="bar-val">{{ p.requests }}</div>
          <div class="bar-day">{{ p.day.slice(5) }}</div>
        </div>
        <el-empty v-if="daily.length === 0" description="无数据" />
      </div>
    </div>

    <div class="card-block">
      <h3 style="margin:0 0 10px;font-size:14px">Top 模型</h3>
      <el-table :data="byModel" stripe size="small" v-loading="loading">
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
        <el-table-column prop="input_tokens" label="输入 tok" width="120" />
        <el-table-column prop="output_tokens" label="输出 tok" width="120" />
        <el-table-column prop="image_count" label="图数" width="80" />
        <el-table-column label="扣费" width="130">
          <template #default="{ row }">{{ formatCredit(row.credit_cost) }}</template>
        </el-table-column>
        <el-table-column prop="avg_dur_ms" label="平均耗时(ms)" width="130" />
      </el-table>
    </div>

    <div class="card-block">
      <h3 style="margin:0 0 10px;font-size:14px">Top 用户</h3>
      <el-table :data="byUser" stripe size="small" v-loading="loading">
        <el-table-column prop="user_id" label="ID" width="80" />
        <el-table-column prop="email" label="邮箱" min-width="200" />
        <el-table-column prop="requests" label="请求数" width="120" />
        <el-table-column prop="failures" label="失败" width="100" />
        <el-table-column label="扣费" width="140">
          <template #default="{ row }">{{ formatCredit(row.credit_cost) }}</template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<style scoped lang="scss">
.stat {
  border-radius: 8px;
  .lbl { font-size: 13px; color: var(--el-text-color-secondary); }
  .val {
    font-size: 26px; font-weight: 700; margin: 6px 0;
    color: var(--el-text-color-primary);
    &.primary { color: #409eff; }
  }
  .sub { font-size: 12px; color: var(--el-text-color-secondary); }
}
.bars {
  display: flex; gap: 4px;
  height: 160px;
  align-items: flex-end;
  .bar-cell {
    flex: 1; display: flex; flex-direction: column; align-items: center;
    min-width: 0;
  }
  .bar-wrap { position: relative; width: 100%; height: 100%; display: flex; align-items: flex-end; }
  .bar {
    width: 60%; margin: 0 auto;
    background: linear-gradient(180deg, #409eff, #67c23a);
    border-radius: 3px 3px 0 0;
    min-height: 2px;
    transition: height .3s;
  }
  .bar-fail {
    position: absolute; bottom: 0; left: 20%; right: 20%;
    background: #f56c6c; border-radius: 3px 3px 0 0;
    opacity: 0.6;
  }
  .bar-val { font-size: 11px; color: var(--el-text-color-secondary); margin-top: 2px; }
  .bar-day { font-size: 11px; color: var(--el-text-color-placeholder); }
}
code {
  background: #f2f3f5;
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 12px;
}
:global(html.dark) code { background: #1d2026; }
</style>
