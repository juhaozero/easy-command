<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, toRaw } from 'vue'
import { listen } from '@tauri-apps/api/event'
import { automation } from './api/automation'
import { desktop } from './api/desktop'
import { scripts, type ConflictPolicy, type ScriptFields, type ScriptMeta, type ScriptSummary } from './api/scripts'
import { engine, type EngineStatus } from './engine'
import ParamEditor from './components/ParamEditor.vue'
import ExecutionPanel from './components/ExecutionPanel.vue'
import HistoryPage from './components/HistoryPage.vue'
import RetentionSettings from './components/RetentionSettings.vue'
import PasswordEditor from './components/PasswordEditor.vue'
import DpapiNotice from './components/DpapiNotice.vue'
import SchedulePage from './components/SchedulePage.vue'
import HotkeySettings from './components/HotkeySettings.vue'
import NavIcon from './components/NavIcon.vue'
import OnboardingPage from './components/OnboardingPage.vue'
import DesktopSettings from './components/DesktopSettings.vue'

type PageId = 'library' | 'history' | 'settings' | 'schedule'

const NAV_ITEMS: ReadonlyArray<{ id: PageId; label: string; icon: 'library' | 'history' | 'schedule' | 'settings' }> = [
  { id: 'library', label: '脚本库', icon: 'library' },
  { id: 'history', label: '执行历史', icon: 'history' },
  { id: 'schedule', label: '调度', icon: 'schedule' },
  { id: 'settings', label: '设置', icon: 'settings' },
]

const PAGE_META: Record<PageId, { title: string; kicker: string; subtle: string }> = {
  library: { title: '脚本库', kicker: 'LIBRARY', subtle: '导入、编辑并运行本地脚本' },
  history: { title: '执行历史', kicker: 'HISTORY', subtle: '查看运行记录与日志' },
  schedule: { title: '调度', kicker: 'SCHEDULE', subtle: '定时与间隔规则仅在应用运行时生效' },
  settings: { title: '设置', kicker: 'PREFERENCES', subtle: '日志保留、快捷键与安全说明' },
}

const page = ref<PageId>('library')
const pageMeta = computed(() => PAGE_META[page.value])

const rows = ref<ScriptSummary[]>([])
const next = ref<string | null>(null)
const current = ref<ScriptMeta>()
const draft = ref<ScriptFields>()
const tags = ref('')
const revision = ref('')
const text = ref<string | null>(null)
const originalText = ref<string | null>(null)
const importDialog = ref<HTMLDialogElement>()
const deleteDialog = ref<HTMLDialogElement>()
const sourcePath = ref('')
const policy = ref<ConflictPolicy>('cancel')
const busy = ref(false)
const error = ref('')
const notice = ref('')
const automationNotice = ref('')
let stopNotice: (() => void) | undefined
let stopEngine: (() => void) | undefined
let disposed = false
const engineStatus = ref<EngineStatus>()
const onboardingCompleted = ref<boolean>()
const startupError = ref('')
let readingSettings = false

async function work(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    await action()
  } catch (failure) {
    error.value = String(failure)
  } finally {
    busy.value = false
  }
}

async function load(append = false) {
  const result = await scripts.list(append ? next.value ?? undefined : undefined)
  rows.value = append ? [...rows.value, ...result.scripts] : result.scripts
  next.value = result.nextAfterId
}

function fields(meta: ScriptMeta): ScriptFields {
  const { name, description, enabled, dangerous, workingDir, params, tags } = toRaw(meta)
  return structuredClone({ name, description, enabled, dangerous, workingDir, params, tags })
}

async function open(id: string, reportSelection = true) {
  const detail = await scripts.get(id)
  if (reportSelection) await automation.select(id)
  current.value = detail.meta
  draft.value = fields(detail.meta)
  tags.value = (detail.meta.tags ?? []).join(', ')
  revision.value = detail.revision
  text.value = detail.text
  originalText.value = detail.text
}

async function importFile() {
  const { meta } = await scripts.import(sourcePath.value.trim(), policy.value)
  importDialog.value?.close()
  await load()
  await open(meta.id)
  notice.value = '已导入脚本'
}

async function saveMeta() {
  if (!current.value || !draft.value) return
  const id = current.value.id
  const update = { ...draft.value, tags: tags.value.split(',').map((tag) => tag.trim()).filter(Boolean) }
  await scripts.updateMeta(id, update)
  await load()
  const unsaved = text.value !== originalText.value ? text.value : undefined
  await open(id, false)
  if (unsaved !== undefined) text.value = unsaved
  notice.value = '已保存元数据'
}

async function saveContent() {
  if (!current.value || text.value === null) return
  const id = current.value.id
  const original = originalText.value ?? ''
  let body = text.value
  // Textarea normalizes newline sequences on input.
  if (original.includes('\r\n') && !original.replaceAll('\r\n', '').includes('\n')) {
    body = body.replace(/\r?\n/g, '\r\n')
  }
  await scripts.updateContent(id, body, revision.value)
  const detail = await scripts.get(id)
  current.value = detail.meta
  revision.value = detail.revision
  text.value = originalText.value = detail.text
  await load()
  notice.value = '已保存正文'
}

async function remove() {
  if (!current.value) return
  await scripts.delete(current.value.id)
  deleteDialog.value?.close()
  current.value = draft.value = undefined
  await load()
  notice.value = '已删除脚本'
}

function passwordChanged(id: string, key: string, updatedAt?: string) {
  if (current.value?.id !== id) return
  const changed = current.value.params.find((param) => param.key === key)
  // Existing definitions may explicitly share a secret binding.
  const binding = changed?.secretBinding || key
  for (const param of current.value.params) {
    if (param.type === 'password' && (param.secretBinding || param.key) === binding) {
      param.passwordStored = updatedAt !== undefined
      param.updatedAt = updatedAt
    }
  }
}

async function finishOnboarding(importedId?: string) {
  onboardingCompleted.value = true
  await work(async () => {
    await load()
    if (importedId) {
      await open(importedId)
      return
    }
    const { lastSelectedScriptId } = await automation.state()
    if (lastSelectedScriptId) await open(lastSelectedScriptId, false)
  })
}

async function readOnboarding() {
  if (readingSettings || onboardingCompleted.value !== undefined || engineStatus.value?.phase !== 'ready') return
  readingSettings = true
  startupError.value = ''
  try {
    const settings = await desktop.settings()
    if (disposed) return
    if (settings.onboardingCompleted) await finishOnboarding()
    else onboardingCompleted.value = false
  } catch (failure) {
    startupError.value = String(failure)
  } finally {
    readingSettings = false
  }
}

function onEngine(status: EngineStatus) {
  if (disposed || (engineStatus.value && status.revision <= engineStatus.value.revision)) return
  engineStatus.value = status
  if (status.phase === 'ready') void readOnboarding()
}

onMounted(async () => {
  void listen<string>('automation://notice', ({ payload }) => { automationNotice.value = payload })
    .then((stop) => { if (disposed) stop(); else stopNotice = stop })
    .catch(() => {})
  try {
    const stop = await engine.onStatus(onEngine)
    if (disposed) { stop(); return }
    stopEngine = stop
    onEngine(await engine.status())
  } catch (failure) {
    startupError.value = String(failure)
  }
})
onUnmounted(() => {
  disposed = true
  stopNotice?.()
  stopEngine?.()
})
</script>

<template>
  <OnboardingPage v-if="onboardingCompleted === false" :engine-status="engineStatus" @completed="finishOnboarding" />
  <main v-else-if="onboardingCompleted === undefined" class="onboarding-loading">
    <section class="panel">
      <h1>Easy Command</h1>
      <p role="status">{{ engineStatus?.phase === 'ready' ? '正在读取设置' : engineStatus?.message ?? '正在启动执行引擎' }}</p>
      <p v-if="startupError" class="error" role="alert">{{ startupError }}</p>
      <button v-if="startupError && engineStatus?.phase === 'ready'" type="button" @click="readOnboarding">重试</button>
    </section>
  </main>
  <div v-else class="app-shell">
    <aside class="app-sidebar" aria-label="主导航">
      <div class="app-brand">
        <div class="app-brand-icon">
          <NavIcon name="app" />
        </div>
        <div class="app-brand-copy">
          <strong>Easy Command</strong>
          <span>本地脚本，一键就位。</span>
        </div>
      </div>
      <nav class="app-nav">
        <button
          v-for="item in NAV_ITEMS"
          :key="item.id"
          type="button"
          class="app-nav-item"
          :class="{ 'is-active': page === item.id }"
          :aria-current="page === item.id ? 'page' : undefined"
          @click="page = item.id"
        >
          <NavIcon :name="item.icon" />
          <span>{{ item.label }}</span>
        </button>
      </nav>
      <div class="app-sidebar-note">
        <span>本地脚本工作台</span>
        <small>Easy Command</small>
      </div>
    </aside>

    <div class="app-main">
      <header class="app-main-header">
        <div>
          <span class="app-page-kicker">EASY COMMAND / {{ pageMeta.kicker }}</span>
          <h1>{{ pageMeta.title }}</h1>
          <p class="app-subtle">{{ pageMeta.subtle }}</p>
        </div>
        <div v-if="page === 'library'" class="app-main-actions">
          <button type="button" :disabled="busy" @click="work(load)">刷新</button>
          <button
            type="button"
            class="primary"
            :disabled="busy"
            @click="sourcePath = ''; policy = 'cancel'; importDialog?.showModal()"
          >
            导入脚本
          </button>
        </div>
      </header>

      <div class="app-content">
        <p v-if="automationNotice" class="app-toast" role="status">{{ automationNotice }}</p>

        <div v-show="page === 'library'">
          <p
            v-if="error && !importDialog?.open && !deleteDialog?.open"
            class="app-toast is-error"
            role="alert"
          >
            {{ error }}
          </p>
          <p v-if="notice" class="app-toast is-ok" role="status">{{ notice }}</p>

          <div class="library-layout">
            <aside class="script-list" aria-label="脚本列表">
              <p v-if="!rows.length">库中暂无脚本。</p>
              <button
                v-for="row in rows"
                :key="row.id"
                type="button"
                class="item"
                :aria-pressed="current?.id === row.id"
                :disabled="busy"
                @click="work(() => open(row.id, row.enabled))"
              >
                <strong>{{ row.name }}</strong>
                <small>{{ row.enabled ? '已启用' : '已禁用' }}{{ row.dangerous ? ' · 危险脚本' : '' }}</small>
              </button>
              <button v-if="next" type="button" :disabled="busy" @click="work(() => load(true))">加载更多</button>
            </aside>

            <div class="library-detail">
              <section v-if="current && draft" class="panel" aria-label="脚本详情">
                <header class="toolbar">
                  <h2>{{ current.name }}</h2>
                  <button type="button" class="danger" :disabled="busy" @click="deleteDialog?.showModal()">删除脚本</button>
                </header>
                <form @submit.prevent="work(saveMeta)">
                  <div class="fields">
                    <label>名称 <input v-model="draft.name" required /></label>
                    <label>工作目录 <input v-model="draft.workingDir" /></label>
                    <label>说明 <input v-model="draft.description" /></label>
                    <label>标签（逗号分隔） <input v-model="tags" /></label>
                  </div>
                  <label class="check"><input v-model="draft.enabled" type="checkbox" /> 启用</label>
                  <label class="check"><input v-model="draft.dangerous" type="checkbox" /> 标记为危险脚本</label>
                  <ParamEditor v-model="draft.params" />
                  <button class="primary" :disabled="busy" type="submit">保存元数据</button>
                </form>
                <fieldset v-if="current.params.some((param) => param.type === 'password')">
                  <legend>密码参数</legend>
                  <PasswordEditor
                    v-for="param in current.params.filter((param) => param.type === 'password')"
                    :key="current.id + ':' + param.key"
                    :script-id="current.id"
                    :param="param"
                    :disabled="busy"
                    @changed="passwordChanged"
                  />
                </fieldset>
                <form @submit.prevent="work(saveContent)">
                  <label v-if="text !== null">脚本正文
                    <textarea v-model="text" rows="10" spellcheck="false"></textarea>
                  </label>
                  <p v-else>正文使用非 UTF-8 编码，本步保留原始文件。</p>
                  <button v-if="text !== null" :disabled="busy || text === originalText" type="submit">保存正文</button>
                </form>
              </section>
              <section v-else class="panel" aria-label="脚本详情">
                <p>选择脚本查看或编辑。</p>
              </section>
              <ExecutionPanel v-if="current" :script="current" />
            </div>
          </div>

          <dialog ref="importDialog" @cancel.prevent="!busy && importDialog?.close()">
            <form @submit.prevent="work(importFile)">
              <h2>导入脚本</h2>
              <label>源文件绝对路径 <input v-model="sourcePath" required placeholder="C:\scripts\example.bat" /></label>
              <label>遇到同名脚本
                <select v-model="policy">
                  <option value="cancel">取消导入</option>
                  <option value="rename">重命名新脚本</option>
                  <option value="overwrite">覆盖脚本及元数据</option>
                </select>
              </label>
              <p v-if="error" class="error" role="alert">{{ error }}</p>
              <div class="toolbar">
                <button type="button" :disabled="busy" @click="importDialog?.close(); error = ''">关闭</button>
                <button class="primary" type="submit" :disabled="busy">导入</button>
              </div>
            </form>
          </dialog>

          <dialog ref="deleteDialog" @cancel.prevent="!busy && deleteDialog?.close()">
            <form @submit.prevent="work(remove)">
              <h2>删除脚本</h2>
              <p>删除库内的「{{ current?.name }}」及其元数据？</p>
              <p v-if="error" class="error" role="alert">{{ error }}</p>
              <div class="toolbar">
                <button type="button" :disabled="busy" @click="deleteDialog?.close(); error = ''">取消</button>
                <button class="danger" type="submit" :disabled="busy">确认删除</button>
              </div>
            </form>
          </dialog>
        </div>

        <HistoryPage v-if="page === 'history'" />
        <SchedulePage v-if="page === 'schedule'" />
        <div v-if="page === 'settings'" class="settings-stack">
          <DesktopSettings />
          <RetentionSettings />
          <HotkeySettings />
          <section><DpapiNotice /></section>
        </div>
      </div>
    </div>

  </div>
</template>

<style scoped>
.onboarding-loading { max-width: 46rem; margin: 2rem auto; padding: 0 1rem; }
</style>
