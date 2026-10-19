import { fileURLToPath, URL } from 'node:url'
import fs from 'node:fs'
import path from 'node:path'
import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// While `vite` (dev) runs, write its URL to web/hot. The Go server sees the
// file and loads assets from Vite with hot reload instead of the embedded
// build (gonertia's Vite integration).
function hotFile(): Plugin {
  const file = path.resolve(import.meta.dirname, 'hot')
  const remove = () => fs.rmSync(file, { force: true })
  return {
    name: 'kitmaker-hot-file',
    apply: 'serve',
    configureServer(server) {
      server.httpServer?.once('listening', () => {
        const url = server.resolvedUrls?.local[0] ?? 'http://localhost:5173/'
        fs.writeFileSync(file, url.replace(/\/$/, ''))
      })
      server.httpServer?.once('close', remove)
      for (const sig of ['SIGINT', 'SIGTERM', 'exit'] as const) {
        process.once(sig, () => {
          remove()
          if (sig !== 'exit') process.exit()
        })
      }
    },
  }
}

export default defineConfig({
  plugins: [vue(), tailwindcss(), hotFile()],
  base: '/build/',
  resolve: {
    alias: { '@': fileURLToPath(new URL('.', import.meta.url)) },
  },
  server: {
    port: 5173,
    strictPort: true,
    // Pages are served by Go on :8080 and load modules from here.
    cors: { origin: /^https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/ },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    manifest: true,
    // styles.css is its own entry: gonertia drops CSS that only appears in
    // a JS entry's `css` list, and it keeps hot reload working for styles.
    rollupOptions: { input: ['app.ts', 'styles.css'] },
  },
})
