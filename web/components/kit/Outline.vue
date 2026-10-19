<script setup lang="ts">
import { computed, ref } from 'vue'
import { Link, router } from '@inertiajs/vue3'
import { ChevronRight, LayoutDashboard, PanelLeftClose, Plus, Trash2 } from '@lucide/vue'
import OriginIcon from './OriginIcon.vue'
import { originStyle, sectionStyle, slotHref, type Item, type KitInfo, type Section } from '@/lib/kit'

const props = defineProps<{
  kit: KitInfo
  sections: Section[]
  selected: string | null
  canEdit?: boolean
  // floating: shown as a hover flyout while the sidebar is hidden, so the
  // selection bridge (which joins the docked outline to the frame) is off.
  floating?: boolean
}>()
const emit = defineEmits<{ hide: [] }>()

// A filled slot reads as the block it holds; an empty one by its label.
function title(item: Item) {
  return item.blockSlug ? item.blockName || item.label : item.label
}

// Adding goes straight to the new blank slot, where a block is picked.
function add(type: string) {
  router.post(`/kits/${props.kit.id}/slots`, { type, label: '', required: true })
}
function remove(key: string, label: string) {
  if (confirm(`Remove "${label}" from this kit?`)) router.delete(slotHref(props.kit.id, key), { preserveScroll: true })
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
  () => props.sections.flatMap((s) => s.items).filter((i) => i.status === 'blank' || i.status === 'problem').length,
)

function dimmed(status?: string, origin?: string) {
  if (filter.value === 'needs') return !(status === 'blank' || status === 'problem')
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
  <nav aria-label="Kit outline" class="flex w-[272px] flex-col gap-3 rounded-xl border bg-background px-3 py-3.5 text-[13px]">
    <div class="flex flex-col gap-1 px-1">
      <div class="flex items-center justify-between gap-2">
        <Link :href="slotHref(kit.id)" class="truncate text-[15px] font-semibold">{{ kit.slug }}</Link>
        <span class="ml-auto rounded-full bg-amber-50 px-2 py-0.5 text-[11px] font-semibold text-amber-700">draft</span>
        <button v-if="!floating" type="button" aria-label="Hide outline" title="Hide outline" class="rounded p-1 text-slate-400 hover:bg-muted hover:text-slate-700" @click="emit('hide')">
          <PanelLeftClose class="size-4" />
        </button>
      </div>
      <span class="text-xs text-muted-foreground">{{ kit.kind }} · {{ kit.team }}</span>
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

    <div class="flex flex-col gap-px">
      <Link
        :href="slotHref(kit.id)"
        class="relative flex items-center gap-2 rounded-md px-1.5 py-1.5"
        :class="selected === null ? 'rounded-r-none bg-primary font-semibold text-primary-foreground' : 'hover:bg-muted'"
        :aria-current="selected === null ? 'page' : undefined"
      >
        <LayoutDashboard class="size-3.5" :class="selected === null ? '' : 'text-primary'" aria-hidden="true" />
        <span class="flex-1">Overview</span>
        <span v-if="selected === null && !floating" aria-hidden="true" class="absolute inset-y-0 -right-[44px] z-10 hidden w-[44px] bg-primary lg:block" />
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
        <div v-for="item in section.items" :key="item.key" class="group relative">
          <Link
            :href="slotHref(kit.id, item.key)"
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
            <span class="flex shrink-0 items-center gap-1" :class="canEdit ? 'group-hover:invisible' : ''">
              <span
                class="inline-flex items-center gap-0.5 text-[10.5px] font-medium"
                :style="{ color: selected === item.key ? '#fff' : originStyle[item.origin].fg }"
                :title="originStyle[item.origin].label"
              >
                <OriginIcon :origin="item.origin" class="size-3.5" />
                <template v-if="item.origin === 'modified'">{{ item.edits }}</template>
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
                v-else-if="item.latest && item.version && item.latest > item.version"
                class="rounded-full px-1.5 py-px text-[10px] font-semibold"
                :class="selected === item.key ? 'bg-white text-green-700' : 'bg-green-50 text-green-700'"
                >v{{ item.latest }}</span
              >
            </span>
            <span v-if="selected === item.key && !floating" aria-hidden="true" class="absolute inset-y-0 -right-[44px] z-10 hidden w-[44px] bg-primary lg:block" />
          </Link>
          <button
            v-if="canEdit"
            type="button"
            :aria-label="`Remove ${title(item)}`"
            title="Remove from this kit"
            class="absolute top-1/2 right-1 z-20 hidden -translate-y-1/2 rounded p-1 group-hover:block focus-visible:block"
            :class="selected === item.key ? 'text-white hover:bg-white/20' : 'text-slate-400 hover:bg-red-50 hover:text-red-600'"
            @click="remove(item.key, title(item))"
          >
            <Trash2 class="size-3.5" />
          </button>
        </div>

        <template v-if="canEdit">
          <button
            v-for="t in section.types"
            :key="t.key"
            type="button"
            class="flex items-center gap-1.5 rounded-md px-1.5 py-1 text-left text-[12.5px] text-primary hover:bg-primary/5"
            @click="add(t.key)"
          >
            <Plus class="size-3.5" /> Add {{ t.label.toLowerCase() }}
          </button>
        </template>
        <p v-if="!section.types.length && !section.items.length" class="px-1.5 py-1 text-[11.5px] text-slate-400">Nothing to add here yet</p>
      </div>
    </div>

    <div class="grid grid-cols-2 gap-x-2 gap-y-1.5 border-t px-1 pt-2.5 text-[11px] text-slate-600">
      <span v-for="o in (['inherited', 'modified', 'custom', 'blank'] as const)" :key="o" class="inline-flex items-center gap-1.5">
        <OriginIcon :origin="o" class="size-3" :style="{ color: originStyle[o].fg }" />
        {{ originStyle[o].label }}
      </span>
    </div>
  </nav>
</template>
