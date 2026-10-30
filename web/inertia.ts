import { createApp, h, type DefineComponent } from 'vue'
import { createInertiaApp } from '@inertiajs/vue3'
import type { Page } from '@inertiajs/core'

// startApp mounts the Inertia app: from the page the server rendered into
// #app, or from `page` when the Cloudflare edition's engine built it.
export function startApp(page?: Page) {
  return createInertiaApp({
    page,
    title: (title) => (title ? `${title} · Kit Maker` : 'Kit Maker'),
    resolve: (name) => {
      const pages = import.meta.glob<DefineComponent>('./pages/**/*.vue', { eager: true })
      const found = pages[`./pages/${name}.vue`]
      if (!found) throw new Error(`Unknown page: ${name}`)
      return found
    },
    setup({ el, App, props, plugin }) {
      createApp({ render: () => h(App, props) })
        .use(plugin)
        .mount(el)
    },
  })
}
