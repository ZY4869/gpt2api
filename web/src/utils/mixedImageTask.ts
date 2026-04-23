import { getMyImageTask, type ImageTask, type PlayMixedImage } from '@/api/me'

export interface PollMixedImageTaskOptions {
  signal?: AbortSignal
  intervalMs?: number
  timeoutMs?: number
  onUpdate?: (images: PlayMixedImage[], task: ImageTask) => void
}

export function taskToMixedImages(task: ImageTask): PlayMixedImage[] {
  return (task.image_urls || []).map((url, idx) => ({
    url,
    file_id: task.file_ids?.[idx],
    content_type: 'image/png',
    task_id: task.task_id,
  }))
}

export async function pollMixedImageTask(
  taskID: string,
  options: PollMixedImageTaskOptions = {},
): Promise<ImageTask | null> {
  const intervalMs = options.intervalMs ?? 3000
  const timeoutMs = options.timeoutMs ?? 60000
  const startedAt = Date.now()

  while (Date.now() - startedAt <= timeoutMs) {
    throwIfAborted(options.signal)

    const task = await getMyImageTask(taskID)
    const images = taskToMixedImages(task)
    options.onUpdate?.(images, task)

    if (task.status === 'failed') return task
    if (images.length >= Math.max(task.n || 1, 1)) return task

    await sleep(intervalMs, options.signal)
  }
  return null
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => {
      cleanup()
      resolve()
    }, ms)
    const onAbort = () => {
      cleanup()
      reject(abortError())
    }
    const cleanup = () => {
      window.clearTimeout(timer)
      signal?.removeEventListener('abort', onAbort)
    }
    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

function throwIfAborted(signal?: AbortSignal) {
  if (signal?.aborted) throw abortError()
}

function abortError(): Error {
  return new DOMException('Aborted', 'AbortError')
}
