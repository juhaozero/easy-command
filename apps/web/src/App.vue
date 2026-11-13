<script setup lang="ts">
import { onMounted, ref, toRaw } from 'vue'
import { scripts, type ConflictPolicy, type ScriptFields, type ScriptMeta, type ScriptSummary } from './api/scripts'
import ParamEditor from './components/ParamEditor.vue'

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
  const page = await scripts.list(append ? next.value ?? undefined : undefined)
  rows.value = append ? [...rows.value, ...page.scripts] : page.scripts
  next.value = page.nextAfterId
}

function fields(meta: ScriptMeta): ScriptFields {
  const { name, description, enabled, dangerous, workingDir, params, tags } = toRaw(meta)
  return structuredClone({ name, description, enabled, dangerous, workingDir, params, tags })
}

async function open(id: string) {
  const detail = await scripts.get(id)
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
  await open(id)
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

onMounted(() => work(load))
</script>

<template>
  <main>
    <header class="toolbar">
      <h1>脚本库</h1>
      <div>
        <button :disabled="busy" @click="work(load)">刷新</button>
        <button class="primary" :disabled="busy" @click="sourcePath = ''; policy = 'cancel'; importDialog?.showModal()">导入脚本</button>
      </div>
    </header>
    <p v-if="error && !importDialog?.open && !deleteDialog?.open" class="error" role="alert">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <div class="layout">
      <aside aria-label="脚本列表">
        <p v-if="!rows.length">库中暂无脚本。</p>
        <button
          v-for="row in rows" :key="row.id" type="button" class="item"
          :aria-pressed="current?.id === row.id" :disabled="busy"
          @click="work(() => open(row.id))"
        >
          <strong>{{ row.name }}</strong>
          <small>{{ row.enabled ? '已启用' : '已禁用' }}{{ row.dangerous ? ' · 危险脚本' : '' }}</small>
        </button>
        <button v-if="next" :disabled="busy" @click="work(() => load(true))">加载更多</button>
      </aside>
      <section v-if="current && draft" aria-label="脚本详情">
        <header class="toolbar">
          <h2>{{ current.name }}</h2>
          <button class="danger" :disabled="busy" @click="deleteDialog?.showModal()">删除脚本</button>
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
        <form @submit.prevent="work(saveContent)">
          <label v-if="text !== null">脚本正文
            <textarea v-model="text" rows="10" spellcheck="false"></textarea>
          </label>
          <p v-else>正文使用非 UTF-8 编码，本步保留原始文件。</p>
          <button v-if="text !== null" :disabled="busy || text === originalText" type="submit">保存正文</button>
        </form>
      </section>
      <section v-else aria-label="脚本详情"><p>选择脚本查看或编辑。</p></section>
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
  </main>
</template>

<style>
:root {
  font-family: 'Microsoft YaHei UI', system-ui, sans-serif;
  color: #152c29;
  background: #f2f5f4;
}
body { margin: 0; }
main { max-width: 70rem; margin: auto; padding: 1.25rem; }
h1, h2 { margin: 0; }
h1 { font-size: 1.55rem; }
h2 { font-size: 1.15rem; overflow-wrap: anywhere; }
.toolbar, .fields { display: flex; align-items: center; gap: .65rem; }
.toolbar { justify-content: space-between; }
.toolbar > div { display: flex; gap: .5rem; }
.layout { display: grid; grid-template-columns: minmax(9rem, 1fr) minmax(0, 3fr); gap: 1rem; margin-top: 1rem; }
aside, section, dialog { background: white; border: 1px solid #cedbd7; border-radius: .5rem; padding: 1rem; }
.item { display: grid; gap: .25rem; width: 100%; margin-bottom: .45rem; text-align: left; overflow-wrap: anywhere; }
.item[aria-pressed="true"] { background: #d7ebe5; border-color: #286e63; }
.fields { flex-wrap: wrap; align-items: stretch; }
.fields > label { flex: 1 1 10rem; min-width: 0; }
.fields input, .fields select { min-width: 0; box-sizing: border-box; width: 100%; }
form { display: grid; gap: .65rem; margin-top: 1rem; }
label { display: grid; gap: .3rem; }
.check { display: flex; align-items: center; gap: .35rem; margin: .25rem 0; }
button, input, select, textarea { font: inherit; color: inherit; border: 1px solid #9cb6ae; border-radius: .3rem; padding: .4rem .6rem; }
button { background: #f2f5f4; cursor: pointer; }
button:disabled { opacity: .55; cursor: default; }
.primary { background: #195b51; color: white; }
.danger, .error { color: #952b2b; }
.error { overflow-wrap: anywhere; }
textarea { font-family: Consolas, monospace; resize: vertical; }
fieldset { min-width: 0; border: 1px solid #cedbd7; }
fieldset fieldset { margin-bottom: .6rem; }
dialog { width: min(28rem, calc(100% - 4rem)); }
dialog::backdrop { background: #132b2780; }
@media (max-width: 570px) { .layout { grid-template-columns: 1fr; } }
</style>
