import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

// Vitest config for component-level tests. Uses jsdom so DOM APIs (window,
// fetch, EventSource via globals stubbed in setup.ts) are available without
// a real browser. The vite config base="/ui/" is irrelevant here — tests
// don't navigate to URLs.
export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    // Pattern is `.unit.tsx` (not `.test.tsx`) so Playwright 1.40's default
    // testMatch (`**/*.{test,spec}.*`) doesn't try to load these files
    // during e2e discovery and crash on the .tsx extension.
    include: ['src/**/*.{unit,test}.{ts,tsx}', 'src/**/__tests__/**/*.{ts,tsx}'],
    exclude: ['e2e/**', 'node_modules/**', 'dist/**', '**/.claude/**'],
    css: false,
    restoreMocks: true,
  },
});
