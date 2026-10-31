import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    include: ['tests/core/**/*.test.ts', 'tests/db/**/*.test.ts', 'tests/server/**/*.test.ts'],
    environment: 'node',
  },
});
