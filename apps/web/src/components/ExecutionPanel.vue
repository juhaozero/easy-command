<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { execution, type FinalStatus, type RunArgs, type RunEvent } from '../api/execution'
import { engine } from '../engine'
import type { ScriptMeta } from '../api/scripts'
import RunParameters from './RunParameters.vue'

const props = defineProps<{ script: ScriptMeta }>()
const values = ref<RunArgs>(Object.create(null))
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
let buffered: RunEvent[] = []
let masked: string[] = []
let disposed = false
const stop: Array<() => void> = []
const maxLog = 2 * 1024 * 1024
let logSize = 0
let engineRevision = -1

watch(() => props.script, () => {
  const next: RunArgs = Object.create(null)
  for (const param of props.script.params) {
    if (param.type !== 'password') next[param.key] = param.default ?? null
  }
  values.value = next
}, { immediate: true })

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
  <section aria-label="执行台">
    <h2>执行台 · {{ script.name }}</h2>
    <form @submit.prevent="ask">
      <RunParameters v-model="values" :params="script.params" :disabled="active" />
      <div>命令预览 <code>{{ preview }}</code></div>
      <div class="toolbar">
        <button class="primary" type="submit" :disabled="active || !connected || !ready || !script.enabled">运行</button>
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
</style>
