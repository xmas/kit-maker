<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { Sparkles } from '@lucide/vue'
import { useDismiss } from '@/lib/useDismiss'
import ExplainPanel from './ExplainPanel.vue'

// Explain / Ask AI as a header button: the answer streams into a popover that
// floats over the editor instead of pushing it down. The panel stays mounted
// while hidden, so an answer is still there when the popover reopens.
const props = defineProps<{ href: string }>()
const open = ref(false)
const root = ref<HTMLElement | null>(null)
useDismiss(root, open)
watch(
  () => props.href,
  () => (open.value = false),
)
watch(open, async (o) => {
  if (!o) return
  await nextTick()
  root.value?.querySelector<HTMLInputElement>('input[aria-label="Ask AI about this block"]')?.focus()
})
</script>

<template>
  <div ref="root" class="relative">
    <button
      type="button"
      class="inline-flex h-8 items-center gap-1.5 rounded-md bg-ai px-2.5 text-[13px] font-medium text-white hover:bg-ai/90"
      :aria-expanded="open"
      aria-haspopup="dialog"
      title="Ask AI about this block"
      @click="open = !open"
    >
      <Sparkles class="size-3.5" /> Explain
    </button>
    <div
      v-show="open"
      role="dialog"
      aria-label="Ask AI about this block"
      class="absolute top-full right-0 z-40 mt-2 w-[min(460px,85vw)] rounded-xl border bg-background p-3 text-foreground shadow-[0_16px_40px_rgba(15,23,42,0.2)]"
    >
      <ExplainPanel :href="href" />
    </div>
  </div>
</template>
