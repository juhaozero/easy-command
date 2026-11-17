<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, toRaw } from 'vue'
import { listen } from '@tauri-apps/api/event'
import { automation } from './api/automation'
import { desktop } from './api/desktop'
import { pickScriptFiles, pickScriptFolder } from './api/dialog'
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
import PresetEditor from './components/PresetEditor.vue'

type PageId = 'library' | 'history' | 'settings' | 'schedule'
type LibraryPane = 'run' | 'config'

const NAV_ITEMS: ReadonlyArray<{ id: PageId; label: string; icon: 'library' | 'history' | 'schedule' | 'settings' }> = [
  { id: 'library', label: '脚本库', icon: 'library' },
  { id: 'history', label: '执行历史', icon: 'history' },
  { id: 'schedule', label: '调度', icon: 'schedule' },
  { id: 'settings', label: '设置', icon: 'settings' },
]

const PAGE_META: Record<PageId, { title: string; kicker: string; subtle: string }> = {
  library: { title: '脚本库', kicker: 'LIBRARY', subtle: '运行台选方案 · 脚本库配置管参数与方案' },
  history: { title: '执行历史', kicker: 'HISTORY', subtle: '查看运行记录与日志' },
  schedule: { title: '调度', kicker: 'SCHEDULE', subtle: '定时与间隔规则仅在应用运行时生效' },
  settings: { title: '设置', kicker: 'PREFERENCES', subtle: '日志保留、快捷键与安全说明' },
}

const page = ref<PageId>('library')
const libraryPane = ref<LibraryPane>('run')
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
const importMode = ref<'file' | 'folder'>('file')
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
  const {
    name, description, enabled, dangerous, workingDir, params, tags, presets, defaultPresetId,
  } = toRaw(meta)
  return structuredClone({
    name,
    description,
    enabled,
    dangerous,
    workingDir,
    params: params ?? [],
    tags,
    presets: presets ?? [],
    defaultPresetId: defaultPresetId ?? '',
  })
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
  libraryPane.value = 'run'
}

async function browseImportSource() {
  error.value = ''
  if (importMode.value === 'folder') {
    const folder = await pickScriptFolder()
    if (folder) sourcePath.value = folder
    return
  }
  const files = await pickScriptFiles()
  if (files.length === 1) sourcePath.value = files[0]
  else if (files.length > 1) sourcePath.value = files.join(';')
}

async function importFile() {
  const policyValue = policy.value
  if (importMode.value === 'folder') {
    const dir = sourcePath.value.trim()
    if (!dir) throw new Error('请选择脚本文件夹')
    const result = await scripts.importDir(dir, policyValue)
    if (!result.imported.length) {
      const details = [...result.failed, ...result.skipped]
        .map((item) => `${item.path}: ${item.message}`)
        .join('\n')
      throw new Error(details || '未导入任何脚本')
    }
    importDialog.value?.close()
    await load()
    await open(result.imported.at(-1)!.id)
    const parts = [`已导入 ${result.imported.length} 个脚本`]
    if (result.skipped.length) parts.push(`跳过 ${result.skipped.length}`)
    if (result.failed.length) parts.push(`失败 ${result.failed.length}`)
    notice.value = parts.join('，')
    return
  }

  const paths = sourcePath.value
    .split(';')
    .map((item) => item.trim())
    .filter(Boolean)
  if (!paths.length) throw new Error('请选择脚本文件')
  let lastId = ''
  let imported = 0
  const failures: string[] = []
  for (const path of paths) {
    try {
      const { meta } = await scripts.import(path, policyValue)
      lastId = meta.id
      imported++
    } catch (failure) {
      failures.push(`${path}: ${String(failure)}`)
    }
  }
  if (!imported) throw new Error(failures.join('\n') || '未导入任何脚本')
  importDialog.value?.close()
  await load()
  if (lastId) await open(lastId)
  notice.value = paths.length === 1 && imported === 1
    ? '已导入脚本'
    : `已导入 ${imported}/${paths.length} 个脚本`
  if (failures.length) error.value = failures.join('\n')
}

function openImportDialog() {
  importMode.value = 'file'
  sourcePath.value = ''
  policy.value = 'cancel'
  error.value = ''
  importDialog.value?.showModal()
}

async function saveMeta() {
  if (!current.value || !draft.value) return
  const id = current.value.id
  const update = {
    ...draft.value,
    tags: tags.value.split(',').map((tag) => tag.trim()).filter(Boolean),
    presets: draft.value.presets ?? [],
    defaultPresetId: draft.value.defaultPresetId || undefined,
  }
  await scripts.updateMeta(id, update)
  await load()
  const unsaved = text.value !== originalText.value ? text.value : undefined
  await open(id, false)
  if (unsaved !== undefined) text.value = unsaved
  notice.value = '已保存脚本库配置'
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
          <button type="button" class="primary" :disabled="busy" @click="openImportDialog">
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
              <template v-if="current && draft">
                <div class="library-pane-switch">
                  <button type="button" :class="{ primary: libraryPane === 'run' }" @click="libraryPane = 'run'">运行台</button>
                  <button type="button" :class="{ primary: libraryPane === 'config' }" @click="libraryPane = 'config'">脚本库配置</button>
                </div>

                <ExecutionPanel v-if="libraryPane === 'run'" :script="current" />

                <section v-else class="panel" aria-label="脚本库配置">
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
                    <PresetEditor
                      v-model="draft.presets!"
                      v-model:default-preset-id="draft.defaultPresetId!"
                      :params="draft.params"
                      :disabled="busy"
                    />
                    <button class="primary" :disabled="busy" type="submit">保存配置</button>
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
              </template>
              <section v-else class="panel" aria-label="脚本详情">
                <p>选择脚本查看或编辑。</p>
              </section>
            </div>
          </div>

          <dialog ref="importDialog" @cancel.prevent="!busy && importDialog?.close()">
            <form @submit.prevent="work(importFile)">
              <h2>导入脚本</h2>
              <fieldset class="import-mode">
                <legend>导入方式</legend>
                <label class="check">
                  <input v-model="importMode" type="radio" value="file" :disabled="busy" @change="sourcePath = ''" /> 选择文件
                </label>
                <label class="check">
                  <input v-model="importMode" type="radio" value="folder" :disabled="busy" @change="sourcePath = ''" /> 绑定文件夹
                </label>
              </fieldset>
              <label>
                {{ importMode === 'folder' ? '文件夹绝对路径' : '源文件绝对路径' }}
                <div class="path-row">
                  <input
                    v-model="sourcePath"
                    required
                    :placeholder="importMode === 'folder' ? 'C:\\scripts' : 'C:\\scripts\\example.bat'"
                    :disabled="busy"
                  />
                  <button type="button" :disabled="busy" @click="work(browseImportSource)">浏览…</button>
                </div>
              </label>
              <p v-if="importMode === 'folder'" class="hint">
                将导入该文件夹下全部 `.bat` / `.sh` 文件（不含子文件夹）。
              </p>
              <label>遇到同名脚本
                <select v-model="policy" :disabled="busy">
                  <option value="cancel">取消导入</option>
                  <option value="rename">重命名新脚本</option>
                  <option value="overwrite">覆盖脚本及元数据</option>
                </select>
              </label>
              <p v-if="error" class="error" role="alert">{{ error }}</p>
              <div class="toolbar">
                <button type="button" :disabled="busy" @click="importDialog?.close(); error = ''">关闭</button>
                <button class="primary" type="submit" :disabled="busy">
                  {{ importMode === 'folder' ? '导入文件夹' : '导入' }}
                </button>
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
.library-pane-switch {
  display: flex;
  gap: 0.45rem;
  flex-wrap: wrap;
  margin-bottom: 0.75rem;
}
</style>
