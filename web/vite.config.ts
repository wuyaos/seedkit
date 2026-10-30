import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import { writeFileSync } from 'node:fs'

// Keep web/dist/.gitkeep so the Go embed directive always has a directory.
const gitkeep = (): Plugin => ({
  name: 'gitkeep',
  closeBundle() { writeFileSync('dist/.gitkeep', '') },
})

export default defineConfig({
  plugins: [vue(), gitkeep()],
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 1500 },
  server: {
    port: 5173,
    proxy: { '/api': { target: 'http://127.0.0.1:18080', changeOrigin: false } },
  },
})
