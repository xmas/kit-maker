<script setup lang="ts">
import { CircleDashed, Dot, Layers, Link2, Pencil } from '@lucide/vue'
import type { Origin } from '@/lib/kit'

// Linked = link; linked with saved edits = link with an amber dot (still
// linked: library updates flow in under the edits); unsaved = pencil, used
// for live, not-yet-saved editing only.
defineProps<{ origin: Origin | 'unsaved'; class?: string }>()
</script>

<template>
  <Link2 v-if="origin === 'inherited'" :class="$props.class" aria-hidden="true" />
  <span v-else-if="origin === 'modified'" class="relative inline-flex" aria-hidden="true">
    <Link2 :class="$props.class" />
    <span class="absolute -top-0.5 -right-0.5 size-[7px] rounded-full bg-amber-500 ring-[1.5px] ring-white" />
  </span>
  <Pencil v-else-if="origin === 'unsaved'" :class="$props.class" aria-hidden="true" />
  <Dot v-else-if="origin === 'custom'" :class="$props.class" aria-hidden="true" />
  <Layers v-else-if="origin === 'sub' || origin === 'over'" :class="$props.class" aria-hidden="true" />
  <CircleDashed v-else :class="$props.class" aria-hidden="true" />
</template>
