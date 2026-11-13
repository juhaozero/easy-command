<script setup lang="ts">
import type { ParamDef } from '../api/scripts'

const params = defineModel<ParamDef[]>({ required: true })

function add() {
  params.value.push({ key: '', label: '', type: 'string', required: false, maskInLog: false, cli: { mode: 'arg' } })
}
</script>

<template>
  <fieldset class="params">
    <legend>参数定义</legend>
    <fieldset v-for="(param, index) in params" :key="index">
      <legend>{{ param.label || '新参数' }}</legend>
      <div class="fields">
        <label>参数键 <input v-model="param.key" required /></label>
        <label>标签 <input v-model="param.label" required /></label>
        <label>类型
          <select v-model="param.type" @change="delete param.default">
            <option v-for="kind in ['string', 'number', 'boolean', 'password', 'file', 'dir']" :key="kind">{{ kind }}</option>
          </select>
        </label>
        <label v-if="param.type === 'number'">默认值
          <input type="number" step="any" :value="param.default" @input="param.default = ($event.target as HTMLInputElement).value === '' ? undefined : ($event.target as HTMLInputElement).valueAsNumber" />
        </label>
        <label v-else-if="param.type === 'boolean'">默认值
          <select v-model="param.default">
            <option :value="undefined">无</option><option :value="true">是</option><option :value="false">否</option>
          </select>
        </label>
        <label v-else-if="param.type !== 'password'">默认值
          <input :value="param.default" @input="param.default = ($event.target as HTMLInputElement).value || undefined" />
        </label>
        <p v-else>保存参数定义后，可在下方设置密码。</p>
        <label>传参方式
          <select v-model="param.cli.mode"><option>arg</option><option>env</option><option>flag</option></select>
        </label>
        <label v-if="param.cli.mode === 'env'">环境变量名 <input v-model="param.cli.envName" required /></label>
        <label v-if="param.cli.mode === 'flag'">标志名称 <input v-model="param.cli.flag" required /></label>
      </div>
      <label class="check"><input v-model="param.required" type="checkbox" /> 必填</label>
      <label v-if="param.type !== 'password'" class="check"><input v-model="param.maskInLog" type="checkbox" /> 日志脱敏</label>
      <button type="button" @click="params.splice(index, 1)">移除参数</button>
    </fieldset>
    <button type="button" @click="add">添加参数</button>
  </fieldset>
</template>
