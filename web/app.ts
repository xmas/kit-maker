import { createApp, h, type DefineComponent } from 'vue'
import { createInertiaApp } from '@inertiajs/vue3'

createInertiaApp({
  title: (title) => (title ? `${title} · Kit Maker` : 'Kit Maker'),
  resolve: (name) => {
    const pages = import.meta.glob<DefineComponent>('./pages/**/*.vue', { eager: true })
    const page = pages[`./pages/${name}.vue`]
    if (!page) throw new Error(`Unknown page: ${name}`)
    return page
  },
  setup({ el, App, props, plugin }) {
    createApp({ render: () => h(App, props) })
      .use(plugin)
      .mount(el)
  },
})
