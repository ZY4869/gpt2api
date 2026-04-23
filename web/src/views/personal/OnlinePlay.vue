<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { storeToRefs } from 'pinia'
import { useUserStore } from '@/stores/user'
import { formatCredit } from '@/utils/format'
import { pollMixedImageTask, taskToMixedImages } from '@/utils/mixedImageTask'
import { pickDefaultChatModel, preferredThinkingModelSlug, sortChatModelsForDisplay } from '@/utils/models'
import {
  listMyModels,
  streamPlayChat,
  playGenerateImage,
  type SimpleModel,
  type PlayChatMessage,
  type PlayImageData,
  type PlayMixedImage,
  type PlayMixedImageTask,
} from '@/api/me'
import { ENABLE_CHAT_IMAGE_MIXED, ENABLE_CHAT_MODEL } from '@/config/feature'

// ----------------------------------------------------
// 用户 / 模型
// ----------------------------------------------------
const userStore = useUserStore()
const { user } = storeToRefs(userStore)

const balance = computed(() => formatCredit(user.value?.credit_balance))

const models = ref<SimpleModel[]>([])
const chatModels = computed(() => sortChatModelsForDisplay(models.value.filter((m) => m.type === 'chat')))
const imageModels = computed(() => models.value.filter((m) => m.type === 'image'))

const selectedChatModel = ref('')
const selectedImageModel = ref('')

const selectedChatMeta = computed(
  () => chatModels.value.find((m) => m.slug === selectedChatModel.value) || null,
)
const preferredThinkingSlug = computed(() => preferredThinkingModelSlug(chatModels.value))
const selectedChatIsThinking = computed(() => !!selectedChatMeta.value?.is_thinking)
const currentChatDesc = computed(() => selectedChatMeta.value?.description || '')
const currentImageDesc = computed(
  () => imageModels.value.find((m) => m.slug === selectedImageModel.value)?.description || '',
)

onMounted(async () => {
  try {
    await userStore.fetchMe()
  } catch {
    /* ignore */
  }
  try {
    const m = await listMyModels()
    // feature flag 关闭时,前端直接把 chat 类型的模型从列表过滤掉,
    // 保证 chatModels / imageModels / selectedChatModel 等下游 state 都不会
    // 拿到 chat 模型(即便模板里还有残留引用)。
    models.value = ENABLE_CHAT_MODEL
      ? m.items
      : m.items.filter((x) => x.type !== 'chat')
    const firstChat = pickDefaultChatModel(m.items.filter((x) => x.type === 'chat'))
    const firstImage = m.items.find((x) => x.type === 'image')
    if (firstChat) selectedChatModel.value = firstChat
    if (firstImage) selectedImageModel.value = firstImage.slug
  } catch {
    // 静默;错误拦截器已提示
  }
})

// ----------------------------------------------------
// Tabs
// ----------------------------------------------------
const activeTab = ref<'chat' | 'text2img' | 'img2img'>(
  ENABLE_CHAT_MODEL ? 'chat' : 'text2img',
)

// ====================================================
// 对话(Chat)
// ====================================================
interface UIMessage {
  id: number
  role: 'user' | 'assistant' | 'system'
  reasoning?: string
  content: string
  images?: PlayMixedImage[]
  imageTask?: PlayMixedImageTask
  pending?: boolean
  error?: boolean
  at: number
}

let uid = 0

const systemPrompt = ref('你是一个友好、博学、回答精准的中文助手。回答中若涉及代码请使用 Markdown 代码块。')
const temperature = ref(0.7)
const chatImageMode = ref(false)
const chatInput = ref('')
const chatMsgs = ref<UIMessage[]>([])
const chatSending = ref(false)
const chatAbort = ref<AbortController | null>(null)
const chatScroll = ref<HTMLElement | null>(null)
const inputRef = ref<any>(null)
const canUseChatImageMixed = ENABLE_CHAT_IMAGE_MIXED
let chatTaskPollAbort: AbortController | null = null

const suggestions = [
  { icon: '💡', title: '向我解释', sub: '量子纠缠到底是什么?' },
  { icon: '✍️', title: '帮我写作', sub: '一段 200 字的产品发布文案' },
  { icon: '🧑‍💻', title: '写段代码', sub: 'Go 实现令牌桶限流器' },
  { icon: '🌏', title: '中英互译', sub: '把上面这段翻译为英文' },
]

function useSuggestion(s: typeof suggestions[number]) {
  chatInput.value = `${s.title}:${s.sub}`
  nextTick(() => inputRef.value?.focus?.())
}

async function scrollChat(force = false) {
  await nextTick()
  const el = chatScroll.value
  if (!el) return
  if (force) {
    el.scrollTop = el.scrollHeight
    return
  }
  const gap = el.scrollHeight - el.scrollTop - el.clientHeight
  if (gap < 180) el.scrollTop = el.scrollHeight
}

async function sendChat() {
  if (chatSending.value) return
  const text = chatInput.value.trim()
  if (!text) return
  if (!selectedChatModel.value) {
    ElMessage.warning('请选择一个文字模型')
    return
  }
  stopMixedModeTaskPolling()
  const now = Date.now()
  chatMsgs.value.push({ id: ++uid, role: 'user', content: text, at: now })
  chatInput.value = ''
  const assistant: UIMessage = {
    id: ++uid,
    role: 'assistant',
    reasoning: '',
    content: '',
    images: [],
    pending: true,
    at: now,
  }
  chatMsgs.value.push(assistant)
  await scrollChat(true)

  const history: PlayChatMessage[] = []
  if (systemPrompt.value.trim()) {
    history.push({ role: 'system', content: systemPrompt.value.trim() })
  }
  for (const m of chatMsgs.value.slice(0, -1)) {
    if (m.error) continue
    history.push({
      role: m.role as 'user' | 'assistant' | 'system',
      content: m.content || (m.images?.length ? '[assistant generated image]' : ''),
    })
  }

  chatSending.value = true
  chatAbort.value = new AbortController()
  try {
    const result = await streamPlayChat(
      {
        model: selectedChatModel.value,
        messages: history,
        temperature: temperature.value,
        image_generation: chatImageMode.value,
      },
      {
        onReasoningDelta: (delta) => {
          assistant.reasoning = (assistant.reasoning || '') + delta
          assistant.pending = false
          scrollChat()
        },
        onContentDelta: (delta) => {
          assistant.content += delta
          assistant.pending = false
          scrollChat()
        },
        onImages: (images) => {
          assistant.images = images
          if (!assistant.imageTask || assistant.imageTask.status !== 'running') {
            assistant.pending = false
          }
          void startMixedModeTaskPolling(assistant, images, assistant.imageTask)
          scrollChat()
        },
        onImageTask: (task) => {
          assistant.imageTask = task
          assistant.pending = task.status === 'running'
          if (task.status === 'running') {
            void startMixedModeTaskPolling(assistant, assistant.images || [], task)
          }
          scrollChat()
        },
      },
      chatAbort.value.signal,
    )
    if (!assistant.reasoning && result.reasoning) assistant.reasoning = result.reasoning
    if (!assistant.content && result.content) assistant.content = result.content
    if (result.imageTask) assistant.imageTask = result.imageTask
    if ((!assistant.images || assistant.images.length === 0) && result.images.length) assistant.images = result.images
    if (result.imageTask?.status === 'running' || result.images.length) {
      void startMixedModeTaskPolling(assistant, result.images, result.imageTask)
    }
    if (result.imageTask?.status !== 'running') {
      assistant.pending = false
    }
    if (!assistant.reasoning && !assistant.content && !assistant.images?.length && result.imageTask?.status !== 'running') {
      assistant.content = '(无输出)'
    }
  } catch (err: unknown) {
    assistant.pending = false
    assistant.error = true
    const msg = err instanceof Error ? err.message : String(err)
    assistant.content = assistant.content || `请求失败:${msg}`
    ElMessage.error(msg)
  } finally {
    chatSending.value = false
    chatAbort.value = null
    scrollChat()
    userStore.fetchMe().catch(() => {})
  }
}

function stopChat() {
  chatAbort.value?.abort()
  stopMixedModeTaskPolling()
}

function resetChat() {
  if (chatSending.value) stopChat()
  stopMixedModeTaskPolling()
  chatMsgs.value = []
}

async function regenerate() {
  if (chatSending.value) return
  // 去掉最后一条 assistant,把最后一条 user 重发
  let lastUserIdx = -1
  for (let i = chatMsgs.value.length - 1; i >= 0; i--) {
    if (chatMsgs.value[i].role === 'user') { lastUserIdx = i; break }
  }
  if (lastUserIdx < 0) return
  const lastUserText = chatMsgs.value[lastUserIdx].content
  chatMsgs.value = chatMsgs.value.slice(0, lastUserIdx)
  chatInput.value = lastUserText
  await sendChat()
}

function copyText(s: string) {
  try {
    navigator.clipboard.writeText(s)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败')
  }
}

function messageCopyText(m: UIMessage): string {
  return [m.reasoning, m.content].filter((x) => !!x).join('\n\n')
}

function stopMixedModeTaskPolling() {
  chatTaskPollAbort?.abort()
  chatTaskPollAbort = null
}

async function startMixedModeTaskPolling(
  assistant: UIMessage,
  images: PlayMixedImage[],
  imageTask?: PlayMixedImageTask,
) {
  const taskID = imageTask?.task_id || images[0]?.task_id
  if (!chatImageMode.value || !taskID) return

  stopMixedModeTaskPolling()
  const controller = new AbortController()
  chatTaskPollAbort = controller
  assistant.pending = true

  try {
    await pollMixedImageTask(taskID, {
      signal: controller.signal,
      onUpdate: (taskImages, task) => {
        assistant.imageTask = {
          task_id: task.task_id,
          status: task.status,
          requested_n: task.n,
          ready_n: taskImages.length,
          conversation_id: task.conversation_id,
        }
        if (taskImages.length >= (assistant.images?.length || 0)) {
          assistant.images = taskImages.length ? taskImages : taskToMixedImages(task)
          assistant.pending = task.status === 'running' && taskImages.length < Math.max(task.n || 1, 1)
          scrollChat()
        }
      },
    })
  } catch (err: unknown) {
    if (!(err instanceof DOMException && err.name === 'AbortError')) {
      /* ignore */
    }
  } finally {
    if (assistant.imageTask?.status !== 'running') {
      assistant.pending = false
    }
    if (chatTaskPollAbort === controller) chatTaskPollAbort = null
  }
}

onBeforeUnmount(() => {
  chatAbort.value?.abort()
  stopMixedModeTaskPolling()
})

// ---------- 轻量 markdown 渲染(代码块 / 行内代码 / 粗体 / 链接) ----------
function escapeHtml(s: string) {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function renderMarkdown(raw: string): string {
  if (!raw) return ''
  const parts: string[] = []
  const blocks = raw.split(/```/g) // ``` 成对切分
  for (let i = 0; i < blocks.length; i++) {
    const chunk = blocks[i]
    if (i % 2 === 1) {
      // 代码块:首行可能是 lang
      const nl = chunk.indexOf('\n')
      let lang = ''
      let code = chunk
      if (nl >= 0) {
        const head = chunk.slice(0, nl).trim()
        if (/^[a-zA-Z0-9+_\-]{1,20}$/.test(head)) {
          lang = head
          code = chunk.slice(nl + 1)
        }
      }
      parts.push(
        `<pre class="mdk-pre" data-lang="${escapeHtml(lang || '')}"><code>${escapeHtml(
          code.replace(/\n$/, ''),
        )}</code></pre>`,
      )
    } else {
      // 行内元素
      let html = escapeHtml(chunk)
      // 行内代码 `xxx`
      html = html.replace(/`([^`\n]+)`/g, '<code class="mdk-ic">$1</code>')
      // 粗体 **xxx**
      html = html.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>')
      // 自动链接
      html = html.replace(
        /(https?:\/\/[\w\-._~:/?#\[\]@!$&'()*+,;=%]+)/g,
        '<a href="$1" target="_blank" rel="noopener">$1</a>',
      )
      // 换行
      html = html.replace(/\n/g, '<br />')
      parts.push(html)
    }
  }
  return parts.join('')
}

// ====================================================
// 文生图(Text2Img)
// ====================================================
const t2iPrompt = ref('')
const t2iSize = ref<'1024x1024' | '1792x1024' | '1024x1792'>('1024x1024')
const t2iN = ref(1)
const t2iSending = ref(false)
const t2iResult = ref<PlayImageData[]>([])
const t2iPreview = ref(false)
const t2iError = ref('')
const t2iAbort = ref<AbortController | null>(null)

const imgExamples = [
  '赛博朋克城市夜景,霓虹雨夜,电影感光影,8k',
  '一只金色胖柴犬穿西装坐在办公桌前,油画质感',
  '极简几何海报,蓝橙配色,主体是一只展翅的鹤',
  '童话风格蘑菇屋,黄昏光线,柔和景深',
]

async function sendText2Img() {
  const prompt = t2iPrompt.value.trim()
  if (!prompt) {
    ElMessage.warning('请输入描述词 prompt')
    return
  }
  if (!selectedImageModel.value) {
    ElMessage.warning('请选择一个图片模型')
    return
  }
  t2iSending.value = true
  t2iError.value = ''
  t2iPreview.value = false
  t2iResult.value = []
  t2iAbort.value = new AbortController()
  try {
    const resp = await playGenerateImage(
      {
        model: selectedImageModel.value,
        prompt,
        n: t2iN.value,
        size: t2iSize.value,
      },
      t2iAbort.value.signal,
    )
    t2iResult.value = resp.data || []
    t2iPreview.value = !!resp.is_preview
    if (t2iResult.value.length === 0) {
      t2iError.value = '未产出图片,请重试或更换描述'
    } else if (t2iPreview.value) {
      ElMessage.warning(`生成成功(预览模式):本次账号未命中 IMG2 灰度,展示的是 IMG1 预览图`)
    } else {
      ElMessage.success(`生成成功,共 ${t2iResult.value.length} 张`)
    }
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err)
    t2iError.value = msg
    ElMessage.error(msg)
  } finally {
    t2iSending.value = false
    t2iAbort.value = null
    userStore.fetchMe().catch(() => {})
  }
}

function stopText2Img() {
  t2iAbort.value?.abort()
}

// 预览 viewer
const previewVisible = ref(false)
const previewList = ref<string[]>([])
const previewIndex = ref(0)
function openPreview(urls: string[], idx: number) {
  previewList.value = urls
  previewIndex.value = idx
  previewVisible.value = true
}
function downloadUrl(url: string) {
  const a = document.createElement('a')
  a.href = url
  a.target = '_blank'
  a.rel = 'noopener'
  a.download = ''
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
}

// ====================================================
// 图生图(Img2Img)
// ====================================================
interface RefImage {
  name: string
  dataUrl: string
  size: number
}
const refImages = ref<RefImage[]>([])
const i2iPrompt = ref('')
const i2iSize = ref<'1024x1024' | '1792x1024' | '1024x1792'>('1024x1024')
const i2iSending = ref(false)
const i2iResult = ref<PlayImageData[]>([])
const i2iPreview = ref(false)
const i2iError = ref('')
const i2iAbort = ref<AbortController | null>(null)
const MAX_REF_BYTES = 4 * 1024 * 1024 // 4MB

function handleFilePick(e: Event) {
  const input = e.target as HTMLInputElement
  const files = input.files
  if (!files) return
  for (const file of Array.from(files)) {
    if (file.size > MAX_REF_BYTES) {
      ElMessage.warning(`${file.name} 超过 4MB 限制`)
      continue
    }
    const reader = new FileReader()
    reader.onload = () => {
      refImages.value.push({
        name: file.name,
        dataUrl: String(reader.result || ''),
        size: file.size,
      })
    }
    reader.readAsDataURL(file)
  }
  input.value = ''
}

function removeRefImage(idx: number) {
  refImages.value.splice(idx, 1)
}

async function sendImg2Img() {
  if (refImages.value.length === 0) {
    ElMessage.warning('请先上传至少一张参考图')
    return
  }
  if (!i2iPrompt.value.trim()) {
    ElMessage.warning('请描述希望的改动')
    return
  }
  if (!selectedImageModel.value) {
    ElMessage.warning('请选择一个图片模型')
    return
  }
  i2iSending.value = true
  i2iError.value = ''
  i2iPreview.value = false
  i2iResult.value = []
  i2iAbort.value = new AbortController()
  try {
    const resp = await playGenerateImage(
      {
        model: selectedImageModel.value,
        prompt: i2iPrompt.value.trim(),
        n: 1,
        size: i2iSize.value,
        reference_images: refImages.value.map((r) => r.dataUrl),
      },
      i2iAbort.value.signal,
    )
    i2iResult.value = resp.data || []
    i2iPreview.value = !!resp.is_preview
    if (i2iPreview.value && i2iResult.value.length > 0) {
      ElMessage.warning('生成成功(预览模式):本次账号未命中 IMG2 灰度,展示的是 IMG1 预览图')
    }
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err)
    i2iError.value = msg
    ElMessage.error(msg)
  } finally {
    i2iSending.value = false
    i2iAbort.value = null
  }
}

// 代码块内的 "复制" 按钮(通过事件委托,避免每次重渲染都重新绑定)
function onMsgClick(e: MouseEvent) {
  const t = e.target as HTMLElement | null
  if (!t) return
  const btn = t.closest('.mdk-copy') as HTMLElement | null
  if (!btn) return
  const pre = btn.parentElement?.querySelector('code')
  if (!pre) return
  copyText(pre.textContent || '')
}

// input 自动聚焦(tab 切换后)
watch(activeTab, (v) => {
  if (v === 'chat') nextTick(() => inputRef.value?.focus?.())
})
</script>

<template>
  <div class="page-container playground">
    <!-- ============ Hero(紧凑条) ============ -->
    <div class="hero card-block">
      <div class="hero-left">
        <el-icon class="hero-ic"><MagicStick /></el-icon>
        <div class="hero-txt">
          <h2 class="hero-title">在线体验</h2>
          <span class="hero-sub">
            浏览器中直接调用 GPT {{ ENABLE_CHAT_MODEL ? '文字 / ' : '' }}图像模型 · 文生图 & 图生图 · 同一账号池、同一套计费,记录同步到「使用记录」
          </span>
        </div>
      </div>
      <div class="hero-stats">
        <div class="mini-stat">
          <span class="mini-num">{{ balance }}</span>
          <span class="mini-lbl">积分</span>
        </div>
        <template v-if="ENABLE_CHAT_MODEL">
          <span class="mini-dot" />
          <div class="mini-stat">
            <span class="mini-num">{{ chatModels.length }}</span>
            <span class="mini-lbl">文字模型</span>
          </div>
        </template>
        <span class="mini-dot" />
        <div class="mini-stat">
          <span class="mini-num">{{ imageModels.length }}</span>
          <span class="mini-lbl">图片模型</span>
        </div>
      </div>
    </div>

    <!-- ============ Tabs ============ -->
    <el-tabs v-model="activeTab" class="pg-tabs">
      <!-- =================================================== -->
      <!--                          Chat                         -->
      <!-- =================================================== -->
      <el-tab-pane v-if="ENABLE_CHAT_MODEL" name="chat">
        <template #label>
          <span class="tab-lbl"><el-icon><ChatDotRound /></el-icon> 对话</span>
        </template>

        <div class="chat-grid">
          <!-- 左侧:模型 + System + 温度 -->
          <aside class="card-block side">
            <div class="side-row">
              <label class="side-lbl">文字模型</label>
              <el-select v-model="selectedChatModel" placeholder="选择文字模型" size="large" style="width:100%">
                <el-option v-for="m in chatModels" :key="m.id" :label="m.slug" :value="m.slug">
                  <div class="opt-row">
                    <span class="opt-slug">{{ m.slug }}</span>
                    <div class="opt-tags">
                      <el-tag v-if="m.is_thinking" size="small" type="success" effect="plain">thinking</el-tag>
                      <el-tag v-else size="small" type="primary" effect="plain">chat</el-tag>
                    </div>
                  </div>
                </el-option>
              </el-select>
              <div v-if="currentChatDesc" class="side-hint">{{ currentChatDesc }}</div>
            </div>

            <div v-if="canUseChatImageMixed" class="side-row">
              <label class="side-lbl">生图模式</label>
              <el-switch
                v-model="chatImageMode"
                inline-prompt
                active-text="开"
                inactive-text="关"
              />
              <div class="side-hint">开启后会在同一轮 chat 请求里触发生图,思考过程会实时显示,图片会在流末尾返回。</div>
              <div v-if="chatImageMode && !selectedChatIsThinking" class="side-hint side-warn">
                当前选择的是非 thinking 模型,本次 mixed-mode 仍可生图,但不会强制触发思考过程。若要稳定展示思考,请改用 {{ preferredThinkingSlug || 'gpt-5-thinking' }}。
              </div>
            </div>

            <div class="side-row">
              <label class="side-lbl">
                Temperature
                <span class="side-val">{{ temperature.toFixed(1) }}</span>
              </label>
              <el-slider v-model="temperature" :min="0" :max="2" :step="0.1" show-stops />
              <div class="side-hint">越低越保守、越高越发散。默认 0.7</div>
            </div>

            <div class="side-row">
              <label class="side-lbl">System Prompt</label>
              <el-input
                v-model="systemPrompt"
                type="textarea"
                :rows="6"
                resize="none"
                placeholder="为助手设定人格与风格"
              />
            </div>

            <el-button :disabled="chatMsgs.length === 0" @click="resetChat" class="side-btn">
              <el-icon><Delete /></el-icon> 清空会话
            </el-button>
          </aside>

          <!-- 右侧:聊天主区 -->
          <section class="card-block chat-main">
            <header class="chat-header">
              <div class="chat-title">
                <el-avatar :size="32" class="avatar-bot">
                  <el-icon><Cpu /></el-icon>
                </el-avatar>
                <div>
                  <div class="chat-model">{{ selectedChatModel || '未选择模型' }}</div>
                  <div class="chat-sub">
                    {{ chatSending
                      ? (chatImageMode
                        ? (selectedChatIsThinking ? '正在思考并生成图片…' : '正在生成图片…')
                        : (selectedChatIsThinking ? '正在思考并回复…' : '正在回复…'))
                      : (chatMsgs.length ? `${chatMsgs.length} 条消息 · ${chatImageMode ? '生图模式' : '文本模式'}` : '准备就绪') }}
                  </div>
                </div>
              </div>
              <div class="chat-tools">
                <el-tooltip content="重试上一个问题" placement="top">
                  <el-button
                    :disabled="chatSending || chatMsgs.length === 0"
                    circle
                    @click="regenerate"
                  >
                    <el-icon><RefreshRight /></el-icon>
                  </el-button>
                </el-tooltip>
              </div>
            </header>

            <div ref="chatScroll" class="chat-scroll" @click="onMsgClick">
              <!-- 空态:建议卡 -->
              <div v-if="chatMsgs.length === 0" class="welcome">
                <div class="welcome-hi">
                  👋 你好{{ user?.nickname ? ',' + user.nickname : '' }}
                </div>
                <div class="welcome-sub">选一个话题开始,或者直接在下方输入。</div>
                <div class="suggest-grid">
                  <div
                    v-for="(s, i) in suggestions"
                    :key="i"
                    class="suggest-card"
                    @click="useSuggestion(s)"
                  >
                    <div class="s-ic">{{ s.icon }}</div>
                    <div class="s-t">{{ s.title }}</div>
                    <div class="s-s">{{ s.sub }}</div>
                  </div>
                </div>
              </div>

              <!-- 消息列表 -->
              <article
                v-for="m in chatMsgs"
                :key="m.id"
                :class="['msg', m.role, m.error ? 'err' : '']"
              >
                <el-avatar :size="34" :class="m.role === 'user' ? 'avatar-user' : 'avatar-bot'">
                  <el-icon v-if="m.role === 'user'"><User /></el-icon>
                  <el-icon v-else><MagicStick /></el-icon>
                </el-avatar>
                <div class="msg-body">
                  <div class="msg-head">
                    <span class="who">{{ m.role === 'user' ? '我' : '助手' }}</span>
                    <span v-if="!m.pending && messageCopyText(m)" class="copy-btn" @click="copyText(messageCopyText(m))">
                      <el-icon><CopyDocument /></el-icon> 复制
                    </span>
                  </div>
                  <div class="msg-content">
                    <div v-if="m.pending && !m.reasoning && !m.content && !m.images?.length" class="typing">
                      <span></span><span></span><span></span>
                    </div>
                    <div v-if="m.reasoning" class="reasoning-block">
                      <div class="reasoning-head">
                        <el-icon><Cpu /></el-icon>
                        <span>{{ m.pending ? '实时思考中' : '思考过程' }}</span>
                      </div>
                      <div
                        class="md reasoning-md"
                        v-html="renderMarkdown(m.reasoning)"
                      />
                    </div>
                    <div
                      v-if="m.content"
                      class="md"
                      v-html="renderMarkdown(m.content)"
                    />
                    <div v-if="m.pending && (m.reasoning || m.content) && !m.images?.length" class="reasoning-status">
                      {{ chatImageMode ? '图片仍在准备中…' : '回答仍在生成中…' }}
                    </div>
                    <div v-if="m.images?.length" class="chat-image-grid">
                      <div
                        v-for="(img, idx) in m.images"
                        :key="idx"
                        class="img-cell"
                        :class="{ 'is-preview': !!img.is_preview }"
                        @click="openPreview(m.images.map((x) => x.url), idx)"
                      >
                        <img :src="img.url" :alt="`chat-image-${idx}`" />
                        <div v-if="img.is_preview" class="img-badge">预览图</div>
                        <div class="img-actions" @click.stop>
                          <button class="iact" @click="openPreview(m.images.map((x) => x.url), idx)">
                            <el-icon><ZoomIn /></el-icon>
                          </button>
                          <button class="iact" @click="downloadUrl(img.url)">
                            <el-icon><Download /></el-icon>
                          </button>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </article>
            </div>

            <!-- 输入条 -->
            <div class="composer" :class="{ focused: !!chatInput }">
              <el-input
                ref="inputRef"
                v-model="chatInput"
                type="textarea"
                :rows="1"
                :autosize="{ minRows: 1, maxRows: 6 }"
                resize="none"
                placeholder="给助手发消息…  Enter 发送,Shift+Enter 换行"
                @keydown.enter.exact.prevent="sendChat"
              />
              <div class="composer-tools">
                <span class="hint">
                  <el-icon><InfoFilled /></el-icon>
                  {{ chatImageMode
                    ? (selectedChatIsThinking
                      ? '当前为 thinking 生图模式,会实时展示思考过程并在结束后返回图片'
                      : '当前模型不是 thinking 模型,本次会生图,但不保证展示思考过程')
                    : '按 Enter 发送' }}
                </span>
                <div style="flex:1" />
                <el-button v-if="chatSending" type="danger" @click="stopChat" round>
                  <el-icon><VideoPause /></el-icon> 停止
                </el-button>
                <el-button
                  v-else
                  type="primary"
                  :disabled="!chatInput.trim() || !selectedChatModel"
                  @click="sendChat"
                  round
                >
                  {{ chatImageMode ? '生成图片' : '发送' }}
                  <el-icon style="margin-left:4px"><Promotion /></el-icon>
                </el-button>
              </div>
            </div>
          </section>
        </div>
      </el-tab-pane>

      <!-- =================================================== -->
      <!--                        文生图                         -->
      <!-- =================================================== -->
      <el-tab-pane name="text2img">
        <template #label>
          <span class="tab-lbl"><el-icon><Picture /></el-icon> 文生图</span>
        </template>

        <div class="img-grid">
          <aside class="card-block side">
            <div class="side-row">
              <label class="side-lbl">图片模型</label>
              <el-select v-model="selectedImageModel" placeholder="选择图片模型" size="large" style="width:100%">
                <el-option v-for="m in imageModels" :key="m.id" :label="m.slug" :value="m.slug">
                  <div class="opt-row">
                    <span class="opt-slug">{{ m.slug }}</span>
                    <el-tag size="small" type="warning" effect="plain">image</el-tag>
                  </div>
                </el-option>
              </el-select>
              <div v-if="currentImageDesc" class="side-hint">{{ currentImageDesc }}</div>
            </div>

            <div class="side-row">
              <label class="side-lbl">画面比例</label>
              <div class="ratio-row">
                <button
                  v-for="opt in [
                    { v: '1024x1024', l: '1:1',  w: 36, h: 36 },
                    { v: '1792x1024', l: '16:9', w: 48, h: 28 },
                    { v: '1024x1792', l: '9:16', w: 28, h: 48 },
                  ]"
                  :key="opt.v"
                  :class="['ratio-btn', { active: t2iSize === opt.v }]"
                  @click="t2iSize = opt.v as any"
                >
                  <div class="ratio-box" :style="{ width: opt.w + 'px', height: opt.h + 'px' }" />
                  <span>{{ opt.l }}</span>
                </button>
              </div>
            </div>

            <div class="side-row">
              <label class="side-lbl">张数 <span class="side-val">{{ t2iN }}</span></label>
              <el-slider v-model="t2iN" :min="1" :max="4" show-stops />
            </div>

            <div class="side-row">
              <label class="side-lbl">Prompt</label>
              <el-input
                v-model="t2iPrompt"
                type="textarea"
                :rows="5"
                resize="none"
                placeholder="描述画面的主体、风格、光线、构图…越具体效果越好"
              />
              <div class="chips">
                <el-tag
                  v-for="(p, i) in imgExamples"
                  :key="i"
                  effect="plain"
                  round
                  class="chip"
                  @click="t2iPrompt = p"
                >{{ p }}</el-tag>
              </div>
            </div>

            <el-button v-if="t2iSending" type="danger" @click="stopText2Img" round class="side-btn">
              <el-icon><VideoPause /></el-icon> 停止
            </el-button>
            <el-button
              v-else
              type="primary"
              round
              size="large"
              :disabled="!t2iPrompt.trim() || !selectedImageModel"
              @click="sendText2Img"
              class="side-btn gen-btn"
            >
              <el-icon><MagicStick /></el-icon> 生成图片
            </el-button>
          </aside>

          <section class="card-block img-main">
            <div v-if="t2iSending" class="stage loading">
              <div class="orb"><el-icon class="spin"><Loading /></el-icon></div>
              <div class="stage-title">正在为你绘制…</div>
              <div class="stage-sub">上游渲染通常需要 1-2 分钟,请保持页面打开</div>
            </div>
            <div v-else-if="t2iError" class="err-block">
              <el-icon><WarningFilled /></el-icon>
              {{ t2iError }}
            </div>
            <div v-else-if="t2iResult.length === 0" class="stage">
              <div class="stage-art">🖼️</div>
              <div class="stage-title">还没有图片</div>
              <div class="stage-sub">在左侧填好 prompt 和参数,点击「生成图片」</div>
            </div>
            <div v-else class="result-wrap">
              <el-alert
                v-if="t2iPreview"
                class="preview-tip"
                type="warning"
                :closable="false"
                show-icon
                title="本次未使用 IMG2 灰度生成"
                description="上游没有把本账号放入 IMG2 终稿通道,返回的是 IMG1 预览图;效果略简化,属于正常降级,可多试几次或更换账号。"
              />
              <div class="result-grid">
                <div
                  v-for="(img, idx) in t2iResult"
                  :key="idx"
                  class="img-cell"
                  :class="{ 'is-preview': t2iPreview }"
                  @click="openPreview(t2iResult.map((x) => x.url), idx)"
                >
                  <img :src="img.url" :alt="`result-${idx}`" loading="lazy" />
                  <div v-if="t2iPreview" class="img-badge">IMG1 预览</div>
                  <div class="img-actions" @click.stop>
                    <button class="iact" @click="openPreview(t2iResult.map((x) => x.url), idx)">
                      <el-icon><ZoomIn /></el-icon>
                    </button>
                    <button class="iact" @click="downloadUrl(img.url)">
                      <el-icon><Download /></el-icon>
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </el-tab-pane>

      <!-- =================================================== -->
      <!--                        图生图                         -->
      <!-- =================================================== -->
      <el-tab-pane name="img2img">
        <template #label>
          <span class="tab-lbl"><el-icon><PictureFilled /></el-icon> 图生图</span>
        </template>

        <div class="img-grid">
          <aside class="card-block side">
            <div class="side-row">
              <label class="side-lbl">图片模型</label>
              <el-select v-model="selectedImageModel" placeholder="选择图片模型" size="large" style="width:100%">
                <el-option v-for="m in imageModels" :key="m.id" :label="m.slug" :value="m.slug" />
              </el-select>
            </div>

            <div class="side-row">
              <label class="side-lbl">参考图 <span class="side-val">{{ refImages.length }}/多</span></label>
              <label class="upload-zone">
                <el-icon class="up-ic"><UploadFilled /></el-icon>
                <div class="up-t">点击选择 / 拖拽图片到这里</div>
                <div class="up-s">最多多张,每张 ≤ 4MB</div>
                <input type="file" accept="image/*" multiple @change="handleFilePick" />
              </label>

              <div v-if="refImages.length" class="ref-grid">
                <div v-for="(r, idx) in refImages" :key="idx" class="ref-thumb">
                  <img :src="r.dataUrl" :alt="r.name" />
                  <div class="ref-x" @click="removeRefImage(idx)">
                    <el-icon><Close /></el-icon>
                  </div>
                  <div class="ref-meta">{{ (r.size / 1024).toFixed(0) }} KB</div>
                </div>
              </div>
            </div>

            <div class="side-row">
              <label class="side-lbl">输出比例</label>
              <div class="ratio-row">
                <button
                  v-for="opt in [
                    { v: '1024x1024', l: '1:1',  w: 36, h: 36 },
                    { v: '1792x1024', l: '16:9', w: 48, h: 28 },
                    { v: '1024x1792', l: '9:16', w: 28, h: 48 },
                  ]"
                  :key="opt.v"
                  :class="['ratio-btn', { active: i2iSize === opt.v }]"
                  @click="i2iSize = opt.v as any"
                >
                  <div class="ratio-box" :style="{ width: opt.w + 'px', height: opt.h + 'px' }" />
                  <span>{{ opt.l }}</span>
                </button>
              </div>
            </div>

            <div class="side-row">
              <label class="side-lbl">希望如何改动</label>
              <el-input
                v-model="i2iPrompt"
                type="textarea"
                :rows="4"
                resize="none"
                placeholder="例:保持人物姿态,把背景换成赛博朋克夜景"
              />
            </div>

            <el-button
              type="primary"
              round
              size="large"
              :loading="i2iSending"
              :disabled="refImages.length === 0 || !i2iPrompt.trim()"
              @click="sendImg2Img"
              class="side-btn gen-btn"
            >
              <el-icon><MagicStick /></el-icon> 生成
            </el-button>
          </aside>

          <section class="card-block img-main">
            <el-alert
              type="warning"
              :closable="false"
              title="图生图目前处于 Preview"
              description="上游 ChatGPT 文件上传协议还在接入,当前提交会返回 501。UI 已准备就绪,协议完成后即刻可用。"
              show-icon
              style="margin-bottom: 14px; border-radius: 10px;"
            />

            <div v-if="i2iError" class="err-block">
              <el-icon><WarningFilled /></el-icon>
              {{ i2iError }}
            </div>
            <div v-else-if="i2iSending" class="stage loading">
              <div class="orb"><el-icon class="spin"><Loading /></el-icon></div>
              <div class="stage-title">正在生成…</div>
            </div>
            <div v-else-if="i2iResult.length === 0" class="stage">
              <div class="stage-art">🎨</div>
              <div class="stage-title">还没有结果</div>
              <div class="stage-sub">上传参考图 + 描述改动,然后点击「生成」</div>
            </div>
            <div v-else class="result-wrap">
              <el-alert
                v-if="i2iPreview"
                class="preview-tip"
                type="warning"
                :closable="false"
                show-icon
                title="本次未使用 IMG2 灰度生成"
                description="上游没有把本账号放入 IMG2 终稿通道,返回的是 IMG1 预览图。"
              />
              <div class="result-grid">
                <div
                  v-for="(img, idx) in i2iResult"
                  :key="idx"
                  class="img-cell"
                  :class="{ 'is-preview': i2iPreview }"
                  @click="openPreview(i2iResult.map((x) => x.url), idx)"
                >
                  <img :src="img.url" :alt="`result-${idx}`" />
                  <div v-if="i2iPreview" class="img-badge">IMG1 预览</div>
                  <div class="img-actions" @click.stop>
                    <button class="iact" @click="openPreview(i2iResult.map((x) => x.url), idx)">
                      <el-icon><ZoomIn /></el-icon>
                    </button>
                    <button class="iact" @click="downloadUrl(img.url)">
                      <el-icon><Download /></el-icon>
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- ============ 图片预览(全屏 viewer) ============ -->
    <el-image-viewer
      v-if="previewVisible"
      :url-list="previewList"
      :initial-index="previewIndex"
      @close="previewVisible = false"
      teleported
    />
  </div>
</template>

<style scoped lang="scss">
.playground { padding-bottom: 24px; }

/* ====================== Hero(紧凑条) ====================== */
.hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 18px !important;
  margin-bottom: 14px !important;
}
.hero-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  flex: 1;
}
.hero-ic {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  flex-shrink: 0;
}
.hero-txt {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
  flex-wrap: wrap;
}
.hero-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  white-space: nowrap;
}
.hero-sub {
  font-size: 12.5px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}
.hero-stats {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 0;
}
.mini-stat {
  display: inline-flex;
  align-items: baseline;
  gap: 4px;
  .mini-num {
    font-size: 14px;
    font-weight: 600;
    color: var(--el-color-primary);
  }
  .mini-lbl {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
}
.mini-dot {
  width: 3px; height: 3px; border-radius: 50%;
  background: var(--el-border-color);
}

/* ====================== Tabs ====================== */
.pg-tabs {
  :deep(.el-tabs__header) { margin-bottom: 16px; }
  :deep(.el-tabs__nav-wrap::after) { background: var(--el-border-color-lighter); }
  :deep(.el-tabs__item) {
    font-size: 14px;
    font-weight: 500;
    padding: 0 18px;
  }
  :deep(.el-tabs__item.is-active) { font-weight: 600; }
}
.tab-lbl { display: inline-flex; align-items: center; gap: 6px; }

/* ====================== Side ====================== */
.side { display: flex; flex-direction: column; gap: 16px; height: fit-content; position: sticky; top: 12px; }
.side-row { display: flex; flex-direction: column; gap: 6px; }
.side-lbl {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
  display: flex; justify-content: space-between; align-items: center;
  letter-spacing: 0.3px;
  text-transform: uppercase;
}
.side-val { font-weight: 600; color: var(--el-color-primary); letter-spacing: 0; text-transform: none; font-size: 13px; }
.side-hint { font-size: 12px; color: var(--el-text-color-placeholder); line-height: 1.5; }
.side-warn { color: var(--el-color-warning-dark-2); }
.side-btn { margin-top: 4px; }
.gen-btn { box-shadow: 0 6px 18px -6px rgba(64, 158, 255, 0.55); }
.opt-row { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.opt-tags { display: inline-flex; align-items: center; gap: 6px; }
.opt-slug { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 13px; }

/* ====================== Chat ====================== */
.chat-grid {
  display: grid;
  grid-template-columns: 300px minmax(0, 1fr);
  gap: 16px;
  min-height: 620px;
}
.chat-main {
  display: flex; flex-direction: column;
  padding: 0;
  overflow: hidden;
  height: 720px;
}
.chat-header {
  display: flex; justify-content: space-between; align-items: center;
  padding: 12px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  background: linear-gradient(180deg, var(--el-bg-color) 0%, var(--el-fill-color-lighter) 100%);
}
.chat-title { display: flex; align-items: center; gap: 10px; }
.chat-model { font-size: 14px; font-weight: 600; color: var(--el-text-color-primary); }
.chat-sub { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 2px; }
.chat-tools { display: flex; gap: 6px; }

.chat-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 22px 24px;
  scroll-behavior: smooth;
}

/* ----- 欢迎 ----- */
.welcome {
  min-height: 100%;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  padding: 30px 20px;
}
.welcome-hi {
  font-size: 24px; font-weight: 700;
  color: var(--el-text-color-primary);
  margin-bottom: 6px;
}
.welcome-sub { color: var(--el-text-color-secondary); margin-bottom: 22px; font-size: 14px; }
.suggest-grid {
  display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px;
  width: 100%; max-width: 680px;
}
.suggest-card {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 12px;
  padding: 14px 16px;
  cursor: pointer;
  background: var(--el-bg-color);
  transition: all 0.2s;
  .s-ic { font-size: 20px; margin-bottom: 4px; }
  .s-t { font-size: 13px; font-weight: 600; color: var(--el-text-color-primary); }
  .s-s { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; line-height: 1.5; }
  &:hover {
    border-color: var(--el-color-primary);
    transform: translateY(-1px);
    box-shadow: 0 6px 18px -8px rgba(64, 158, 255, 0.35);
  }
}

/* ----- 消息 ----- */
.msg {
  display: flex;
  gap: 12px;
  padding: 14px 0;
  border-bottom: 1px dashed var(--el-border-color-lighter);
  animation: fadeIn 0.25s ease;
  &:last-child { border-bottom: none; }
  &.err .msg-content { color: var(--el-color-danger); }
}
@keyframes fadeIn {
  from { opacity: 0; transform: translateY(4px); }
  to   { opacity: 1; transform: translateY(0); }
}
.avatar-user {
  background: var(--el-color-primary);
  color: #fff;
  flex-shrink: 0;
}
.avatar-bot {
  background: var(--el-color-success);
  color: #fff;
  flex-shrink: 0;
}
.msg-body { flex: 1; min-width: 0; }
.msg-head {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 4px;
  .who { font-size: 12px; font-weight: 600; color: var(--el-text-color-secondary); }
  .copy-btn {
    font-size: 12px; color: var(--el-text-color-placeholder); cursor: pointer;
    display: inline-flex; align-items: center; gap: 2px;
    opacity: 0; transition: opacity 0.2s;
    &:hover { color: var(--el-color-primary); }
  }
}
.msg:hover .copy-btn { opacity: 1; }
.msg-content {
  font-size: 14px; line-height: 1.75;
  color: var(--el-text-color-primary);
  word-break: break-word;
}
.reasoning-block {
  margin-bottom: 12px;
  padding: 12px 14px;
  border-radius: 12px;
  border: 1px solid var(--el-border-color-lighter);
  background: linear-gradient(180deg, var(--el-fill-color-lighter) 0%, var(--el-bg-color) 100%);
}
.reasoning-head {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
  font-size: 12px;
  font-weight: 600;
  color: var(--el-color-primary);
}
.reasoning-md {
  color: var(--el-text-color-regular);
}
.reasoning-status {
  margin-top: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.chat-image-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 10px;
  margin-top: 10px;
}

/* markdown 渲染产物 */
.md :deep(.mdk-pre) {
  background: #0f172a;
  color: #e2e8f0;
  padding: 12px 14px;
  border-radius: 10px;
  overflow-x: auto;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 12.5px;
  line-height: 1.6;
  margin: 8px 0;
  position: relative;
  &::before {
    content: attr(data-lang);
    position: absolute;
    top: 6px; right: 10px;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: #94a3b8;
    opacity: 0.8;
  }
}
.md :deep(.mdk-ic) {
  background: var(--el-fill-color);
  color: var(--el-color-primary);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 12.5px;
}
.md :deep(a) { color: var(--el-color-primary); text-decoration: none; }
.md :deep(a:hover) { text-decoration: underline; }
.md :deep(strong) { font-weight: 600; }

/* typing 指示器 */
.typing {
  display: inline-flex; gap: 5px; padding: 4px 0;
  span {
    width: 7px; height: 7px; border-radius: 50%;
    background: var(--el-color-primary);
    animation: blink 1.4s infinite ease-in-out both;
  }
  span:nth-child(2) { animation-delay: 0.2s; }
  span:nth-child(3) { animation-delay: 0.4s; }
}
@keyframes blink {
  0%, 80%, 100% { opacity: 0.2; transform: scale(0.7); }
  40% { opacity: 1; transform: scale(1); }
}

/* ----- 输入条 ----- */
.composer {
  padding: 12px 18px 16px;
  border-top: 1px solid var(--el-border-color-lighter);
  background: var(--el-bg-color);
  transition: box-shadow 0.2s;
  :deep(.el-textarea__inner) {
    border-radius: 12px;
    padding: 10px 14px;
    font-size: 14px;
    box-shadow: none;
    border: 1px solid var(--el-border-color);
    transition: border-color 0.2s, box-shadow 0.2s;
    &:focus {
      border-color: var(--el-color-primary);
      box-shadow: 0 0 0 3px rgba(64, 158, 255, 0.15);
    }
  }
}
.composer-tools {
  display: flex; align-items: center; gap: 8px; margin-top: 10px;
  .hint {
    display: inline-flex; align-items: center; gap: 4px;
    font-size: 12px; color: var(--el-text-color-placeholder);
  }
}

/* ====================== 图片面板 ====================== */
.img-grid {
  display: grid;
  grid-template-columns: 340px minmax(0, 1fr);
  gap: 16px;
}
.img-main { min-height: 560px; }

/* 比例按钮 */
.ratio-row { display: flex; gap: 8px; }
.ratio-btn {
  flex: 1;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  padding: 10px 0 8px;
  cursor: pointer;
  display: flex; flex-direction: column; align-items: center; gap: 6px;
  font-size: 12px; color: var(--el-text-color-secondary);
  transition: all 0.15s;
  .ratio-box {
    background: var(--el-fill-color-light);
    border-radius: 2px;
    border: 1px solid var(--el-border-color-lighter);
  }
  &:hover {
    border-color: var(--el-color-primary);
    color: var(--el-color-primary);
  }
  &.active {
    border-color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
    color: var(--el-color-primary);
    font-weight: 600;
    .ratio-box {
      background: var(--el-color-primary);
      border-color: var(--el-color-primary);
    }
  }
}

/* prompt chips */
.chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.chip { cursor: pointer; max-width: 100%; overflow: hidden; text-overflow: ellipsis; }
.chip:hover { background: var(--el-color-primary-light-9); color: var(--el-color-primary); }

/* 上传区 */
.upload-zone {
  position: relative;
  display: flex; flex-direction: column; align-items: center;
  padding: 20px 12px;
  border: 2px dashed var(--el-border-color);
  border-radius: 12px;
  cursor: pointer;
  background: var(--el-fill-color-lighter);
  transition: all 0.2s;
  &:hover { border-color: var(--el-color-primary); background: var(--el-color-primary-light-9); }
  .up-ic { font-size: 32px; color: var(--el-color-primary); }
  .up-t { font-size: 13px; margin-top: 6px; color: var(--el-text-color-primary); }
  .up-s { font-size: 11px; color: var(--el-text-color-placeholder); margin-top: 2px; }
  input { position: absolute; inset: 0; opacity: 0; cursor: pointer; }
}

.ref-grid {
  margin-top: 10px;
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}
.ref-thumb {
  position: relative;
  aspect-ratio: 1;
  border-radius: 8px;
  overflow: hidden;
  background: var(--el-fill-color-light);
  img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .ref-x {
    position: absolute; top: 4px; right: 4px;
    width: 20px; height: 20px; border-radius: 50%;
    background: rgba(0,0,0,0.55); color: #fff;
    display: flex; align-items: center; justify-content: center;
    cursor: pointer; font-size: 12px;
    opacity: 0; transition: opacity 0.2s;
  }
  .ref-meta {
    position: absolute; bottom: 0; left: 0; right: 0;
    padding: 2px 6px;
    background: linear-gradient(transparent, rgba(0,0,0,0.6));
    color: #fff; font-size: 10px;
    opacity: 0; transition: opacity 0.2s;
  }
  &:hover { .ref-x, .ref-meta { opacity: 1; } }
}

/* 主区 stage / 结果 */
.stage {
  min-height: 480px;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  text-align: center; color: var(--el-text-color-secondary); padding: 40px 24px;
  .stage-art { font-size: 64px; margin-bottom: 16px; opacity: 0.7; }
  .stage-title { font-size: 16px; font-weight: 600; color: var(--el-text-color-primary); }
  .stage-sub { font-size: 13px; margin-top: 6px; }
  &.loading { gap: 14px; }
  .orb {
    width: 72px; height: 72px; border-radius: 50%;
    background: var(--el-color-primary-light-8);
    display: flex; align-items: center; justify-content: center;
    animation: pulseOrb 1.8s ease-in-out infinite;
  }
}
@keyframes pulseOrb {
  0%, 100% { transform: scale(1); box-shadow: 0 0 0 0 var(--el-color-primary-light-5); }
  50%      { transform: scale(1.08); box-shadow: 0 0 0 14px rgba(64,158,255,0); }
}
.spin { font-size: 30px; animation: spin 1s linear infinite; color: var(--el-color-primary); }
@keyframes spin { to { transform: rotate(360deg); } }

.err-block {
  background: var(--el-color-danger-light-9);
  color: var(--el-color-danger);
  padding: 12px 14px;
  border-radius: 10px;
  display: flex; align-items: center; gap: 8px;
  white-space: pre-wrap; word-break: break-word;
  border: 1px solid var(--el-color-danger-light-5);
}

.result-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 14px;
  padding: 4px;
}
.img-cell {
  position: relative;
  aspect-ratio: 1;
  border-radius: 12px;
  overflow: hidden;
  cursor: zoom-in;
  background: var(--el-fill-color-light);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
  transition: all 0.2s;
  img {
    width: 100%; height: 100%; object-fit: cover; display: block;
    transition: transform 0.4s;
  }
  &:hover {
    transform: translateY(-2px);
    box-shadow: 0 10px 24px rgba(0, 0, 0, 0.12);
    img { transform: scale(1.03); }
    .img-actions { opacity: 1; }
  }
}
.img-actions {
  position: absolute; top: 8px; right: 8px;
  display: flex; gap: 6px;
  opacity: 0; transition: opacity 0.2s;
  .iact {
    width: 30px; height: 30px; border-radius: 50%;
    background: rgba(0,0,0,0.55); color: #fff;
    border: none; cursor: pointer;
    display: inline-flex; align-items: center; justify-content: center;
    font-size: 14px;
    &:hover { background: var(--el-color-primary); }
  }
}

/* IMG1 预览兜底专用样式 */
.result-wrap {
  display: flex; flex-direction: column; gap: 10px;
  padding: 4px;
  .result-grid { padding: 0; }
  .preview-tip { border-radius: 10px; }
}
.img-cell.is-preview {
  box-shadow: 0 2px 8px rgba(251, 146, 60, 0.25);
  &::after {
    content: ''; position: absolute; inset: 0;
    border: 1.5px dashed rgba(245, 158, 11, 0.55);
    border-radius: 12px;
    pointer-events: none;
  }
}
.img-badge {
  position: absolute;
  left: 8px; top: 8px;
  padding: 2px 8px;
  font-size: 11px;
  border-radius: 999px;
  background: rgba(245, 158, 11, 0.92);
  color: #fff;
  letter-spacing: 0.3px;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.12);
  pointer-events: none;
}

/* ====================== Dark mode ====================== */
:global(html.dark) .md :deep(.mdk-pre) {
  background: #0b1020;
  color: #cbd5e1;
}

/* ====================== Responsive ====================== */
@media (max-width: 1100px) {
  .chat-grid, .img-grid { grid-template-columns: 1fr; }
  .side { position: static; }
  .chat-main { height: 580px; }
}
@media (max-width: 720px) {
  .hero {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }
  .hero-sub { display: none; }
  .hero-stats { width: 100%; justify-content: flex-start; }
}
</style>
