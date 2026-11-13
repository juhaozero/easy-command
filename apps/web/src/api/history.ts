import { invoke } from '@tauri-apps/api/core'

export type RunStatus = 'running' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted' | 'skipped'
export const statusLabels: Record<RunStatus, string> = {
  running: '运行中', succeeded: '成功', failed: '失败', cancelled: '已取消', interrupted: '中断', skipped: '已跳过',
}
export interface RunRecord {
  id: string
  scriptId: string
  scriptName: string
  startedAt: string
  endedAt?: string
  exitCode: number | null
  status: RunStatus
  reasonCode?: string
  trigger: string
  argsMasked: Record<string, unknown>
  logRef?: string
}
export interface LogEntry {
  runId: string
  ts: string
  kind: 'start' | 'stdout' | 'stderr' | 'end' | 'interrupted'
  data: unknown
}
export interface LogPage {
  entries: LogEntry[]
  nextCursor: number | null
}
export interface Detail {
  record: RunRecord
  scriptDeleted: boolean
  log?: LogPage
}
export interface Filter {
  scriptId?: string
  status?: RunStatus
  from?: string
  to?: string
  cursor?: string
}
export type Retention = { mode: 'keep_forever' } | { mode: 'days'; days: number }

export const history = {
  list: (filter: Filter) => invoke<{ runs: RunRecord[]; nextCursor: string | null }>('history_list', { filter }),
  get: (runId: string, logCursor?: number) => invoke<Detail>('history_get', { runId, logCursor }),
  settings: () => invoke<{ logRetention: Retention }>('settings_get'),
  save: (logRetention: Retention) => invoke<{ logRetention: Retention }>('settings_set', { logRetention }),
}
