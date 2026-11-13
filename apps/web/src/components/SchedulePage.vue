<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { automation, type Rule, type Timing } from '../api/automation'
import { scripts, type ScriptSummary } from '../api/scripts'

const available = ref<ScriptSummary[]>([])
const rules = ref<Rule[]>([])
const paused = ref(false)
const busy = ref(false)
const error = ref('')
const draft = ref<Rule>()
const overlay = ref('')

async function work(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try { await action() }
  catch (failure) { error.value = String(failure) }
  finally { busy.value = false }
}

async function reload() {
  const result = await automation.list()
  rules.value = result.rules
  paused.value = result.paused
}

function timing(type: Timing['type']): Timing {
  switch (type) {
    case 'cron': return { type, expr: '0 9 * * *' }
    case 'daily': return { type, at: '09:00' }
    case 'interval': return { type, everySeconds: 60 }
  }
}

function edit(rule?: Rule) {
  draft.value = rule ? structuredClone({ ...rule, rule: { ...rule.rule }, argsOverlay: undefined })
    : { id: '', enabled: true, scriptId: available.value[0]?.id ?? '', rule: timing('interval') }
  overlay.value = rule?.argsOverlay ? JSON.stringify(rule.argsOverlay, null, 2) : ''
}

function changeType(event: Event) {
  if (draft.value) draft.value.rule = timing((event.target as HTMLSelectElement).value as Timing['type'])
}

function save() {
  void work(async () => {
    if (!draft.value) return
    const args = overlay.value.trim() ? JSON.parse(overlay.value) : undefined
    if (args !== undefined && (!args || Array.isArray(args) || typeof args !== 'object')) {
      throw new Error('参数覆盖须为 JSON 对象')
    }
    await automation.save({ ...draft.value, argsOverlay: args })
    draft.value = undefined
    await reload()
  })
}

function label(rule: Rule) {
  switch (rule.rule.type) {
    case 'cron': return rule.rule.expr
    case 'interval': return `每 ${rule.rule.everySeconds} 秒`
    case 'daily': return `每天 ${rule.rule.at}`
  }
}

onMounted(() => work(async () => {
  await reload()
  let cursor: string | undefined
  do {
    const page = await scripts.list(cursor)
    available.value.push(...page.scripts)
    cursor = page.nextAfterId ?? undefined
  } while (cursor)
}))
</script>

<template>
  <section aria-label="调度管理">
    <header class="toolbar">
      <p role="status">{{ paused ? '调度已暂停' : '调度仅在应用运行期间生效' }}</p>
      <button :disabled="busy" @click="work(async () => { await automation.pause(!paused); await reload() })">
        {{ paused ? '恢复全部调度' : '暂停全部调度' }}
      </button>
    </header>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <ul v-if="rules.length">
      <li v-for="rule in rules" :key="rule.id">
        {{ available.find((item) => item.id === rule.scriptId)?.name ?? '脚本已删除' }}
        · {{ label(rule) }} · {{ rule.enabled ? '启用' : '禁用' }}
        <button :disabled="busy" @click="edit(rule)">编辑</button>
        <button :disabled="busy" @click="work(async () => { await automation.remove(rule.id); draft = undefined; await reload() })">删除规则</button>
      </li>
    </ul>
    <p v-else>暂无调度规则。</p>
    <button :disabled="busy || !available.length" @click="edit()">新建规则</button>
    <form v-if="draft" @submit.prevent="save">
      <label>脚本
        <select v-model="draft.scriptId" required>
          <option v-for="script in available" :key="script.id" :value="script.id">{{ script.name }}</option>
        </select>
      </label>
      <label>规则类型
        <select :value="draft.rule.type" @change="changeType">
          <option value="interval">固定间隔</option><option value="daily">每日时刻</option><option value="cron">cron</option>
        </select>
      </label>
      <label v-if="draft.rule.type === 'interval'">间隔（秒）<input v-model.number="draft.rule.everySeconds" type="number" min="10" max="86400" required /></label>
      <label v-else-if="draft.rule.type === 'daily'">本地时间<input v-model="draft.rule.at" type="time" required /></label>
      <label v-else>五段 cron 表达式<input v-model="draft.rule.expr" required /></label>
      <label>非密码参数覆盖（JSON 对象，可留空）<textarea v-model="overlay" rows="3" /></label>
      <label class="check"><input v-model="draft.enabled" type="checkbox" /> 启用此规则</label>
      <div class="toolbar"><button type="button" @click="draft = undefined">取消</button><button class="primary" :disabled="busy">保存规则</button></div>
    </form>
  </section>
</template>
