<script setup lang="ts">
import { ArrowUp, Link2 } from '@lucide/vue'
import OriginIcon from './OriginIcon.vue'
import { originStyle, sectionStyle, type Origin } from '@/lib/kit'

// The blue frame around the main pane. Its header repeats the selected
// outline row (type, name, origin, status), so the selection visibly runs
// from the outline into the content (DESIGN.md §2.6).
defineProps<{
  section: string
  eyebrow: string
  title: string
  origin?: Origin
  edits?: number
  version?: number
  latest?: number
  usedBy?: number
  blockSlug?: string
}>()
</script>

<template>
  <section class="min-w-0 flex-1 rounded-[14px] border-2 border-primary bg-[#FAFBFF] shadow-[0_8px_24px_rgba(29,99,237,0.08)]">
    <header class="flex flex-wrap items-center gap-x-3.5 gap-y-2.5 rounded-t-[11px] bg-primary px-4.5 py-3.5 text-primary-foreground">
      <span
        class="flex size-9 shrink-0 items-center justify-center rounded-[9px] bg-white text-sm font-bold"
        :style="{ color: sectionStyle[section]?.fg ?? '#4338CA' }"
      >
        <slot name="icon">{{ (sectionStyle[section]?.label ?? eyebrow).slice(0, 1) }}</slot>
      </span>
      <div class="flex min-w-0 flex-col gap-0.5">
        <span class="text-[11px] font-semibold tracking-wider uppercase opacity-90">{{ eyebrow }}</span>
        <h1 class="text-xl font-semibold">{{ title }}</h1>
      </div>
      <div class="flex flex-wrap gap-1.5">
        <span
          v-if="origin && origin !== 'blank'"
          class="inline-flex items-center gap-1.5 rounded-full bg-white px-2.5 py-1 text-xs font-semibold"
          :style="{ color: originStyle[origin].fg }"
        >
          <OriginIcon :origin="origin" class="size-3" />
          {{ originStyle[origin].label }}<template v-if="origin === 'modified' && edits"> · {{ edits }} {{ edits === 1 ? 'edit' : 'edits' }}</template>
        </span>
        <span v-if="blockSlug && origin === 'custom'" class="inline-flex items-center gap-1.5 rounded-full bg-white px-2.5 py-1 text-xs font-semibold text-slate-700">
          Only in this kit · v{{ version }}
        </span>
        <span v-else-if="blockSlug" class="inline-flex items-center gap-1.5 rounded-full bg-white px-2.5 py-1 text-xs font-semibold text-slate-700">
          <Link2 class="size-3" /> Library v{{ version }}<template v-if="usedBy && usedBy > 1"> · used by {{ usedBy }} kits</template>
        </span>
        <span
          v-if="latest && version && latest > version"
          class="inline-flex items-center gap-1.5 rounded-full bg-white px-2.5 py-1 text-xs font-semibold text-green-700"
        >
          <ArrowUp class="size-3" /> v{{ latest }} available
        </span>
        <slot name="chips" />
      </div>
      <div class="ml-auto flex flex-wrap gap-2">
        <slot name="actions" />
      </div>
    </header>
    <div class="@container flex flex-col gap-4 p-4">
      <slot />
    </div>
  </section>
</template>
