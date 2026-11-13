<script setup lang="ts">
import type { ParamDef } from '../api/scripts'
import type { RunArgs } from '../api/execution'

defineProps<{ params: ParamDef[]; disabled: boolean }>()
const values = defineModel<RunArgs>({ required: true })
</script>

<template>
  <fieldset v-if="params.length" :disabled="disabled">
    <legend>运行参数</legend>
    <div class="fields">
      <label v-for="param in params" :key="param.key">
        {{ param.label }}{{ param.required ? '（必填）' : '' }}
        <span v-if="param.type === 'password'">*** · {{ param.passwordStored ? '已保存' : '未保存' }}</span>
        <select v-else-if="param.type === 'boolean'" v-model="values[param.key]" :required="param.required">
          <option :value="null">未填写</option>
          <option :value="true">是</option>
          <option :value="false">否</option>
        </select>
        <input
          v-else-if="param.type === 'number'" type="number" step="any" :required="param.required"
          :value="values[param.key] ?? ''"
          @input="values[param.key] = ($event.target as HTMLInputElement).value === '' ? null : ($event.target as HTMLInputElement).valueAsNumber"
        />
        <input
          v-else v-model="values[param.key]" :required="param.required"
          :placeholder="param.type === 'file' ? '文件路径' : param.type === 'dir' ? '目录路径' : undefined"
        />
      </label>
    </div>
  </fieldset>
</template>
