import { open } from '@tauri-apps/plugin-dialog'

const scriptFilters = [{ name: '脚本', extensions: ['bat', 'sh'] }]

export async function pickScriptFiles(): Promise<string[]> {
  const selected = await open({
    multiple: true,
    filters: scriptFilters,
    title: '选择要导入的脚本',
  })
  if (Array.isArray(selected)) return selected.filter(Boolean)
  if (typeof selected === 'string' && selected) return [selected]
  return []
}

export async function pickScriptFolder(): Promise<string | null> {
  const selected = await open({
    multiple: false,
    directory: true,
    title: '选择脚本文件夹',
  })
  return typeof selected === 'string' && selected ? selected : null
}
