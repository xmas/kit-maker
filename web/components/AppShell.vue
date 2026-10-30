<script setup lang="ts">
import { computed } from 'vue'
import { Link, router, usePage } from '@inertiajs/vue3'
import { Boxes, LogOut } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import ConfirmDialog from '@/components/ConfirmDialog.vue'

interface AuthUser {
  id: string
  email: string
  displayName: string
}

// mode is set by the Cloudflare edition: "local" keeps everything in this
// browser, so there's nobody to sign out.
const page = usePage<{ auth: { user: AuthUser | null; mode?: 'shared' | 'local' } }>()
const user = computed(() => page.props.auth?.user)
const local = computed(() => page.props.auth?.mode === 'local')
const initials = computed(() => {
  const name = user.value?.displayName || user.value?.email || '?'
  return name
    .split(/[\s@.]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase())
    .join('')
})

const nav = [
  { href: '/blueprints', label: 'Blueprints' },
  { href: '/blocks', label: 'Blocks' },
  { href: '/projects', label: 'Projects', disabled: true },
  { href: '/sandboxes', label: 'Sandboxes', disabled: true },
]

function signOut() {
  router.post('/logout')
}
</script>

<template>
  <div class="flex min-h-screen flex-col bg-[#F6F7F9]">
    <header class="sticky top-0 z-50 border-b bg-background">
      <div class="flex h-14 items-center gap-6 px-6">
        <Link href="/blueprints" class="flex shrink-0 items-center gap-2 font-semibold whitespace-nowrap">
          <span class="flex size-7 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Boxes class="size-4" />
          </span>
          kit maker
        </Link>
        <nav aria-label="Primary" class="flex gap-1 text-sm">
          <template v-for="item in nav" :key="item.href">
            <Link
              v-if="!item.disabled"
              :href="item.href"
              class="rounded-md px-3 py-1.5 font-medium"
              :class="$page.url.startsWith(item.href) ? 'bg-muted text-foreground' : 'text-muted-foreground hover:text-foreground'"
            >
              {{ item.label }}
            </Link>
            <span v-else class="cursor-not-allowed rounded-md px-3 py-1.5 text-muted-foreground/60" title="Coming soon">
              {{ item.label }}
            </span>
          </template>
        </nav>
        <div class="flex-1" />
        <span
          v-if="local"
          class="hidden rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground md:inline"
          title="This copy of Kit Maker has no backend: your blueprints and blocks are saved in this browser only."
        >
          Saved in this browser
        </span>
        <div v-else-if="user" class="flex items-center gap-3">
          <span class="hidden text-sm text-muted-foreground sm:inline">{{ user.email }}</span>
          <span
            class="flex size-8 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary"
            :title="user.displayName || user.email"
          >
            {{ initials }}
          </span>
          <Button variant="ghost" size="icon-sm" aria-label="Sign out" title="Sign out" @click="signOut">
            <LogOut />
          </Button>
        </div>
      </div>
    </header>
    <slot />
    <ConfirmDialog />
  </div>
</template>
