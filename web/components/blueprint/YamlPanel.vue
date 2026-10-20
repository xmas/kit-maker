<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { CircleAlert, CircleCheck, Copy, PanelRightClose, Play, WrapText } from '@lucide/vue'
import { diffLines, splitLines } from '@/lib/diff'
import type { Compiled } from '@/lib/blueprint'

// The blueprint as YAML, always beside the editor. The selected slot's lines are
// highlighted. While there are unsaved edits, `preview` is the blueprint they will
// produce, shown against the saved YAML:
//   result:  what the YAML will be, new lines green, changed amber, and a red
//            mark where lines were removed (the default)
//   changes: removed and added lines, like a diff
const props = defineProps<{
  compiled: Compiled
  preview?: Compiled | null
  slug: string
  highlight?: [number, number][]
  // previewLabel names what `preview` is when it isn't unsaved edits,
  // e.g. "Previewing #12" for a point in history.
  previewLabel?: string
}>()
const emit = defineEmits<{ hide: []; open: [slotKey: string]; run: [] }>()

// Word wrap, on unless turned off (remembered per browser).
const wrap = ref(true)
try {
  wrap.value = localStorage.getItem('kitmaker.yaml.wrap') !== '0'
} catch {
  /* storage may be unavailable */
}
watch(wrap, (w) => {
  try {
    localStorage.setItem('kitmaker.yaml.wrap', w ? '1' : '0')
  } catch {
    /* storage may be unavailable */
  }
})

// Clicking a line opens the slot that produced it (spans map slots to lines).
function slotAt(no?: number): string | undefined {
  if (no === undefined) return undefined
  for (const [key, ranges] of Object.entries(shown.value.spans ?? {})) {
    if (ranges.some(([a, b]) => no >= a && no <= b)) return key
  }
  return undefined
}
function openLine(no?: number) {
  const key = slotAt(no)
  if (!key || window.getSelection()?.toString()) return // let text selection be
  emit('open', key)
}

const mode = ref<'result' | 'changes'>(storedMode())
function storedMode(): 'result' | 'changes' {
  try {
    return localStorage.getItem('kitmaker.yaml.mode') === 'changes' ? 'changes' : 'result'
  } catch {
    return 'result'
  }
}
watch(mode, (m) => {
  try {
    localStorage.setItem('kitmaker.yaml.mode', m)
  } catch {
    /* storage may be unavailable */
  }
})

const shown = computed(() => props.preview ?? props.compiled)

interface Row {
  key: string
  no?: number // line number in the YAML being shown
  sign: '' | '+' | '-'
  kind: 'same' | 'add' | 'change' | 'del'
  removedBefore: number // result mode: lines removed just above this one
  text: string
}

const ops = computed(() => (props.preview ? diffLines(splitLines(props.compiled.yaml), splitLines(props.preview.yaml)) : null))
const stats = computed(() => {
  const o = ops.value ?? []
  return { add: o.filter((x) => x.type === 'add').length, del: o.filter((x) => x.type === 'del').length }
})
const changed = computed(() => stats.value.add + stats.value.del > 0)

const rows = computed<Row[]>(() => {
  if (!ops.value) {
    return splitLines(props.compiled.yaml).map((text, i) => ({ key: `s${i}`, no: i + 1, sign: '', kind: 'same', removedBefore: 0, text }))
  }
  const out: Row[] = []
  if (mode.value === 'changes') {
    ops.value.forEach((o, i) =>
      out.push({
        key: `c${i}`,
        no: o.newNo,
        sign: o.type === 'add' ? '+' : o.type === 'del' ? '-' : '',
        kind: o.type === 'add' ? 'add' : o.type === 'del' ? 'del' : 'same',
        removedBefore: 0,
        text: o.text,
      }),
    )
    return out
  }
  // Result: hide removed lines, count them onto the next line shown, and call
  // an added line "changed" when it replaces removed ones.
  let removed = 0
  let replacing = false
  ops.value.forEach((o, i) => {
    if (o.type === 'del') {
      removed++
      replacing = true
      return
    }
    if (o.type === 'same') replacing = false
    const kind = o.type === 'add' ? (replacing ? 'change' : 'add') : 'same'
    // A red mark only for lines that went away without a replacement.
    out.push({ key: `r${i}`, no: o.newNo, sign: '', kind, removedBefore: kind === 'same' ? removed : 0, text: o.text })
    if (kind === 'same') removed = 0
    else if (kind === 'change') removed = Math.max(0, removed - 1)
  })
  if (removed) out.push({ key: 'r-end', sign: '', kind: 'same', removedBefore: removed, text: '' })
  return out
})

// Syntax colouring for one line: comments, keys, quoted strings.
function parts(text: string) {
  if (text.trim().startsWith('#')) return { comment: text, indent: '', dash: '', key: '', rest: '', str: false }
  const m = text.match(/^(\s*)(- )?([A-Za-z0-9_]+)(:)(.*)$/)
  const rest = m ? (m[4] ?? '') + (m[5] ?? '') : text
  return { comment: '', indent: m ? (m[1] ?? '') : '', dash: m ? (m[2] ?? '') : '', key: m ? (m[3] ?? '') : '', rest, str: rest.includes('"') }
}

// Edits saved in the blueprint (vs. the library), from the compiler's marks.
const markBar: Record<string, string> = { added: 'bg-emerald-400', changed: 'bg-amber-400', removed: 'bg-red-400' }
const markText: Record<string, string> = { added: 'text-emerald-300', changed: 'text-amber-300', removed: 'text-red-300' }
function mark(no?: number) {
  return no === undefined ? undefined : shown.value.marks?.[String(no)]
}
const hasMarks = computed(() => Object.keys(shown.value.marks ?? {}).length > 0)

function inSpan(no?: number) {
  return no !== undefined && (props.highlight ?? []).some(([a, b]) => no >= a && no <= b)
}
function isSpanStart(no?: number) {
  return no !== undefined && (props.highlight ?? []).some(([a]) => no === a)
}
const rowClass: Record<Row['kind'], string> = {
  same: '',
  add: 'bg-emerald-500/15',
  change: 'bg-amber-400/15',
  del: 'bg-red-500/15 text-[#E9A3A3]',
}
const signClass: Record<string, string> = { '+': 'text-emerald-400', '-': 'text-red-400', '': '' }

// Bring the selected block (or the first change) into view inside the panel.
const scroller = ref<HTMLElement | null>(null)
async function reveal(selector: string, smooth: boolean) {
  await nextTick()
  const box = scroller.value
  const el = box?.querySelector<HTMLElement>(selector)
  if (!box || !el) return
  const top = el.offsetTop
  if (top < box.scrollTop || top > box.scrollTop + box.clientHeight - 40) {
    box.scrollTo({ top: Math.max(0, top - 48), behavior: smooth ? 'smooth' : 'auto' })
  }
}
watch(() => props.highlight, () => reveal('[data-span-start]', true), { immediate: true })
watch(
  () => props.preview?.yaml,
  () => reveal('[data-changed]', false),
)

async function copy() {
  try {
    await navigator.clipboard.writeText(shown.value.yaml)
  } catch {
    /* clipboard may be unavailable */
  }
}
</script>

<template>
  <section aria-label="Blueprint YAML" class="flex min-h-0 min-w-0 flex-col overflow-hidden rounded-xl border bg-[#0B0D11] text-[#D5D9E0]">
    <div class="flex items-center gap-2 border-b border-white/10 bg-white/5 px-3 py-2">
      <span class="truncate font-mono text-xs">{{ slug }}.yaml</span>
      <span v-if="shown.valid" class="inline-flex shrink-0 items-center gap-1 text-xs text-green-400">
        <CircleCheck class="size-3.5" /> valid kit v3
      </span>
      <span v-else class="inline-flex shrink-0 items-center gap-1 text-xs text-amber-300">
        <CircleAlert class="size-3.5" /> not valid yet
      </span>
      <span class="flex-1" />
      <button
        type="button"
        class="inline-flex h-6 items-center gap-1 rounded-md bg-white/10 px-2 text-xs font-medium text-white hover:bg-white/20"
        title="Run this blueprint locally with sbx"
        @click="emit('run')"
      >
        <Play class="size-3" /> Run
      </button>
      <button
        type="button"
        class="rounded p-1 hover:bg-white/10 hover:text-white"
        :class="wrap ? 'text-white' : 'text-white/60'"
        :aria-pressed="wrap"
        aria-label="Wrap long lines"
        title="Wrap long lines"
        @click="wrap = !wrap"
      >
        <WrapText class="size-3.5" />
      </button>
      <button type="button" class="rounded p-1 text-white/60 hover:bg-white/10 hover:text-white" aria-label="Copy YAML" title="Copy" @click="copy">
        <Copy class="size-3.5" />
      </button>
      <button type="button" class="rounded p-1 text-white/60 hover:bg-white/10 hover:text-white" aria-label="Hide YAML" title="Hide YAML" @click="emit('hide')">
        <PanelRightClose class="size-3.5" />
      </button>
    </div>

    <!-- Unsaved edits: summary and view toggle -->
    <div v-if="preview && (changed || previewLabel)" class="flex items-center gap-2 border-b border-white/10 bg-[#141821] px-3 py-1.5 text-xs">
      <span class="text-white/70">{{ previewLabel ?? 'Unsaved' }}</span>
      <span v-if="previewLabel && !changed" class="text-white/50">same as now</span>
      <span class="font-mono text-emerald-400">+{{ stats.add }}</span>
      <span class="font-mono text-red-400">−{{ stats.del }}</span>
      <span class="flex-1" />
      <div role="tablist" aria-label="YAML view" class="flex rounded-md bg-white/5 p-0.5">
        <button
          v-for="m in (['result', 'changes'] as const)"
          :key="m"
          type="button"
          role="tab"
          :aria-selected="mode === m"
          class="rounded px-2 py-0.5"
          :class="mode === m ? 'bg-white/15 text-white' : 'text-white/55 hover:text-white'"
          @click="mode = m"
        >
          {{ m === 'result' ? 'Result' : 'Changes' }}
        </button>
      </div>
    </div>

    <div v-if="shown.error" role="alert" class="border-b border-white/10 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
      {{ shown.error }}
    </div>
    <div v-if="shown.blanks?.length" class="border-b border-white/10 px-3 py-2 text-xs text-white/60">
      {{ shown.blanks.length }} empty {{ shown.blanks.length === 1 ? 'slot' : 'slots' }} not in the YAML yet: {{ shown.blanks.join(', ') }}
    </div>

    <div ref="scroller" class="relative min-h-0 flex-1 overflow-auto py-2 font-mono text-[12px] leading-[1.6]">
      <div
        v-for="r in rows"
        :key="r.key"
        :data-span-start="isSpanStart(r.no) ? '' : undefined"
        :data-changed="r.kind !== 'same' || r.removedBefore ? '' : undefined"
        class="relative flex"
        :class="[
          wrap ? 'whitespace-pre-wrap' : 'whitespace-pre',
          rowClass[r.kind],
          inSpan(r.no) ? 'shadow-[inset_3px_0_0_#5B8DF5]' : '',
          inSpan(r.no) && r.kind === 'same' ? 'bg-[#1D63ED]/20' : '',
          slotAt(r.no) && !inSpan(r.no) ? 'cursor-pointer hover:bg-white/5' : '',
        ]"
        :title="slotAt(r.no) && !inSpan(r.no) ? `Open ${slotAt(r.no)}` : undefined"
        @click="openLine(r.no)"
      >
        <span
          v-if="r.removedBefore"
          class="absolute top-0 right-0 left-0 h-0.5 bg-red-400"
          :title="`${r.removedBefore} ${r.removedBefore === 1 ? 'line' : 'lines'} removed here`"
          aria-hidden="true"
        />
        <span
          class="w-1 shrink-0 self-stretch"
          :class="mark(r.no) ? markBar[mark(r.no)!] : ''"
          :title="mark(r.no) ? `${mark(r.no)} in this blueprint` : undefined"
          aria-hidden="true"
        />
        <span class="w-8 shrink-0 pr-2 text-right select-none" :class="mark(r.no) ? markText[mark(r.no)!] : 'text-white/25'">{{ r.no ?? '' }}</span>
        <span class="w-3 shrink-0 select-none" :class="signClass[r.sign]">{{ r.sign }}</span>
        <template v-for="p in [parts(r.text)]" :key="0">
          <span
            v-if="p.comment"
            :class="[mark(r.no) ? markText[mark(r.no)!] : inSpan(r.no) ? 'text-[#A9C1F5]' : 'text-[#7D8696]', wrap ? 'min-w-0 flex-1 [overflow-wrap:anywhere]' : '']"
            >{{ p.comment }}</span
          >
          <span v-else :class="[r.kind === 'del' ? 'line-through decoration-red-400/50' : '', wrap ? 'min-w-0 flex-1 [overflow-wrap:anywhere]' : '']"
            >{{ p.indent }}{{ p.dash }}<span v-if="p.key" :class="r.kind === 'del' ? '' : 'text-[#8AB4F8]'">{{ p.key }}</span
            ><span :class="{ 'text-[#F0B37E]': p.str && r.kind !== 'del' }">{{ p.rest }}</span></span
          >
        </template>
      </div>
    </div>

    <div class="flex items-center gap-3 border-t border-white/10 px-3 py-1.5 text-[11px] text-white/40">
      <span v-if="shown.digest" class="font-mono">{{ shown.digest.slice(0, 23) }}…</span>
      <template v-if="!(preview && changed) && hasMarks">
        <span class="flex-1" />
        <span>blueprint edits:</span>
        <span class="inline-flex items-center gap-1"><i class="h-2.5 w-1 bg-emerald-400" /> added</span>
        <span class="inline-flex items-center gap-1"><i class="h-2.5 w-1 bg-amber-400" /> changed</span>
        <span class="inline-flex items-center gap-1"><i class="h-2.5 w-1 bg-red-400" /> removed</span>
      </template>
      <template v-if="preview && changed && mode === 'result'">
        <span class="flex-1" />
        <span class="inline-flex items-center gap-1"><i class="size-2 rounded-sm bg-emerald-500/60" /> new</span>
        <span class="inline-flex items-center gap-1"><i class="size-2 rounded-sm bg-amber-400/60" /> changed</span>
        <span class="inline-flex items-center gap-1"><i class="h-0.5 w-2.5 bg-red-400" /> removed</span>
      </template>
    </div>
  </section>
</template>
