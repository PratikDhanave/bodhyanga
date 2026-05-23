import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// During `npm run dev`, the dev server runs on :5173 and proxies /api to the
// Go backend on :8088 (`go run ./cmd/care`). For prod, `npm run build`
// produces dist/ which cmd/care embeds via go:embed and serves at /.
export default defineConfig({
  plugins: [react()],
  base: '/ui/',
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8088',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // Keep filenames stable enough for go:embed but allow content hashing.
    rollupOptions: {
      output: {
        manualChunks: undefined,
      },
    },
  },
});
