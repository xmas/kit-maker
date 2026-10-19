<script setup lang="ts">
import { Head, Link } from '@inertiajs/vue3'
import { ShieldAlert } from '@lucide/vue'
import AppShell from '@/components/AppShell.vue'

defineProps<{ status: number; title: string; message: string }>()
</script>

<template>
  <Head :title="title" />
  <AppShell>
    <main class="mx-auto flex max-w-lg flex-1 flex-col items-center justify-center gap-4 px-4 py-24 text-center">
      <span class="flex size-12 items-center justify-center rounded-full bg-amber-50 text-amber-700">
        <ShieldAlert class="size-6" />
      </span>
      <div class="space-y-1">
        <p class="text-sm font-medium text-muted-foreground">{{ status }}</p>
        <h1 class="text-xl font-semibold">{{ title }}</h1>
        <p class="text-sm text-muted-foreground">{{ message }}</p>
      </div>
      <Link href="/kits" class="text-sm font-medium text-primary hover:underline">Back to kits</Link>
    </main>
  </AppShell>
</template>
