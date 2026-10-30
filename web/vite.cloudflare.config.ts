import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// The Cloudflare edition (cloudflare/): a static site whose page runs the
// engine (kitmaker.wasm) in place of the server. `make cf-build` builds the
// engine and starter content into cloudflare/public first.
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  base: '/',
  publicDir: fileURLToPath(new URL('../cloudflare/public', import.meta.url)),
  resolve: {
    alias: { '@': fileURLToPath(new URL('.', import.meta.url)) },
  },
  build: {
    outDir: fileURLToPath(new URL('../cloudflare/dist', import.meta.url)),
    emptyOutDir: true,
  },
})
