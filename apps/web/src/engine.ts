import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'

export interface EngineStatus {
  revision: number
  phase: 'starting' | 'ready' | 'stopping' | 'backoff'
  message: string
  pid: number | null
  version: string | null
  dataDir: string | null
  retryMs: number | null
}

interface ProtocolError {
  event: 'system.protocol_error'
  payload: { code: string; message: string; truncated: boolean }
}

export const engine = {
  status: () => invoke<EngineStatus>('engine_status'),
  ping: () => invoke<{ pong: true }>('engine_ping'),
  shutdown: () => invoke<Record<string, never>>('engine_shutdown'),
  onStatus: (handle: (status: EngineStatus) => void) =>
    listen<EngineStatus>('engine://status', ({ payload }) => handle(payload)),
  onProtocolError: (handle: (error: ProtocolError['payload']) => void) =>
    listen<ProtocolError>('engine://evt', ({ payload }) => handle(payload.payload)),
}
