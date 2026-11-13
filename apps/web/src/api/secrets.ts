import { invoke } from '@tauri-apps/api/core'

export const secrets = {
  set: (scriptId: string, paramKey: string, plaintext: string) =>
    invoke<{ updatedAt: string }>('secret_set', { scriptId, paramKey, plaintext }),
  clear: (scriptId: string, paramKey: string) =>
    invoke<Record<string, never>>('secret_clear', { scriptId, paramKey }),
}
