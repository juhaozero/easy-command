import { invoke } from '@tauri-apps/api/core'

export type ConflictPolicy = 'rename' | 'overwrite' | 'cancel'
export type ParamType = 'string' | 'number' | 'boolean' | 'password' | 'file' | 'dir'

export interface ParamDef {
  key: string
  label: string
  type: ParamType
  required: boolean
  default?: string | number | boolean
  passwordStored?: boolean
  updatedAt?: string
  secretBinding?: string
  maskInLog: boolean
  cli: { mode: 'arg' | 'env' | 'flag'; envName?: string; flag?: string }
}

export interface ScriptFields {
  name: string
  description?: string
  enabled: boolean
  dangerous: boolean
  workingDir?: string
  tags?: string[]
  params: ParamDef[]
}

export interface ScriptSummary {
  id: string
  name: string
  enabled: boolean
  dangerous: boolean
  updatedAt: string
}

export interface ScriptMeta extends ScriptFields, ScriptSummary {
  fileName: string
  kind: 'bat' | 'sh'
  createdAt: string
}

interface ScriptPage {
  scripts: ScriptSummary[]
  nextAfterId: string | null
}

interface ContentPage {
  meta: ScriptMeta
  contentBase64: string
  nextOffset: number | null
  revision: string
}

const chunkSize = 256 * 1024
const maxContent = 2 * 1024 * 1024

function toBase64(bytes: Uint8Array) {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
}

export const scripts = {
  list: (afterId?: string) => invoke<ScriptPage>('script_list', { afterId }),
  import: (sourcePath: string, conflictPolicy: ConflictPolicy) =>
    invoke<{ meta: ScriptMeta }>('script_import', { sourcePath, conflictPolicy }),
  updateMeta: (scriptId: string, meta: ScriptFields) =>
    invoke<{ meta: ScriptMeta; selectionInvalidated: boolean }>('script_update_meta', { scriptId, meta }),
  delete: (scriptId: string) =>
    invoke<{ selectionInvalidated: boolean; lastRunInvalidated: boolean }>('script_delete', { scriptId }),

  async get(scriptId: string) {
    const first = await invoke<ContentPage>('script_get', { scriptId })
    const decoder = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true })
    let page = first
    let text = ''
    try {
      while (true) {
        const bytes = Uint8Array.from(atob(page.contentBase64), (char) => char.charCodeAt(0))
        text += decoder.decode(bytes, { stream: page.nextOffset !== null })
        if (page.nextOffset === null) return { meta: first.meta, revision: first.revision, text }
        page = await invoke<ContentPage>('script_get', {
          scriptId, offset: page.nextOffset, revision: first.revision,
        })
      }
    } catch (error) {
      if (error instanceof TypeError) return { meta: first.meta, revision: first.revision, text: null }
      throw error
    }
  },

  async updateContent(scriptId: string, text: string, revision: string) {
    const data = new TextEncoder().encode(text)
    if (data.length > maxContent) throw new Error('SIZE_LIMIT：脚本正文不能超过 2 MiB')
    let uploadId: string | undefined
    let offset = 0
    do {
      const end = Math.min(offset + chunkSize, data.length)
      const final = end === data.length
      const response: { uploadId: string; nextOffset: number; meta?: ScriptMeta } = await invoke(
        'script_update_content', {
          upload: { scriptId, offset, revision, uploadId, final, contentBase64: toBase64(data.subarray(offset, end)) },
        },
      )
      if (final) return response.meta
      uploadId = response.uploadId
      offset = response.nextOffset
    } while (offset < data.length)
  },
}
