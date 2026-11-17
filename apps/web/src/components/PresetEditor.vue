<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ParamDef, ScriptPreset } from '../api/scripts'

const props = defineProps<{ params: ParamDef[]; disabled?: boolean }>()
const presets = defineModel<ScriptPreset[]>({ required: true })
const defaultPresetId = defineModel<string>('defaultPresetId', { default: '' })

const editingId = ref('')
const draftName = ref('')
const draftDescription = ref('')
const draftValues = ref<Record<string, string>>({})
const notice = ref('')

const selectableParams = computed(() =>
  props.params.filter((param) => param.type !== 'password'),
)

const paramsReady = computed(() =>
  selectableParams.value.length > 0
  && selectableParams.value.every((param) => (param.choices?.length ?? 0) > 0
    && param.choices!.every((choice) => choice.label.trim() && choice.value.trim())),
)

const editing = computed(() => presets.value.find((item) => item.id === editingId.value))

function emptyValues() {
  const next: Record<string, string> = {}
  for (const param of selectableParams.value) next[param.key] = ''
  return next
}

function loadDraft(id?: string) {
  const target = presets.value.find((item) => item.id === (id || editingId.value))
  editingId.value = target?.id ?? ''
  draftName.value = target?.name ?? ''
  draftDescription.value = target?.description ?? ''
  const next = emptyValues()
  if (target) Object.assign(next, target.values)
  draftValues.value = next
  notice.value = ''
}

function startNew() {
  editingId.value = ''
  draftName.value = ''
  draftDescription.value = ''
  draftValues.value = emptyValues()
  notice.value = '正在新建方案：只能从参数选项中选择。'
}

function validate(): string | null {
  if (!draftName.value.trim()) return '请填写方案名称'
  if (!paramsReady.value) return '请先为每个非密码参数配置选项'
  for (const param of selectableParams.value) {
    const value = draftValues.value[param.key] ?? ''
    if (param.required && !value) return `请选择必填参数「${param.label}」`
    if (value && !(param.choices ?? []).some((choice) => choice.value === value)) {
      return `「${param.label}」必须选自已有选项`
    }
  }
  return null
}

function snapshotValues() {
  const next: Record<string, string> = {}
  for (const param of selectableParams.value) {
    const value = (draftValues.value[param.key] ?? '').trim()
    if (value) next[param.key] = value
  }
  return next
}

function saveNew() {
  const error = validate()
  if (error) {
    notice.value = error
    return
  }
  const created: ScriptPreset = {
    id: `preset-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`,
    name: draftName.value.trim(),
    description: draftDescription.value.trim() || undefined,
    values: snapshotValues(),
  }
  presets.value = [...presets.value, created]
  if (!defaultPresetId.value) defaultPresetId.value = created.id
  editingId.value = created.id
  notice.value = `已新增方案「${created.name}」`
}

function updateCurrent() {
  if (!editing.value) {
    notice.value = '当前是新建草稿，请用「新增方案」保存'
    return
  }
  const error = validate()
  if (error) {
    notice.value = error
    return
  }
  const next = presets.value.map((item) =>
    item.id === editing.value!.id
      ? {
          ...item,
          name: draftName.value.trim(),
          description: draftDescription.value.trim() || undefined,
          values: snapshotValues(),
        }
      : item,
  )
  presets.value = next
  notice.value = `已更新方案「${draftName.value.trim()}」`
}

function setDefault() {
  if (!editingId.value) {
    notice.value = '请先选择一个已有方案'
    return
  }
  defaultPresetId.value = editingId.value
  notice.value = `已将「${editing.value?.name}」设为默认`
}

function removeCurrent() {
  if (!editing.value) {
    notice.value = '请先选择要删除的方案'
    return
  }
  if (presets.value.length <= 1) {
    notice.value = '至少保留一个方案'
    return
  }
  const removed = editing.value
  const next = presets.value.filter((item) => item.id !== removed.id)
  presets.value = next
  if (defaultPresetId.value === removed.id) defaultPresetId.value = next[0]?.id ?? ''
  loadDraft(defaultPresetId.value || undefined)
  notice.value = `已删除方案「${removed.name}」`
}

watch(
  () => [props.params, presets.value] as const,
  () => {
    if (editingId.value && !presets.value.some((item) => item.id === editingId.value)) {
      loadDraft(defaultPresetId.value || presets.value[0]?.id)
    }
  },
  { deep: true },
)

watch(editingId, (id, prev) => {
  if (id !== prev) loadDraft(id || undefined)
})

loadDraft(defaultPresetId.value || presets.value[0]?.id)
</script>

<template>
  <fieldset class="presets" :disabled="disabled">
    <legend>运行方案</legend>
    <p class="hint">在脚本库配置中新增方案；每个参数只能从上方参数选项里选择。</p>

    <template v-if="selectableParams.length">
      <div class="toolbar">
        <label class="grow">
          编辑已有方案
          <select v-model="editingId">
            <option value="">（新建草稿）</option>
            <option v-for="item in presets" :key="item.id" :value="item.id">
              {{ item.name }}{{ item.id === defaultPresetId ? '（默认）' : '' }}
            </option>
          </select>
        </label>
        <button type="button" @click="startNew">新建方案草稿</button>
      </div>

      <div class="edit-card">
        <label>方案名称 <input v-model="draftName" placeholder="例如：切到 8.2" /></label>
        <label>说明 <input v-model="draftDescription" placeholder="可选" /></label>
        <div class="fields">
          <label v-for="param in selectableParams" :key="param.key">
            {{ param.label }}{{ param.required ? '（必填）' : '' }}
            <select v-model="draftValues[param.key]">
              <option value="">未选择</option>
              <option
                v-for="choice in (param.choices ?? []).filter((item) => item.value.trim())"
                :key="choice.value"
                :value="choice.value"
              >
                {{ choice.label || choice.value }}
              </option>
            </select>
          </label>
        </div>
        <p v-if="!paramsReady" class="hint warn">还有参数缺少有效选项，补全后再保存方案。</p>
      </div>

      <p v-if="notice" class="hint">{{ notice }}</p>
      <div class="toolbar">
        <button type="button" class="primary" :disabled="!paramsReady" @click="saveNew">新增方案</button>
        <button type="button" :disabled="!editing || !paramsReady" @click="updateCurrent">更新当前方案</button>
        <button type="button" :disabled="!editing" @click="setDefault">设为默认</button>
        <button type="button" class="danger" :disabled="!editing" @click="removeCurrent">删除方案</button>
      </div>
    </template>
    <p v-else class="hint">暂无可用参数。为脚本添加参数并配置选项后，才能创建方案。</p>
  </fieldset>
</template>

<style scoped>
.presets { display: grid; gap: 0.75rem; }
.grow { flex: 1; min-width: 12rem; }
.edit-card {
  display: grid;
  gap: 0.75rem;
  padding: 0.85rem 1rem;
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius-control);
  background: var(--app-surface);
}
.hint { margin: 0; color: var(--app-muted); font-size: var(--app-font-hint); }
.warn { color: var(--app-warning); }
</style>
