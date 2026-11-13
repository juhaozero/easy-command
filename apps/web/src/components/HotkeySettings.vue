<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { automation, type Hotkey } from '../api/automation'

const config = ref<Hotkey>({ enabled: false, accelerator: '' })
const error = ref('')
const status = ref('')
const busy = ref(true)

onMounted(async () => {
  try { config.value = (await automation.hotkey()).hotkey }
  catch (failure) { error.value = String(failure) }
  finally { busy.value = false }
})

async function save() {
  busy.value = true
  error.value = status.value = ''
  try {
    await automation.saveHotkey(config.value)
    status.value = '已保存快捷键配置'
  } catch (failure) { error.value = String(failure) }
  finally { busy.value = false }
}
</script>

<template>
  <section aria-label="全局快捷键">
    <h2>全局快捷键</h2>
    <form @submit.prevent="save">
      <label class="check"><input v-model="config.enabled" type="checkbox" :disabled="busy" /> 启用</label>
      <label>运行最后选中的脚本
        <input v-model="config.accelerator" maxlength="128" placeholder="Ctrl+Shift+E" :disabled="busy" />
      </label>
      <div><button class="primary" :disabled="busy">保存快捷键</button></div>
    </form>
    <p v-if="status" role="status">{{ status }}</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
  </section>
</template>
