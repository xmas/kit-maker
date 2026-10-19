<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { CircleAlert, CircleCheck, Copy, PanelRightClose } from '@lucide/vue'
import type { Compiled } from '@/lib/kit'

// The kit as YAML, always beside the editor. The lines the selected slot
// produced are highlighted and scrolled into view.
const props = defineProps<{ compiled: Compiled; slug: string; highlight?: [number, number][] }>()
const emit = defineEmits<{ hide: [] }>()

interface Line {
  n: number
  indent: string
  dash: string
  key: string
  rest: string
  comment: string
  str: boolean
  hl: boolean
  first: boolean
}

const lines = computed<Line[]>(() => {
  const spans = props.highlight ?? []
  return (props.compiled.yaml || '').replace(/\n$/, '').split('\n').map((line, i) => {
    const n = i + 1
    const hl = spans.some(([a, b]) => n >= a && n <= b)
    const first = spans.some(([a]) => n === a)
    if (line.trim().startsWith('#')) return { n, indent: '', dash: '', key: '', rest: '', comment: line, str: false, hl, first }
    const m = line.match(/^(\s*)(- )?([A-Za-z0-9_]+)(:)(.*)$/)
    const rest = m ? (m[4] ?? '') + (m[5] ?? '') : line
    return {
      n,
      indent: m ? (m[1] ?? '') : '',
      dash: m ? (m[2] ?? '') : '',
      key: m ? (m[3] ?? '') : '',
      rest,
      comment: '',
      str: rest.includes('"'),
      hl,
      first,
    }
  })
})

// Bring the highlighted block into view inside the panel (not the page).
const scroller = ref<HTMLElement | null>(null)
watch(
  () => props.highlight,
  async () => {
    await nextTick()
    const box = scroller.value
    const el = box?.querySelector<HTMLElement>('[data-first]')
    if (!box || !el) return
    const top = el.offsetTop
    if (top < box.scrollTop || top > box.scrollTop + box.clientHeight - 40) {
      box.scrollTo({ top: Math.max(0, top - 48), behavior: 'smooth' })
    }
  },
  { immediate: true },
)

async function copy() {
  try {
    await navigator.clipboard.writeText(props.compiled.yaml)
  } catch {
    /* clipboard may be unavailable */
  }
}
</script>

<template>
  <section aria-label="Kit YAML" class="flex min-h-0 min-w-0 flex-col overflow-hidden rounded-xl border bg-[#0B0D11] text-[#D5D9E0]">
    <div class="flex items-center gap-2 border-b border-white/10 bg-white/5 px-3 py-2">
      <span class="truncate font-mono text-xs">{{ slug }}.yaml</span>
      <span v-if="compiled.valid" class="inline-flex shrink-0 items-center gap-1 text-xs text-green-400">
        <CircleCheck class="size-3.5" /> valid kit v3
      </span>
      <span v-else class="inline-flex shrink-0 items-center gap-1 text-xs text-amber-300">
        <CircleAlert class="size-3.5" /> not valid yet
      </span>
      <span class="flex-1" />
      <button type="button" class="rounded p-1 text-white/60 hover:bg-white/10 hover:text-white" aria-label="Copy YAML" title="Copy" @click="copy">
        <Copy class="size-3.5" />
      </button>
      <button type="button" class="rounded p-1 text-white/60 hover:bg-white/10 hover:text-white" aria-label="Hide YAML" title="Hide YAML" @click="emit('hide')">
        <PanelRightClose class="size-3.5" />
      </button>
    </div>
    <div v-if="compiled.error" role="alert" class="border-b border-white/10 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
      {{ compiled.error }}
    </div>
    <div v-if="compiled.blanks?.length" class="border-b border-white/10 px-3 py-2 text-xs text-white/60">
      {{ compiled.blanks.length }} empty {{ compiled.blanks.length === 1 ? 'slot' : 'slots' }} not in the YAML yet: {{ compiled.blanks.join(', ') }}
    </div>
    <div ref="scroller" class="relative min-h-0 flex-1 overflow-auto py-2 font-mono text-[12px] leading-[1.6]">
      <div
        v-for="l in lines"
        :key="l.n"
        :data-first="l.first ? '' : undefined"
        class="flex whitespace-pre"
        :class="l.hl ? 'bg-[#1D63ED]/25 shadow-[inset_3px_0_0_#5B8DF5]' : ''"
      >
        <span class="w-9 shrink-0 pr-3 text-right text-white/25 select-none">{{ l.n }}</span>
        <span v-if="l.comment" :class="l.hl ? 'text-[#A9C1F5]' : 'text-[#7D8696]'">{{ l.comment }}</span>
        <span v-else
          >{{ l.indent }}{{ l.dash }}<span v-if="l.key" class="text-[#8AB4F8]">{{ l.key }}</span
          ><span :class="{ 'text-[#F0B37E]': l.str }">{{ l.rest }}</span></span
        >
      </div>
    </div>
    <div v-if="compiled.digest" class="border-t border-white/10 px-3 py-1.5 font-mono text-[11px] text-white/40">{{ compiled.digest.slice(0, 23) }}…</div>
  </section>
</template>
