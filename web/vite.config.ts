import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// The build goes straight into the Go embed folder: `npm run build`, then `go build`.
export default defineConfig({
  plugins: [vue()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
    target: 'es2022',
  },
  server: {
    // Run an agent locally (go run ./cmd/vps-admin serve -config dev.json) on port 7171
    proxy: { '/api': { target: 'http://127.0.0.1:7171', changeOrigin: false } },
  },
})
