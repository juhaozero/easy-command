import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'

export type RunArgs = Record<string, string | number | boolean | null>
export type FinalStatus = 'succeeded' | 'failed' | 'cancelled'

interface RunPayload { runId: string; ts: string }

export type RunEvent =
  | { event: 'exec.started'; payload: RunPayload }
  | { event: 'exec.stdout' | 'exec.stderr'; payload: RunPayload & { line: string; truncated: boolean } }
  | { event: 'exec.exit'; payload: RunPayload & { status: FinalStatus; exitCode: number | null } }
  | { event: 'system.log'; payload: RunPayload & { code: 'HISTORY_WRITE_FAILED'; message: string } }

interface BashResult { path: string | null; source: 'manual' | 'registry' | 'common' | null }

export const execution = {
  run: (scriptId: string, args: RunArgs) => invoke<{ runId: string }>('script_run', { scriptId, args }),
  cancel: (runId: string) => invoke('script_cancel', { runId }),
  detectBash: () => invoke<BashResult>('bash_detect'),
  setBashPath: (path: string | null) => invoke<BashResult>('bash_set_path', { path }),
  onRun: (callback: (event: RunEvent) => void) =>
    listen<RunEvent | { event: 'system.protocol_error' | 'sched.fired' }>('engine://evt', ({ payload }) => {
      if (payload.event === 'system.log' || payload.event === 'exec.started' || payload.event === 'exec.stdout' ||
          payload.event === 'exec.stderr' || payload.event === 'exec.exit') callback(payload)
    }),
}
