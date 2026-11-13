<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { history, statusLabels, type Detail, type Filter, type LogPage, type RunRecord } from '../api/history'
import { scripts } from '../api/scripts'
import HistoryFilters from './HistoryFilters.vue'

const options = ref<Array<{ id: string; name: string }>>([])
const runs = ref<RunRecord[]>([])
const detail = ref<Detail>()
const logs = ref<LogPage>()
const cursor = ref<string | null>(null)
const busy = ref(false)
const error = ref('')
let filter: Filter = {}

async function work(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try { await action() }
  catch (failure) { error.value = String(failure) }
  finally { busy.value = false }
}

async function load(append = false) {
  const page = await history.list({ ...filter, cursor: append ? cursor.value ?? undefined : undefined })
  runs.value = append ? [...runs.value, ...page.runs] : page.runs
  cursor.value = page.nextCursor
  const byId = new Map(options.value.map((item) => [item.id, item]))
  for (const run of page.runs) {
    if (!byId.has(run.scriptId)) byId.set(run.scriptId, { id: run.scriptId, name: run.scriptName })
  }
  options.value = Array.from(byId.values())
}

function search(next: Filter) {
  filter = next
  void work(async () => { await load(); detail.value = undefined; logs.value = undefined })
}

function open(id: string) {
  void work(async () => { detail.value = await history.get(id); logs.value = undefined })
}

function readLog(next: number) {
  if (!detail.value) return
  const id = detail.value.record.id
  void work(async () => {
    const result = await history.get(id, next)
    detail.value = result
    logs.value = result.log
  })
}

function date(value?: string) {
  return value ? new Date(value).toLocaleString() : '—'
}

function duration(run: RunRecord) {
  if (!run.endedAt) return '—'
  const seconds = Math.max(0, Date.parse(run.endedAt) - Date.parse(run.startedAt)) / 1000
  return `${seconds.toFixed(2)} 秒${run.status === 'interrupted' ? '（估算）' : ''}`
}

onMounted(() => work(async () => {
  await load()
  let after: string | undefined
  do {
    const page = await scripts.list(after)
    const known = new Map(options.value.map((item) => [item.id, item]))
    for (const item of page.scripts) known.set(item.id, item)
    options.value = Array.from(known.values())
    after = page.nextAfterId ?? undefined
  } while (after)
}))
</script>

<template>
  <section aria-label="执行历史">
    <HistoryFilters :busy="busy" :scripts="options" @search="search" />
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="!runs.length && !busy">暂无执行记录。</p>
    <table v-if="runs.length">
      <thead><tr><th>脚本</th><th>开始时间</th><th>状态</th><th>退出码</th></tr></thead>
      <tbody>
        <tr v-for="run in runs" :key="run.id">
          <td><button :disabled="busy" @click="open(run.id)">{{ run.scriptName }}</button></td>
          <td>{{ date(run.startedAt) }}</td><td>{{ statusLabels[run.status] }}</td><td>{{ run.exitCode ?? '—' }}</td>
        </tr>
      </tbody>
    </table>
    <button v-if="cursor" :disabled="busy" @click="work(() => load(true))">加载更多记录</button>
    <section v-if="detail" class="history-detail" aria-label="历史详情">
      <h3>{{ detail.record.scriptName }}</h3>
      <p v-if="detail.scriptDeleted">脚本已删除</p>
      <dl>
        <dt>状态</dt><dd>{{ statusLabels[detail.record.status] }}</dd>
        <dt>触发方式</dt><dd>{{ detail.record.trigger }}</dd>
        <template v-if="detail.record.reasonCode"><dt>原因</dt><dd>{{ detail.record.reasonCode }}</dd></template>
        <dt>开始 / 结束</dt><dd>{{ date(detail.record.startedAt) }} / {{ date(detail.record.endedAt) }}</dd>
        <dt>退出码</dt><dd>{{ detail.record.exitCode ?? '—' }}</dd>
        <dt>耗时</dt><dd>{{ duration(detail.record) }}</dd>
      </dl>
      <h4>参数快照</h4>
      <pre>{{ JSON.stringify(detail.record.argsMasked, null, 2) }}</pre>
      <p>包含密码参数的运行会隐藏脚本输出。其他运行按原文保存，请勿在脚本中输出密码或密钥。</p>
      <button v-if="detail.record.logRef" :disabled="busy" @click="readLog(0)">查看日志</button>
      <div v-if="logs">
        <p v-if="!logs.entries.length">此页没有该次运行的日志条目。</p>
        <pre v-for="(entry, index) in logs.entries" :key="index">[{{ entry.kind }}] {{ typeof entry.data === 'object' && entry.data !== null && 'text' in entry.data ? entry.data.text : JSON.stringify(entry.data) }}</pre>
        <button v-if="logs.nextCursor !== null" :disabled="busy" @click="readLog(logs.nextCursor)">下一页日志</button>
      </div>
    </section>
  </section>
</template>

<style scoped>
table { width: 100%; border-collapse: collapse; margin: 1rem 0; }
th, td { text-align: left; padding: .4rem; border-bottom: 1px solid var(--app-border); }
td button, pre { overflow-wrap: anywhere; }
pre { white-space: pre-wrap; background: var(--app-bg); padding: .45rem; border-radius: var(--app-radius-control); }
.history-detail {
  margin-top: 1rem;
  padding-top: 1rem;
  border-top: 1px solid var(--app-border);
  background: transparent;
  border-left: none;
  border-right: none;
  border-bottom: none;
  border-radius: 0;
  box-shadow: none;
  padding-left: 0;
  padding-right: 0;
}
</style>
