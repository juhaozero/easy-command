<script setup lang="ts">
import { ref } from 'vue'
import { secrets } from '../api/secrets'
import type { ParamDef } from '../api/scripts'

const props = defineProps<{ scriptId: string; param: ParamDef; disabled: boolean }>()
const emit = defineEmits<{ changed: [id: string, key: string, updatedAt?: string] }>()
const input = ref<HTMLInputElement>()
const busy = ref(false)
const error = ref('')

async function change(save: boolean) {
  if (busy.value || props.disabled) return
  if (save && !input.value?.value) {
    error.value = '请输入密码'
    return
  }
  busy.value = true
  error.value = ''
  const id = props.scriptId
  const key = props.param.key
  try {
    if (save && input.value) {
      const pending = secrets.set(id, key, input.value.value)
      input.value.value = ''
      const result = await pending
      emit('changed', id, key, result.updatedAt)
    } else {
      if (input.value) input.value.value = ''
      await secrets.clear(id, key)
      emit('changed', id, key)
    }
  } catch (failure) {
    error.value = String(failure)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <fieldset :disabled="disabled || busy">
    <legend>{{ param.label }}</legend>
    <p role="status">{{ param.passwordStored ? '已保存' : '未保存' }}
      <span v-if="param.updatedAt"> · {{ new Date(param.updatedAt).toLocaleString() }}</span>
    </p>
    <label>密码 <input ref="input" type="password" autocomplete="new-password" maxlength="4096" /></label>
    <div class="toolbar">
      <button type="button" @click="change(true)">保存密码</button>
      <button type="button" :disabled="!param.passwordStored" @click="change(false)">清除密码</button>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
  </fieldset>
</template>
