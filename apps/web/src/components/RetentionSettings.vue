<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { history, type Retention } from '../api/history'

const mode = ref<Retention['mode']>('keep_forever')
const days = ref(30)
const busy = ref(true)
const error = ref('')
const notice = ref('')

onMounted(async () => {
  try {
    const { logRetention } = await history.settings()
    mode.value = logRetention.mode
    if (logRetention.mode === 'days') days.value = logRetention.days
  } catch (failure) { error.value = String(failure) }
  finally { busy.value = false }
})

async function save() {
  if (busy.value) return
  busy.value = true
  error.value = notice.value = ''
  try {
    await history.save(mode.value === 'days' ? { mode: 'days', days: days.value } : { mode: 'keep_forever' })
    notice.value = '已保存日志保留设置'
  } catch (failure) { error.value = String(failure) }
  finally { busy.value = false }
}
</script>

<template>
  <section aria-label="日志保留设置">
    <h2>日志保留</h2>
    <form @submit.prevent="save">
      <label>保留方式
        <select v-model="mode" :disabled="busy">
          <option value="keep_forever">不清理</option>
          <option value="days">按天清理日志和历史</option>
        </select>
      </label>
      <label v-if="mode === 'days'">保留天数
        <input v-model.number="days" :disabled="busy" type="number" required min="1" max="3650" step="1" />
      </label>
      <div><button class="primary" :disabled="busy" type="submit">保存设置</button></div>
    </form>
    <p v-if="notice" role="status">{{ notice }}</p>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
  </section>
</template>
