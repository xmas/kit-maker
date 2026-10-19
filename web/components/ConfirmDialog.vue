<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { TriangleAlert } from '@lucide/vue'
import { confirmState, settleConfirm } from '@/lib/confirm'

// The app's confirmation dialog: modal, Escape or a click outside cancels,
// and for destructive actions Cancel has the focus so Enter doesn't destroy.
const cancelBtn = ref<HTMLButtonElement | null>(null)
const okBtn = ref<HTMLButtonElement | null>(null)
watch(
  () => confirmState.open,
  async (open) => {
    if (!open) return
    await nextTick()
    ;(confirmState.options.destructive ? cancelBtn.value : okBtn.value)?.focus()
  },
)
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') settleConfirm(false)
  if (e.key === 'Tab') {
    // keep focus inside the dialog
    const order = [cancelBtn.value, okBtn.value]
    const i = order.indexOf(document.activeElement as HTMLButtonElement)
    e.preventDefault()
    order[(i + (e.shiftKey ? order.length - 1 : 1)) % order.length]?.focus()
  }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="confirmState.open" class="fixed inset-0 z-[100] flex items-center justify-center bg-slate-900/40 p-4" @mousedown.self="settleConfirm(false)" @keydown="onKey">
      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        :aria-describedby="confirmState.options.message ? 'confirm-message' : undefined"
        class="w-full max-w-md rounded-xl bg-background p-5 shadow-[0_20px_60px_rgba(15,23,42,0.3)]"
      >
        <div class="flex gap-3">
          <span
            v-if="confirmState.options.destructive"
            class="flex size-9 shrink-0 items-center justify-center rounded-full bg-red-50 text-red-600"
            aria-hidden="true"
          >
            <TriangleAlert class="size-4.5" />
          </span>
          <div class="flex min-w-0 flex-col gap-1.5">
            <h2 id="confirm-title" class="text-[15px] font-semibold">{{ confirmState.options.title }}</h2>
            <p v-if="confirmState.options.message" id="confirm-message" class="text-sm leading-relaxed text-slate-600">
              {{ confirmState.options.message }}
            </p>
          </div>
        </div>
        <div class="mt-5 flex justify-end gap-2">
          <button ref="cancelBtn" type="button" class="rounded-lg border px-3.5 py-1.5 text-sm font-medium hover:bg-muted" @click="settleConfirm(false)">
            Cancel
          </button>
          <button
            ref="okBtn"
            type="button"
            class="rounded-lg px-3.5 py-1.5 text-sm font-medium text-white"
            :class="confirmState.options.destructive ? 'bg-red-600 hover:bg-red-700' : 'bg-primary hover:bg-primary/90'"
            @click="settleConfirm(true)"
          >
            {{ confirmState.options.confirmLabel }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
