<script setup lang="ts">
import { computed, ref } from 'vue'
import { Head, Link } from '@inertiajs/vue3'
import { Play, Plus } from '@lucide/vue'
import RunDialog from '@/components/blueprint/RunDialog.vue'
import AppShell from '@/components/AppShell.vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

interface BlueprintRow {
  id: string
  slug: string
  displayName: string
  kind: 'workload' | 'mixin' | 'set' | 'environment'
  team: string
  slots: number
  blanks: number
}

const props = defineProps<{ blueprints: BlueprintRow[] }>()
const empty = computed(() => props.blueprints.length === 0)
// The blueprint whose Run dialog is open.
const running = ref<BlueprintRow | null>(null)
</script>

<template>
  <Head title="Blueprints" />
  <AppShell>

    <main class="mx-auto w-full max-w-6xl px-4 py-8">
      <div class="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold tracking-tight">Blueprints</h1>
          <p class="text-sm text-muted-foreground">
            A blueprint is the design for a Docker Sandboxes kit: built from blocks, versioned, and downloaded to run sandboxes.
          </p>
        </div>
        <Button as-child>
          <Link href="/blueprints/new"><Plus /> New blueprint</Link>
        </Button>
      </div>

      <Card v-if="empty" class="items-center py-12 text-center">
        <CardContent class="space-y-2">
          <p class="font-medium">No blueprints yet</p>
          <p class="text-sm text-muted-foreground">Start one from a goal. It opens as an outline with blanks to fill.</p>
          <Button as-child class="mt-2"><Link href="/blueprints/new"><Plus /> New blueprint</Link></Button>
        </CardContent>
      </Card>

      <div v-else class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Link v-for="blueprint in blueprints" :key="blueprint.id" :href="`/blueprints/${blueprint.id}`" class="rounded-xl focus-visible:outline-2 focus-visible:outline-primary">
        <Card class="h-full transition-colors hover:border-primary">
          <CardHeader>
            <div class="flex items-center justify-between gap-2">
              <CardTitle>{{ blueprint.displayName }}</CardTitle>
              <Badge variant="secondary">{{ blueprint.kind }}</Badge>
            </div>
            <CardDescription>{{ blueprint.team }} · {{ blueprint.slug }}</CardDescription>
          </CardHeader>
          <CardContent class="flex items-center justify-between text-sm">
            <span class="text-muted-foreground">{{ blueprint.slots }} slots</span>
            <span v-if="blueprint.blanks > 0" class="font-medium text-primary">
              {{ blueprint.blanks }} {{ blueprint.blanks === 1 ? 'blank' : 'blanks' }} left
            </span>
            <span v-else class="font-medium">Ready</span>
            <button
              type="button"
              class="ml-3 inline-flex h-7 items-center gap-1 rounded-md border px-2 text-xs font-medium text-primary hover:bg-primary/5"
              :aria-label="`Run ${blueprint.displayName}`"
              @click.prevent.stop="running = blueprint"
            >
              <Play class="size-3" /> Run
            </button>
          </CardContent>
        </Card>
        </Link>
      </div>

    </main>
    <RunDialog v-if="running" :open="!!running" :blueprint-id="running.id" :name="running.displayName" @update:open="(o) => !o && (running = null)" />
  </AppShell>
</template>
