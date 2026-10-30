<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Link, router } from '@inertiajs/vue3'
import { ChevronRight, History, LayoutDashboard, Plus } from '@lucide/vue'
import OriginIcon from './OriginIcon.vue'
import { originStyle, sectionStyle, slotHref, type Item, type BlueprintInfo, type Section } from '@/lib/blueprint'

const props = defineProps<{
  blueprint: BlueprintInfo
  sections: Section[]
  selected: string | null
  canEdit?: boolean
  // dirtyKey: the slot with unsaved edits in the editor (pencil).
  dirtyKey?: string | null
  showHistory?: boolean
  view?: string
}>()

// The outline scrolls by itself; keep the selected row in view (without
// scrolling the page, which scrollIntoView would also do).
const scroller = ref<HTMLElement | null>(null)
async function revealSelected() {
  await nextTick()
  const box = scroller.value
  const row = box?.querySelector<HTMLElement>('[aria-current="page"]')
  if (!box || !row) return
  const top = row.getBoundingClientRect().top - box.getBoundingClientRect().top + box.scrollTop
  if (top < box.scrollTop) box.scrollTop = top - 8
  else if (top + row.offsetHeight > box.scrollTop + box.clientHeight) box.scrollTop = top + row.offsetHeight - box.clientHeight + 8
}
onMounted(revealSelected)
watch(() => [props.selected, props.view], revealSelected)

// Overview and History sit above the sections.
const topItems = computed(() => [
  { key: 'overview', label: 'Overview', icon: LayoutDashboard, href: slotHref(props.blueprint.id), on: props.selected === null && props.view !== 'history' },
  ...(props.showHistory
    ? [{ key: 'history', label: 'History', icon: History, href: `/blueprints/${props.blueprint.id}/history`, on: props.view === 'history' }]
    : []),
])

// A filled slot reads as the block it holds; an empty one by its label.
function title(item: Item) {
  return item.blockSlug ? item.blockName || item.label : item.label
}

// Adding goes straight to the new blank slot, where a block is picked.
const adding = ref<string | null>(null)
function add(type: string) {
  adding.value = null
  router.post(`/blueprints/${props.blueprint.id}/slots`, { type, label: '', required: true })
}

// Collapsed sections, remembered per browser.
const storageKey = 'kitmaker.outline.collapsed'
const collapsed = ref<Record<string, boolean>>(load())
function load(): Record<string, boolean> {
  try {
    return JSON.parse(localStorage.getItem(storageKey) ?? '{}')
  } catch {
    return {}
  }
}
function toggle(key: string) {
  collapsed.value = { ...collapsed.value, [key]: !collapsed.value[key] }
  try {
    localStorage.setItem(storageKey, JSON.stringify(collapsed.value))
  } catch {
    /* storage may be unavailable */
  }
}

const filter = ref<'all' | 'needs' | 'changed'>('all')
const needs = computed(
  () => props.sections.flatMap((s) => s.items).filter((i) => needsYou(i.status)).length,
)
const needsYou = (status?: string) => status === 'blank' || status === 'problem' || status === 'conflict'

function dimmed(status?: string, origin?: string) {
  if (filter.value === 'needs') return !needsYou(status)
  if (filter.value === 'changed') return !(origin === 'modified' || origin === 'over' || origin === 'custom')
  return false
}

const filters = computed(() => [
  { key: 'all' as const, label: 'All' },
  { key: 'needs' as const, label: `Needs you · ${needs.value}` },
  { key: 'changed' as const, label: 'Changed here' },
])
</script>

<template>
  <nav aria-label="Blueprint outline" class="flex w-[272px] flex-col gap-3 rounded-xl border bg-background px-3 py-3.5 text-[13px] md:max-h-[calc(100vh-6.5rem)]">
    <div class="flex flex-col gap-1 px-1">
      <div class="flex items-center justify-between gap-2">
        <Link :href="slotHref(blueprint.id)" class="truncate text-[15px] font-semibold">{{ blueprint.slug }}</Link>
        <span class="ml-auto rounded-full bg-amber-50 px-2 py-0.5 text-[11px] font-semibold text-amber-700">draft</span>
      </div>
      <span class="text-xs text-muted-foreground">{{ blueprint.kind }} · {{ blueprint.team }}</span>
    </div>

    <div role="group" aria-label="Filter outline" class="flex flex-wrap gap-1">
      <button
        v-for="f in filters"
        :key="f.key"
        type="button"
        class="rounded-full border px-2 py-1 text-[11.5px]"
        :class="filter === f.key ? 'border-primary bg-primary text-primary-foreground font-semibold' : 'bg-background text-slate-700'"
        :aria-pressed="filter === f.key"
        @click="filter = f.key"
      >
        {{ f.label }}
      </button>
    </div>

    <!-- Everything below the header scrolls on its own. The scroller reaches
         56px past the nav's right edge (its padding plus the 44px gap) so the
         selection bridge isn't clipped. That strip lets clicks through to the
         frame, and the scrollbar is hidden (it would sit over the frame). -->
    <div
      ref="scroller"
      class="pointer-events-none -mr-[56px] flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto overscroll-contain pr-[56px] [scrollbar-width:none] *:pointer-events-auto [&::-webkit-scrollbar]:hidden"
    >
      <div class="flex flex-col gap-px">
        <Link
          v-for="it in topItems"
          :key="it.key"
          :href="it.href"
          class="relative flex items-center gap-2 rounded-md px-1.5 py-1.5"
          :class="it.on ? 'rounded-r-none bg-primary font-semibold text-primary-foreground' : 'hover:bg-muted'"
          :aria-current="it.on ? 'page' : undefined"
        >
          <component :is="it.icon" class="size-3.5" :class="it.on ? '' : 'text-primary'" aria-hidden="true" />
          <span class="flex-1">{{ it.label }}</span>
          <span v-if="it.on" aria-hidden="true" class="absolute inset-y-0 -right-[44px] z-10 hidden w-[44px] bg-primary md:block" />
        </Link>
      </div>

      <!-- The tree: every section, its slots (with the block each holds), and
           an add row per block type the section takes. -->
      <div v-for="section in sections" :key="section.key" class="flex flex-col">
        <button
          type="button"
          class="flex items-center gap-1 rounded px-1 pb-0.5 pt-1.5 text-left text-[10.5px] font-semibold tracking-wider text-slate-500 uppercase hover:text-slate-700"
          :aria-expanded="!collapsed[section.key]"
          @click="toggle(section.key)"
        >
          <ChevronRight class="size-3 transition-transform" :class="collapsed[section.key] ? '' : 'rotate-90'" aria-hidden="true" />
          <span class="flex-1">{{ section.label }}</span>
          <span v-if="section.items.length" class="font-medium normal-case tracking-normal text-slate-400">{{ section.items.length }}</span>
        </button>
        <div v-show="!collapsed[section.key]" class="ml-[11px] flex flex-col gap-px border-l border-slate-200 pl-1.5">
          <div v-for="item in section.items" :key="item.key" class="relative">
            <Link
              :href="slotHref(blueprint.id, item.key)"
              preserve-scroll
              class="relative flex items-center gap-2 rounded-md px-1.5 py-1 transition-opacity"
              :class="[
                selected === item.key ? 'rounded-r-none bg-primary text-primary-foreground' : 'hover:bg-muted',
                dimmed(item.status, item.origin) && selected !== item.key ? 'opacity-45' : '',
              ]"
              :aria-current="selected === item.key ? 'page' : undefined"
            >
              <span
                class="size-2 shrink-0 rounded-[2px]"
                :style="{ background: selected === item.key ? '#fff' : (sectionStyle[item.section]?.fg ?? '#64748B') }"
              />
              <span class="min-w-0 flex-1 truncate" :class="[selected === item.key ? 'font-semibold' : '', item.blockSlug ? '' : 'italic']">
                {{ title(item) }}
              </span>
              <span class="flex shrink-0 items-center gap-1">
                <span
                  class="inline-flex items-center gap-0.5 text-[10.5px] font-medium"
                  :style="{ color: selected === item.key ? '#fff' : originStyle[item.origin].fg }"
                  :title="originStyle[item.origin].label"
                >
                  <OriginIcon :origin="item.origin" class="size-3.5" />
                  <template v-if="item.origin === 'modified'">{{ item.edits }}</template>
                  <span v-if="dirtyKey === item.key" class="ml-0.5 inline-flex" title="Unsaved edits">
                    <OriginIcon origin="unsaved" class="size-3.5" :class="selected === item.key ? 'text-white' : 'text-amber-600'" />
                  </span>
                </span>
                <span
                  v-if="item.status === 'blank'"
                  class="rounded-full px-1.5 py-px text-[10px] font-semibold"
                  :class="selected === item.key ? 'bg-white text-amber-700' : 'bg-amber-50 text-amber-700'"
                  >blank</span
                >
                <span
                  v-else-if="item.status === 'problem'"
                  class="rounded-full px-1.5 py-px text-[10px] font-semibold"
                  :class="selected === item.key ? 'bg-white text-red-700' : 'bg-red-50 text-red-700'"
                  >fix</span
                >
                <span
                  v-else-if="item.status === 'conflict'"
                  class="rounded-full px-1.5 py-px text-[10px] font-semibold"
                  :class="selected === item.key ? 'bg-white text-amber-700' : 'bg-amber-50 text-amber-700'"
                  title="Works against another block in this blueprint"
                  >conflict</span
                >
                <span
                  v-else-if="item.latest && item.version && item.latest > item.version"
                  class="rounded-full px-1.5 py-px text-[10px] font-semibold"
                  :class="selected === item.key ? 'bg-white text-green-700' : 'bg-green-50 text-green-700'"
                  >v{{ item.latest }}</span
                >
              </span>
              <span v-if="selected === item.key" aria-hidden="true" class="absolute inset-y-0 -right-[44px] z-10 hidden w-[44px] bg-primary md:block" />
            </Link>
          </div>

          <template v-if="canEdit && section.types.length">
            <!-- One type: add it straight away. Several: "Add…" opens the list
                 in place (inside the outline, so it scrolls with it). -->
            <button
              v-if="section.types.length === 1"
              type="button"
              class="flex items-center gap-1.5 rounded-md px-1.5 py-1 text-left text-[12.5px] text-primary hover:bg-primary/5"
              @click="add(section.types[0]!.key)"
            >
              <Plus class="size-3.5" /> Add {{ section.types[0]!.label.toLowerCase() }}
            </button>
            <template v-else>
              <button
                type="button"
                class="flex items-center gap-1.5 rounded-md px-1.5 py-1 text-left text-[12.5px] text-primary hover:bg-primary/5"
                :aria-expanded="adding === section.key"
                @click="adding = adding === section.key ? null : section.key"
              >
                <Plus class="size-3.5" /> Add…
                <ChevronRight class="ml-auto size-3 transition-transform" :class="adding === section.key ? 'rotate-90' : ''" aria-hidden="true" />
              </button>
              <div v-if="adding === section.key" class="mb-1 ml-1.5 flex flex-col gap-px border-l border-primary/20 pl-1.5">
                <button
                  v-for="t in section.types"
                  :key="t.key"
                  type="button"
                  class="flex flex-col rounded-md px-1.5 py-1 text-left hover:bg-primary/5"
                  :title="t.description"
                  @click="add(t.key)"
                >
                  <span class="text-[12.5px] font-medium text-primary">{{ t.label }}</span>
                  <span class="line-clamp-2 text-[11px] leading-snug text-slate-500">{{ t.description }}</span>
                </button>
              </div>
            </template>
          </template>
          <p v-if="!section.types.length && !section.items.length" class="px-1.5 py-1 text-[11.5px] text-slate-400">Nothing to add here yet</p>
        </div>
      </div>

      <div class="grid grid-cols-2 gap-x-2 gap-y-1.5 border-t px-1 pt-2.5 text-[11px] text-slate-600">
        <span v-for="o in (['inherited', 'modified', 'custom', 'blank'] as const)" :key="o" class="inline-flex items-center gap-1.5">
          <OriginIcon :origin="o" class="size-3" :style="{ color: originStyle[o].fg }" />
          {{ originStyle[o].label }}
        </span>
        <span class="inline-flex items-center gap-1.5">
          <OriginIcon origin="unsaved" class="size-3 text-amber-600" />
          Unsaved
        </span>
      </div>
    </div>
  </nav>
</template>
