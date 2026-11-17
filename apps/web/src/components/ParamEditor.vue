<script setup lang="ts">
import type { ParamDef } from '../api/scripts'

const params = defineModel<ParamDef[]>({ required: true })

function add() {
  params.value.push({
    key: '',
    label: '',
    type: 'string',
    required: false,
    maskInLog: false,
    choices: [],
    cli: { mode: 'arg' },
  })
}

function supportsChoices(type: ParamDef['type']) {
  return type === 'string' || type === 'number' || type === 'boolean'
}

function onTypeChange(param: ParamDef) {
  delete param.default
  if (!supportsChoices(param.type)) param.choices = []
  else if (!param.choices) param.choices = []
}

function addChoice(param: ParamDef) {
  if (!param.choices) param.choices = []
  param.choices.push({ label: '', value: '' })
}

function removeChoice(param: ParamDef, index: number) {
  param.choices?.splice(index, 1)
}
</script>

<template>
  <fieldset class="params">
    <legend>参数选项</legend>
    <p class="hint">定义参数及可选值。运行方案只能从这些选项中组合。</p>
    <fieldset v-for="(param, index) in params" :key="index">
      <legend>{{ param.label || '新参数' }}</legend>
      <div class="fields">
        <label>参数键 <input v-model="param.key" required /></label>
        <label>标签 <input v-model="param.label" required /></label>
        <label>类型
          <select v-model="param.type" @change="onTypeChange(param)">
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

      <template v-if="supportsChoices(param.type)">
        <p class="hint">选项列表（新增方案时只能选这些）</p>
        <div v-for="(choice, choiceIndex) in (param.choices ?? [])" :key="choiceIndex" class="choice-row">
          <input v-model="choice.label" placeholder="显示名，如 PHP 8.2" />
          <input v-model="choice.value" placeholder="传给脚本的值，如 8.2" />
          <button type="button" @click="removeChoice(param, choiceIndex)">删除</button>
        </div>
        <button type="button" @click="addChoice(param)">添加选项</button>
      </template>

      <button type="button" @click="params.splice(index, 1)">移除参数</button>
    </fieldset>
    <button type="button" @click="add">添加参数</button>
  </fieldset>
</template>

<style scoped>
.hint { margin: 0.35rem 0; color: var(--app-muted); font-size: var(--app-font-hint); }
.choice-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
  gap: 0.45rem;
  margin-bottom: 0.45rem;
  align-items: center;
}
@media (max-width: 720px) {
  .choice-row { grid-template-columns: 1fr; }
}
</style>
