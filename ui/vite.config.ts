import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// The build emits into the Go server's embed dir so `go:embed ui_dist` picks it
// up. base '/' keeps asset URLs absolute, which the SPA handler serves at root.
export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    outDir: '../internal/server/ui_dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test-setup.ts',
  },
})
