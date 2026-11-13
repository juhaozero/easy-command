import { invoke } from '@tauri-apps/api/core'

export interface DesktopPreferences {
  closeToTray: boolean
  firstCloseTipShown: boolean
  onboardingCompleted: boolean
  updater: { checkOnStartup: boolean }
}

export const desktop = {
  checkUpdate: () => invoke<'current' | 'busy' | 'cancelled' | 'installed'>('check_update'),
  settings: () => invoke<DesktopPreferences>('settings_get'),
  save: (patch: Partial<DesktopPreferences>) => invoke<Partial<DesktopPreferences>>('settings_set', patch),
}
