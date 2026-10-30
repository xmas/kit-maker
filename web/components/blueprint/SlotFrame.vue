<script setup lang="ts">
import { ref } from 'vue'
import { ChevronDown } from '@lucide/vue'
import { useDismiss } from '@/lib/useDismiss'
import OriginIcon from './OriginIcon.vue'
import { originStyle, sectionStyle, type Origin } from '@/lib/blueprint'

// The blue frame around the main pane. Its header repeats the selected
// outline row (type, name, origin, status), so the selection visibly runs
// from the outline into the content (DESIGN.md §2.6).
const statusOpen = ref(false)
const statusRoot = ref<HTMLElement | null>(null)
useDismiss(statusRoot, statusOpen)

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
  <section class="flex min-w-0 flex-1 flex-col rounded-[14px] border-2 border-primary bg-[#FAFBFF] shadow-[0_8px_24px_rgba(29,99,237,0.08)]">
    <!-- One line, everything centred on it: tile · SECTION / Title · status · actions.
         A narrow frame (the outline always stays open) drops the section
         label and the chip's details first; the title truncates. -->
    <header class="@container/head flex min-h-14 items-center gap-3 rounded-t-[11px] bg-primary px-4 py-2.5 text-primary-foreground">
      <span
        class="flex size-8 shrink-0 items-center justify-center rounded-lg bg-white text-sm font-bold"
        :style="{ color: sectionStyle[section]?.fg ?? '#4338CA' }"
      >
        <slot name="icon">{{ (sectionStyle[section]?.label ?? eyebrow).slice(0, 1) }}</slot>
      </span>
      <div class="flex min-w-0 flex-1 items-center gap-2.5">
        <span class="hidden shrink-0 text-[11px] font-semibold tracking-wider uppercase opacity-80 @[680px]/head:inline">{{ eyebrow }}</span>
        <span class="hidden shrink-0 opacity-50 @[680px]/head:inline" aria-hidden="true">/</span>
        <h1 class="min-w-0 truncate text-lg leading-tight font-semibold" :title="title">{{ title }}</h1>
        <!-- One status chip. When the page passes a "status" slot, the chip
             opens it as a popover (details and actions about the link). -->
        <div v-if="origin && origin !== 'blank'" ref="statusRoot" class="relative shrink-0">
          <component
            :is="$slots.status ? 'button' : 'span'"
            :type="$slots.status ? 'button' : undefined"
            class="inline-flex h-6 items-center gap-1.5 rounded-full bg-white px-2.5 text-xs font-semibold whitespace-nowrap"
            :class="$slots.status ? 'hover:bg-white/90' : ''"
            :style="{ color: originStyle[origin].fg }"
            :title="usedBy && usedBy > 1 ? `Used by ${usedBy} blueprints` : undefined"
            :aria-expanded="$slots.status ? statusOpen : undefined"
            @click="$slots.status && (statusOpen = !statusOpen)"
          >
            <OriginIcon :origin="origin" class="size-3" />
            {{ originStyle[origin].label
            }}<span class="hidden @[560px]/head:inline"
              ><template v-if="version"> · v{{ version }}</template
              ><template v-if="origin === 'modified' && edits"> · {{ edits }} {{ edits === 1 ? 'edit' : 'edits' }}</template></span
            >
            <ChevronDown v-if="$slots.status" class="-mr-0.5 size-3 opacity-60" aria-hidden="true" />
          </component>
          <div
            v-if="$slots.status && statusOpen"
            role="dialog"
            aria-label="About this block"
            class="absolute top-full left-0 z-40 mt-2 w-[min(380px,80vw)] rounded-xl border bg-background p-4 text-foreground shadow-[0_16px_40px_rgba(15,23,42,0.2)]"
          >
            <slot name="status" :close="() => (statusOpen = false)" />
          </div>
        </div>
        <slot name="chips" />
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <slot name="actions" />
      </div>
    </header>
    <div class="@container flex flex-1 flex-col gap-4 p-4">
      <slot />
    </div>
  </section>
</template>
