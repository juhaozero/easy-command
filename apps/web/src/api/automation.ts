import { invoke } from '@tauri-apps/api/core'

export type Timing =
  | { type: 'cron'; expr: string }
  | { type: 'interval'; everySeconds: number }
  | { type: 'daily'; at: string }

export interface Rule {
  id: string
  scriptId: string
  enabled: boolean
  rule: Timing
  argsOverlay?: Record<string, unknown>
}

export interface Hotkey { enabled: boolean; accelerator: string }

export const automation = {
  list: () => invoke<{ rules: Rule[]; paused: boolean }>('sched_list'),
  save: (rule: Rule) => invoke('sched_upsert', { rule }),
  remove: (ruleId: string) => invoke('sched_delete', { ruleId }),
  pause: (paused: boolean) => invoke(paused ? 'sched_pause_all' : 'sched_resume_all'),
  select: (scriptId: string) => invoke('ui_set_selected', { scriptId }),
  state: () => invoke<{ lastSelectedScriptId: string | null; lastRunScriptId: string | null }>('ui_get_state'),
  hotkey: () => invoke<{ hotkey: Hotkey }>('settings_get'),
  saveHotkey: (hotkey: Hotkey) => invoke('settings_set', { hotkey }),
}
