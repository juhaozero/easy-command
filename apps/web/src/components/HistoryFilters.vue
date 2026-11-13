<script setup lang="ts">
import { ref } from 'vue'
import { statusLabels, type Filter, type RunStatus } from '../api/history'

defineProps<{ busy: boolean; scripts: Array<{ id: string; name: string }> }>()
const emit = defineEmits<{ search: [filter: Filter] }>()
const scriptId = ref('')
const status = ref<RunStatus | ''>('')
const from = ref('')
const to = ref('')

function search() {
  emit('search', {
    scriptId: scriptId.value || undefined,
    status: status.value || undefined,
    from: from.value ? new Date(from.value).toISOString() : undefined,
    to: to.value ? new Date(to.value).toISOString() : undefined,
  })
}
</script>

<template>
  <form @submit.prevent="search">
    <div class="fields">
      <label>脚本
        <select v-model="scriptId">
          <option value="">全部脚本</option>
          <option v-for="script in scripts" :key="script.id" :value="script.id">{{ script.name }}</option>
        </select>
      </label>
      <label>状态
        <select v-model="status">
          <option value="">全部状态</option>
          <option v-for="(label, value) in statusLabels" :key="value" :value="value">{{ label }}</option>
        </select>
      </label>
      <label>开始时间从 <input v-model="from" type="datetime-local" /></label>
      <label>开始时间至（不含） <input v-model="to" type="datetime-local" /></label>
    </div>
    <div><button type="submit" :disabled="busy">查询 / 刷新</button></div>
  </form>
</template>
