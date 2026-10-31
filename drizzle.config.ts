import { defineConfig } from 'drizzle-kit';

// `npm run db:generate` diffs src/server/db/schema.ts into a new SQL file in migrations/.
// Wrangler applies migrations/*.sql in order (`npm run db:migrate:local`).
export default defineConfig({
  dialect: 'sqlite',
  schema: './src/server/db/schema.ts',
  out: './migrations',
});
