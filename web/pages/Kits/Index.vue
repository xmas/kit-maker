<script setup lang="ts">
import { computed } from 'vue'
import { Head, Link } from '@inertiajs/vue3'
import { Plus } from '@lucide/vue'
import AppShell from '@/components/AppShell.vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

interface KitRow {
  id: string
  slug: string
  displayName: string
  kind: 'workload' | 'mixin' | 'set' | 'environment'
  team: string
  slots: number
  blanks: number
}

const props = defineProps<{ kits: KitRow[] }>()
const empty = computed(() => props.kits.length === 0)
</script>

<template>
  <Head title="Kits" />
  <AppShell>

    <main class="mx-auto w-full max-w-6xl px-4 py-8">
      <div class="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold tracking-tight">Kits</h1>
          <p class="text-sm text-muted-foreground">
            Start from a goal. The kit shows up as an outline with blanks to fill.
          </p>
        </div>
        <Button as-child>
          <Link href="/kits/new"><Plus /> New kit</Link>
        </Button>
      </div>

      <Card v-if="empty" class="items-center py-12 text-center">
        <CardContent class="space-y-2">
          <p class="font-medium">No kits yet</p>
          <p class="text-sm text-muted-foreground">Start one from a goal.</p>
          <Button as-child class="mt-2"><Link href="/kits/new"><Plus /> New kit</Link></Button>
        </CardContent>
      </Card>

      <div v-else class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Link v-for="kit in kits" :key="kit.id" :href="`/kits/${kit.id}`" class="rounded-xl focus-visible:outline-2 focus-visible:outline-primary">
        <Card class="h-full transition-colors hover:border-primary">
          <CardHeader>
            <div class="flex items-center justify-between gap-2">
              <CardTitle>{{ kit.displayName }}</CardTitle>
              <Badge variant="secondary">{{ kit.kind }}</Badge>
            </div>
            <CardDescription>{{ kit.team }} · {{ kit.slug }}</CardDescription>
          </CardHeader>
          <CardContent class="flex items-center justify-between text-sm">
            <span class="text-muted-foreground">{{ kit.slots }} slots</span>
            <span v-if="kit.blanks > 0" class="font-medium text-primary">
              {{ kit.blanks }} {{ kit.blanks === 1 ? 'blank' : 'blanks' }} left
            </span>
            <span v-else class="font-medium">Ready</span>
          </CardContent>
        </Card>
        </Link>
      </div>

    </main>
  </AppShell>
</template>
