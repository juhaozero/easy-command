<script setup lang="ts">
import { computed, ref } from 'vue'
import { desktop } from '../api/desktop'
import { execution } from '../api/execution'
import { scripts, type ConflictPolicy } from '../api/scripts'
import type { EngineStatus } from '../engine'
import DpapiNotice from './DpapiNotice.vue'

const props = defineProps<{ engineStatus?: EngineStatus }>()
const emit = defineEmits<{ completed: [importedId?: string] }>()
const titles = ['欢迎使用 Easy Command', '数据目录', 'Git Bash', 'Windows 密码保护', '导入第一个脚本', '准备就绪']
const step = ref(0)
const busy = ref(false)
const error = ref('')
const bash = ref<Awaited<ReturnType<typeof execution.detectBash>>>()
const bashPath = ref('')
const sourcePath = ref('')
const conflictPolicy = ref<ConflictPolicy>('cancel')
const imported = ref<{ id: string; name: string }>()
const ready = computed(() => props.engineStatus?.phase === 'ready' && !!props.engineStatus.dataDir)

async function work(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try { await action() }
  catch (failure) { error.value = String(failure) }
  finally { busy.value = false }
}

async function probeBash() {
  bash.value = await execution.detectBash()
  bashPath.value = bash.value.source === 'manual' ? bash.value.path ?? '' : ''
}

function next() {
  if (step.value >= titles.length - 1 || (step.value === 1 && !ready.value)) return
  error.value = ''
  step.value++
  if (step.value === 2) void work(probeBash)
}

function saveBash() {
  void work(async () => {
    const path = bashPath.value.trim()
    if (!path) throw new Error('请输入 bash.exe 的绝对路径')
    bash.value = await execution.setBashPath(path)
    bashPath.value = bash.value.path ?? ''
  })
}

function importFirst() {
  void work(async () => {
    const { meta } = await scripts.import(sourcePath.value.trim(), conflictPolicy.value)
    imported.value = { id: meta.id, name: meta.name }
  })
}

function finish() {
  void work(async () => {
    await desktop.save({ onboardingCompleted: true })
    emit('completed', imported.value?.id)
  })
}
</script>

<template>
  <main class="onboarding-page">
    <section class="panel" aria-labelledby="onboarding-title">
      <h1 id="onboarding-title">{{ titles[step] }}</h1>

      <p v-if="step === 0">导入本地 .bat 或 .sh 脚本后，即可在脚本库编辑和运行。</p>
      <div v-else-if="step === 1">
        <p v-if="ready" role="status">数据目录已就绪</p>
        <code v-if="ready">{{ engineStatus?.dataDir }}</code>
        <p v-else role="status">{{ engineStatus?.message ?? '等待执行引擎初始化' }}</p>
      </div>
      <div v-else-if="step === 2">
        <p v-if="bash?.path" role="status">已找到 Git Bash：{{ bash.path }}</p>
        <p v-else-if="bash" role="status">未找到 Git Bash。安装或指定路径后即可运行 .sh 脚本。</p>
        <p><a href="https://git-scm.com/install/windows" target="_blank" rel="noopener noreferrer">Git for Windows 安装指引</a></p>
        <form @submit.prevent="saveBash">
          <label>手动指定 bash.exe
            <input v-model="bashPath" required placeholder="C:\Program Files\Git\bin\bash.exe" :disabled="busy" />
          </label>
          <div class="onboarding-actions">
            <button type="button" :disabled="busy" @click="work(probeBash)">重新探测</button>
            <button type="submit" :disabled="busy">保存路径</button>
          </div>
        </form>
      </div>
      <DpapiNotice v-else-if="step === 3" :show-title="false" />
      <div v-else-if="step === 4">
        <p v-if="imported" role="status">已导入 {{ imported.name }}。可继续完成引导。</p>
        <form v-else @submit.prevent="importFirst">
          <label>源文件绝对路径 <input v-model="sourcePath" required placeholder="C:\scripts\example.bat" :disabled="busy" /></label>
          <label>遇到同名脚本
            <select v-model="conflictPolicy" :disabled="busy">
              <option value="cancel">取消导入</option>
              <option value="rename">重命名新脚本</option>
              <option value="overwrite">覆盖脚本及元数据</option>
            </select>
          </label>
          <div><button type="submit" :disabled="busy">导入脚本</button></div>
        </form>
      </div>
      <p v-else>完成后进入脚本库。</p>

      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <div class="onboarding-actions">
        <button type="button" :disabled="busy || !ready" @click="finish">跳过引导</button>
        <button v-if="step > 0" type="button" :disabled="busy" @click="step--; error = ''">上一步</button>
        <button v-if="step < titles.length - 1" type="button" class="primary"
          :disabled="busy || (step === 1 && !ready)" @click="next">下一步</button>
        <button v-else type="button" class="primary" :disabled="busy || !ready" @click="finish">完成</button>
      </div>
    </section>
  </main>
</template>

<style scoped>
.onboarding-page { max-width: 46rem; margin: 2rem auto; padding: 0 1rem; }
.onboarding-actions { display: flex; flex-wrap: wrap; gap: .5rem; margin-top: 1rem; }
code { overflow-wrap: anywhere; }
</style>
