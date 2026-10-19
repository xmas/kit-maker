<script setup lang="ts">
import { CircleDashed, Dot, Layers, Link2, Pencil } from '@lucide/vue'
import type { Origin } from '@/lib/kit'

defineProps<{ origin: Origin; class?: string }>()
</script>

<template>
  <Link2 v-if="origin === 'inherited'" :class="$props.class" aria-hidden="true" />
  <Pencil v-else-if="origin === 'modified'" :class="$props.class" aria-hidden="true" />
  <Dot v-else-if="origin === 'custom'" :class="$props.class" aria-hidden="true" />
  <Layers v-else-if="origin === 'sub' || origin === 'over'" :class="$props.class" aria-hidden="true" />
  <CircleDashed v-else :class="$props.class" aria-hidden="true" />
</template>
