import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    include: ['tests/core/**/*.test.ts', 'tests/db/**/*.test.ts', 'tests/server/**/*.test.ts'],
    environment: 'node',
    // Local D1 tests launch workerd; keep the shared Pi's memory/CPU bounded.
    maxWorkers: 1,
  },
});
