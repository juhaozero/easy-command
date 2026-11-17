<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { execution, type FinalStatus, type RunArgs, type RunEvent } from '../api/execution'
import { engine } from '../engine'
import { presetValuesToArgs, type ScriptMeta } from '../api/scripts'

const props = defineProps<{ script: ScriptMeta }>()
const values = ref<RunArgs>(Object.create(null))
const presetId = ref('')
const confirmRun = ref<HTMLDialogElement>()
const runId = ref('')
const runningName = ref('')
const phase = ref<'idle' | 'starting' | 'running' | 'interrupted' | FinalStatus>('idle')
const statusLabels = {
  idle: '待运行', starting: '正在启动', running: '运行中', interrupted: '引擎连接中断',
  succeeded: '成功', failed: '失败', cancelled: '已取消',
}
const exitCode = ref<number | null>(null)
const error = ref('')
const historyWarning = ref('')
const log = shallowRef<Array<{ stream: string; text: string }>>([])
const bashPath = ref('')
const detected = ref('')
const connected = ref(false)
const ready = ref(false)
const cancelling = ref(false)
const active = computed(() => phase.value === 'starting' || phase.value === 'running')
const presets = computed(() => props.script.presets ?? [])
const hasParams = computed(() => props.script.params.some((param) => param.type !== 'password'))
const hasPresets = computed(() => presets.value.length > 0)
const selectedPreset = computed(() => presets.value.find((item) => item.id === presetId.value))
let buffered: RunEvent[] = []
let masked: string[] = []
let disposed = false
const stop: Array<() => void> = []
const maxLog = 2 * 1024 * 1024
let logSize = 0
let engineRevision = -1

function applyPreset(id?: string) {
  const list = presets.value
  const chosen = list.find((item) => item.id === (id || presetId.value))
    ?? list.find((item) => item.id === props.script.defaultPresetId)
    ?? list[0]
  presetId.value = chosen?.id ?? ''
  if (!chosen) {
    const next: RunArgs = Object.create(null)
    for (const param of props.script.params) {
      if (param.type !== 'password') next[param.key] = param.default ?? null
    }
    values.value = next
    return
  }
  values.value = presetValuesToArgs(props.script.params, chosen.values)
}

watch(() => props.script, () => {
  applyPreset(props.script.defaultPresetId)
}, { immediate: true })

watch(presetId, (id) => {
  if (!id) return
  const chosen = presets.value.find((item) => item.id === id)
  if (chosen) values.value = presetValuesToArgs(props.script.params, chosen.values)
})

const preview = computed(() => {
  const args: string[] = []
  const environment: string[] = []
  for (const param of props.script.params) {
    if (param.type === 'password') {
      if (param.cli.mode === 'env') environment.push(`${param.cli.envName}=***`)
      else {
        if (param.cli.mode === 'flag' && param.cli.flag) args.push(param.cli.flag)
        args.push('***')
      }
      continue
    }
    if (param.cli.mode === 'env') continue
    const value = values.value[param.key]
    if (param.cli.mode === 'flag') {
      if (value === true && param.cli.flag) args.push(param.maskInLog ? '***' : JSON.stringify(param.cli.flag))
    } else if (value !== null && value !== undefined && value !== '') {
      args.push(param.maskInLog ? '***' : JSON.stringify(String(value)))
    }
  }
  return [...environment, props.script.kind === 'bat' ? 'cmd.exe /d /v:off /s /c' : 'bash.exe', props.script.fileName, ...args].join(' ')
})

const summary = computed(() => {
  if (!hasParams.value) return '无需参数，直接运行。'
  if (!selectedPreset.value) return '请先在脚本库配置中新增方案。'
  return props.script.params
    .filter((param) => param.type !== 'password')
    .map((param) => {
      const raw = selectedPreset.value!.values[param.key]
      if (!raw) return `${param.label}：—`
      const label = param.choices?.find((choice) => choice.value === raw)?.label ?? raw
      return `${param.label}：${label}`
    })
    .join(' · ')
})

function accept(event: RunEvent) {
  if (phase.value === 'starting' && !runId.value) {
    buffered.push(event)
    while (buffered.length > 32) buffered.shift()
    return
  }
  if (!runId.value || event.payload.runId !== runId.value || !active.value) return
  if (event.event === 'system.log') {
    historyWarning.value = event.payload.message
    return
  }
  if (event.event === 'exec.exit') {
    phase.value = event.payload.status
    exitCode.value = event.payload.exitCode
    return
  }
  if (event.event === 'exec.started') return
  let text = event.payload.line
  for (const value of masked) text = text.split(value).join('***')
  if (text.length > maxLog) text = text.slice(0, maxLog) + '…[truncated]'
  const next = [...log.value, { stream: event.event === 'exec.stderr' ? 'stderr' : 'stdout', text }]
  logSize += text.length
  while (next.length > 1 && (next.length > 2000 || logSize > maxLog)) logSize -= next.shift()!.text.length
  log.value = next
}

async function start() {
  confirmRun.value?.close()
  if (active.value || !connected.value || !ready.value || !props.script.enabled) return
  if (hasParams.value && !hasPresets.value) {
    error.value = '请先在脚本库配置中新增运行方案'
    return
  }
  error.value = ''
  historyWarning.value = ''
  runId.value = ''
  runningName.value = props.script.name
  exitCode.value = null
  log.value = []
  logSize = 0
  buffered = []
  masked = props.script.params
    .filter((param) => param.type !== 'password' && param.maskInLog)
    .map((param) => values.value[param.key])
    .filter((value) => value !== null && value !== undefined && value !== '')
    .map(String)
    .sort((a, b) => b.length - a.length)
  phase.value = 'starting'
  try {
    const reply = await execution.run(props.script.id, values.value)
    if (phase.value !== 'starting') return
    runId.value = reply.runId
    phase.value = 'running'
    for (const event of buffered) accept(event)
  } catch (failure) {
    if (phase.value === 'starting') phase.value = 'idle'
    error.value = String(failure)
  } finally {
    buffered = []
  }
}

function ask() {
  if (props.script.dangerous) confirmRun.value?.showModal()
  else void start()
}

async function cancel() {
  if (cancelling.value) return
  cancelling.value = true
  error.value = ''
  try {
    await execution.cancel(runId.value)
  } catch (failure) {
    error.value = String(failure)
  } finally {
    cancelling.value = false
  }
}

async function bash(action: 'detect' | 'save' | 'clear') {
  error.value = ''
  try {
    const result = action === 'detect'
      ? await execution.detectBash()
      : await execution.setBashPath(action === 'save' ? bashPath.value.trim() : null)
    detected.value = result.path ?? '未找到 Git Bash'
    bashPath.value = result.source === 'manual' ? result.path ?? '' : ''
  } catch (failure) {
    error.value = String(failure)
  }
}

function updateStatus(status: Awaited<ReturnType<typeof engine.status>>) {
  if (status.revision <= engineRevision) return
  engineRevision = status.revision
  ready.value = status.phase === 'ready'
  if (!ready.value && active.value) phase.value = 'interrupted'
}

onMounted(async () => {
  try {
    for (const subscribe of [() => execution.onRun(accept), () => engine.onStatus(updateStatus)]) {
      const unsubscribe = await subscribe()
      if (disposed) { unsubscribe(); return }
      stop.push(unsubscribe)
    }
    updateStatus(await engine.status())
    connected.value = true
  } catch (failure) {
    error.value = String(failure)
  }
})

onUnmounted(() => {
  disposed = true
  stop.forEach((unsubscribe) => unsubscribe())
})
</script>

<template>
  <section aria-label="运行台">
    <h2>运行台 · {{ script.name }}</h2>
    <form @submit.prevent="ask">
      <div v-if="hasParams" class="preset-row">
        <label class="preset-label">
          运行方案
          <select v-model="presetId" :disabled="active || !hasPresets">
            <option v-if="!hasPresets" value="" disabled>暂无方案，请到脚本库配置新增</option>
            <option v-for="item in presets" :key="item.id" :value="item.id">
              {{ item.name }}{{ item.id === script.defaultPresetId ? '（默认）' : '' }}
            </option>
          </select>
        </label>
        <button
          class="primary run-btn"
          type="submit"
          :disabled="active || !connected || !ready || !script.enabled || !hasPresets"
        >
          运行
        </button>
      </div>
      <div v-else class="preset-row">
        <p class="preset-empty">无参数脚本，可直接运行。</p>
        <button
          class="primary run-btn"
          type="submit"
          :disabled="active || !connected || !ready || !script.enabled"
        >
          运行
        </button>
      </div>

      <p v-if="hasParams && !hasPresets" class="hint">
        还没有运行方案。请切换到「脚本库配置」添加参数选项并新增方案。
      </p>
      <p v-if="selectedPreset" class="hint">{{ selectedPreset.description || summary }}</p>
      <p v-else-if="hasPresets" class="summary">{{ summary }}</p>
      <div>命令预览 <code>{{ preview }}</code></div>
      <div class="toolbar">
        <button type="button" :disabled="phase !== 'running' || cancelling" @click="cancel">取消执行</button>
      </div>
    </form>
    <details v-if="script.kind === 'sh'">
      <summary>Git Bash</summary>
      <form @submit.prevent="bash('save')">
        <label>手动路径 <input v-model="bashPath" required placeholder="C:\Program Files\Git\bin\bash.exe" /></label>
        <div class="toolbar">
          <button type="button" @click="bash('detect')">探测</button>
          <button type="submit">保存路径</button>
          <button type="button" @click="bash('clear')">恢复自动探测</button>
        </div>
      </form>
      <p v-if="detected">{{ detected }}</p>
    </details>
    <p role="status"><span v-if="runId && runningName !== script.name">{{ runningName }} · </span>{{ statusLabels[phase] }}<span v-if="exitCode !== null"> · 退出码 {{ exitCode }}</span></p>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <p v-if="historyWarning" role="status">{{ historyWarning }}</p>
    <pre aria-label="实时输出" class="run-log"><span v-for="(line, index) in log" :key="index" :class="line.stream">[{{ line.stream }}] {{ line.text }}{{ '\n' }}</span></pre>
    <dialog ref="confirmRun">
      <h2>确认运行危险脚本</h2>
      <p>确定运行「{{ script.name }}」？</p>
      <div class="toolbar">
        <button type="button" autofocus @click="confirmRun?.close()">返回</button>
        <button type="button" class="danger" @click="start">确认运行</button>
      </div>
    </dialog>
  </section>
</template>

<style scoped>
.preset-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 0.65rem;
  align-items: end;
  margin-bottom: 0.75rem;
}
.preset-label { display: grid; gap: 0.35rem; min-width: 0; }
.preset-label select { width: 100%; min-height: var(--app-control-h); }
.preset-empty { margin: 0; color: var(--app-muted); align-self: center; }
.run-btn {
  min-width: 6.5rem;
  min-height: 2.75rem;
  font-weight: 650;
}
.hint, .summary { margin: 0 0 0.55rem; color: var(--app-muted); font-size: var(--app-font-hint); }
.summary { color: var(--app-strong); font-weight: 500; }
.run-log {
  min-height: 7rem;
  max-height: 20rem;
  overflow: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  background: var(--app-bg);
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius-control);
  padding: .65rem;
}
.stderr { color: var(--app-danger); }
code { overflow-wrap: anywhere; }
@media (max-width: 720px) {
  .preset-row { grid-template-columns: 1fr; }
  .run-btn { width: 100%; }
}
</style>
