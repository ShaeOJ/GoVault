import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { fileURLToPath, URL } from 'node:url'

// Headless web build of the SAME Svelte frontend. The only difference from the
// desktop build is transport: the Wails generated bindings and runtime are
// aliased to HTTP/SSE shims, so every App call becomes an RPC and every EventsOn
// subscribes to the SSE stream. Output goes into internal/webui/dist for go:embed.
const shim = (p: string) => fileURLToPath(new URL(p, import.meta.url))

export default defineConfig({
  plugins: [svelte()],
  resolve: {
    alias: [
      { find: /.*wailsjs\/go\/appcore\/App(\.js)?$/, replacement: shim('./src/web-shim/App.js') },
      { find: /.*wailsjs\/runtime\/runtime(\.js)?$/, replacement: shim('./src/web-shim/runtime.js') },
    ],
  },
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
})
