import { reactive } from 'vue'

// One confirmation dialog for the whole app (rendered by ConfirmDialog in
// AppShell). Every destructive action awaits confirm() before acting.

export interface ConfirmOptions {
  title: string
  message?: string
  confirmLabel?: string
  // destructive: red confirm button, and Cancel has the initial focus.
  destructive?: boolean
}

export const confirmState = reactive<{ open: boolean; options: ConfirmOptions; resolve: ((ok: boolean) => void) | null }>({
  open: false,
  options: { title: '' },
  resolve: null,
})

export function confirm(options: ConfirmOptions): Promise<boolean> {
  confirmState.resolve?.(false) // a second request replaces the first
  return new Promise((resolve) => {
    confirmState.options = { confirmLabel: 'Confirm', destructive: true, ...options }
    confirmState.resolve = resolve
    confirmState.open = true
  })
}

export function settleConfirm(ok: boolean) {
  const r = confirmState.resolve
  confirmState.open = false
  confirmState.resolve = null
  r?.(ok)
}
