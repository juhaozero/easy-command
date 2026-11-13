<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { desktop } from '../api/desktop'

const closeToTray = ref(true)
const checkOnStartup = ref(false)
const ready = ref(false)
const busy = ref(true)
const error = ref('')
const notice = ref('')
const checking = ref(false)

async function checkUpdate() {
  checking.value = true
  error.value = notice.value = ''
  try {
    const result = await desktop.checkUpdate()
    const status = {
      current: '当前已是最新版本',
      busy: '正在检查或安装更新',
      cancelled: '已取消安装',
      installed: '已启动安装程序',
    }
    notice.value = status[result]
  } catch (failure) { error.value = String(failure) }
  finally { checking.value = false }
}

onMounted(async () => {
  try {
    const settings = await desktop.settings()
    closeToTray.value = settings.closeToTray
    checkOnStartup.value = settings.updater.checkOnStartup
    ready.value = true
  } catch (failure) { error.value = String(failure) }
  finally { busy.value = false }
})

async function save() {
  if (busy.value || !ready.value) return
  busy.value = true
  error.value = notice.value = ''
  try {
    await desktop.save({
      closeToTray: closeToTray.value,
      updater: { checkOnStartup: checkOnStartup.value },
    })
    notice.value = '已保存桌面设置'
  } catch (failure) { error.value = String(failure) }
  finally { busy.value = false }
}
</script>

<template>
  <section aria-label="窗口和更新设置">
    <h2>窗口和更新</h2>
    <form @submit.prevent="save">
      <label class="check"><input v-model="closeToTray" type="checkbox" :disabled="busy || !ready" /> 关闭窗口时隐藏到托盘</label>
      <label class="check"><input v-model="checkOnStartup" type="checkbox" :disabled="busy || !ready" /> 启动时检查更新</label>
      <div class="toolbar">
        <button class="primary" :disabled="busy || !ready">保存桌面设置</button>
        <button type="button" :disabled="checking || busy || !ready" @click="checkUpdate">
          {{ checking ? '正在检查更新…' : '检查更新' }}
        </button>
      </div>
    </form>
    <p v-if="notice" role="status">{{ notice }}</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
  </section>
</template>
