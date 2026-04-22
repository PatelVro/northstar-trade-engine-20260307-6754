import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 3000,
    allowedHosts: true,
    proxy: {
      // Route /api to the aggregator on :8082 — it fans out to :8080 (crypto)
      // and :8081 (IBKR equity) based on trader_id, so the dashboard sees
      // all traders from a single origin.
      '/api': {
        target: 'http://127.0.0.1:8082',
        changeOrigin: true,
      },
      '/health': { target: 'http://127.0.0.1:8082', changeOrigin: true },
      '/readiness': { target: 'http://127.0.0.1:8082', changeOrigin: true },
    },
  },
})
