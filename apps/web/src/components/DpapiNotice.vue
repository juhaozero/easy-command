<script setup lang="ts">
withDefaults(defineProps<{ showTitle?: boolean }>(), { showTitle: true })
</script>

<template>
  <div>
    <h3 v-if="showTitle">Windows 密码保护</h3>
    <p>密文绑定当前 Windows 用户。换用户、重装系统或漫游到其他机器后，请重新设置密码。应用会在无法解密时提示。</p>
    <p>保存时，明文经应用内部转发至本机执行引擎，由 Windows DPAPI 加密后写入磁盘。运行时只向脚本提供解密值。使用命令行参数传递时，能读取进程命令行的工具可能看到明文。</p>
    <p>包含密码参数的运行会隐藏脚本输出。</p>
  </div>
</template>
