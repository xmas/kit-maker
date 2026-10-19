<script setup lang="ts">
import { ref, type Component } from 'vue'
import { Ellipsis } from '@lucide/vue'
import { useDismiss } from '@/lib/useDismiss'

// A "⋯" menu for less common actions. Items with danger are shown in red
// (and their handlers confirm before doing anything).
export interface MenuItem {
  label: string
  icon?: Component
  danger?: boolean
  hint?: string
  onSelect: () => void
}
defineProps<{ items: MenuItem[]; label?: string }>()

const open = ref(false)
const root = ref<HTMLElement | null>(null)
useDismiss(root, open)
function pick(item: MenuItem) {
  open.value = false
  item.onSelect()
}
</script>

<template>
  <div ref="root" class="relative">
    <button
      type="button"
      class="inline-flex size-8 items-center justify-center rounded-md bg-white/15 hover:bg-white/25"
      :aria-label="label ?? 'More actions'"
      :aria-expanded="open"
      aria-haspopup="menu"
      @click="open = !open"
    >
      <Ellipsis class="size-4" />
    </button>
    <div
      v-if="open"
      role="menu"
      class="absolute top-full right-0 z-50 mt-1.5 min-w-[220px] rounded-lg border bg-background p-1 text-foreground shadow-[0_12px_32px_rgba(15,23,42,0.18)]"
    >
      <button
        v-for="item in items"
        :key="item.label"
        type="button"
        role="menuitem"
        class="flex w-full items-start gap-2.5 rounded-md px-2.5 py-2 text-left text-[13px] hover:bg-muted"
        :class="item.danger ? 'text-red-700 hover:bg-red-50' : ''"
        @click="pick(item)"
      >
        <component :is="item.icon" v-if="item.icon" class="mt-0.5 size-3.5 shrink-0" />
        <span class="flex flex-col">
          <span class="font-medium">{{ item.label }}</span>
          <span v-if="item.hint" class="text-xs font-normal text-muted-foreground">{{ item.hint }}</span>
        </span>
      </button>
    </div>
  </div>
</template>
