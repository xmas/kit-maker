<script setup lang="ts">
import { router } from '@inertiajs/vue3'
import { Eye, History as HistoryIcon, RotateCcw } from '@lucide/vue'
import { confirm } from '@/lib/confirm'

export interface Revision {
  id: string
  seq: number
  action: string
  slotKey?: string
  summary: string
  actor?: string
  createdAt: string
  current?: boolean
  slot?: { blockName?: string; version?: number; overrides?: unknown[]; label: string } | null
}

// A blueprint's history (or one slot's). Each point can be previewed in the YAML
// panel against the blueprint as it is now, and restored: the whole blueprint, or with
// slotKey just that slot. Restoring is recorded too, so it can be undone.
const props = defineProps<{
  blueprintId: string
  revisions: Revision[]
  slotKey?: string
  previewing?: string | null
  canRestore: boolean
}>()
const emit = defineEmits<{ preview: [rev: Revision | null] }>()

async function restore(r: Revision) {
  const what = props.slotKey ? 'this block' : 'the whole blueprint'
  const ok = await confirm({
    title: `Restore ${what} to #${r.seq}?`,
    message: `“${r.summary}”. ${props.slotKey ? 'Only this slot changes.' : 'Goal, blocks and their edits go back to that point.'} The restore is recorded too, so you can come back.`,
    confirmLabel: 'Restore',
  })
  if (!ok) return
  router.post(`/blueprints/${props.blueprintId}/revisions/${r.id}/restore`, props.slotKey ? { slotKey: props.slotKey } : {}, {
    preserveScroll: true,
    onSuccess: () => emit('preview', null),
  })
}
function state(r: Revision) {
  const s = r.slot
  if (!s) return 'not in the blueprint yet'
  if (!s.blockName) return 'empty'
  const n = s.overrides?.length ?? 0
  return `${s.blockName} v${s.version}${n ? ` · ${n} ${n === 1 ? 'edit' : 'edits'}` : ''}`
}
function when(iso: string) {
  const d = new Date(iso)
  const mins = Math.round((Date.now() - d.getTime()) / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins} min ago`
  if (mins < 60 * 24) return `${Math.round(mins / 60)} h ago`
  return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}
</script>

<template>
  <ol class="flex flex-col">
    <li
      v-for="r in revisions"
      :key="r.id"
      class="flex items-start gap-3 border-t border-slate-100 py-2.5 first:border-t-0"
      :class="previewing === r.id ? '-mx-2 rounded-md bg-primary/5 px-2' : ''"
    >
      <span class="mt-0.5 w-9 shrink-0 font-mono text-[11.5px] text-muted-foreground">#{{ r.seq }}</span>
      <div class="flex min-w-0 flex-1 flex-col gap-0.5">
        <span class="text-[13.5px] leading-snug">{{ r.summary }}</span>
        <span class="text-xs text-muted-foreground">
          {{ r.actor ?? 'Someone' }} · {{ when(r.createdAt) }}<template v-if="slotKey"> · {{ state(r) }}</template>
        </span>
      </div>
      <span v-if="r.current" class="mt-0.5 shrink-0 rounded-full bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-600">Now</span>
      <template v-else>
        <button
          type="button"
          class="inline-flex shrink-0 items-center gap-1 rounded px-2 py-1 text-[12.5px] hover:bg-muted"
          :class="previewing === r.id ? 'font-medium text-primary' : 'text-slate-600'"
          :aria-pressed="previewing === r.id"
          title="Show this point's YAML against now"
          @click="emit('preview', previewing === r.id ? null : r)"
        >
          <Eye class="size-3.5" /> {{ previewing === r.id ? 'Previewing' : 'Preview' }}
        </button>
        <button
          v-if="canRestore"
          type="button"
          class="inline-flex shrink-0 items-center gap-1 rounded px-2 py-1 text-[12.5px] font-medium text-primary hover:bg-primary/5"
          @click="restore(r)"
        >
          <RotateCcw class="size-3.5" /> Restore
        </button>
      </template>
    </li>
    <li v-if="!revisions.length" class="flex items-center gap-2 py-3 text-sm text-muted-foreground">
      <HistoryIcon class="size-4" /> No changes recorded yet. Every change from now on shows here.
    </li>
  </ol>
</template>
