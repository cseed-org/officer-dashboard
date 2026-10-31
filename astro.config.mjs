// @ts-check
import { defineConfig } from 'astro/config';
import cloudflare from '@astrojs/cloudflare';
import preact from '@astrojs/preact';

// https://astro.build/config
export default defineConfig({
  output: 'server',
  // Interactive page bodies are Preact islands (src/ui/islands) that call /api/* through src/ui/api.
  integrations: [preact()],
  adapter: cloudflare({
    // No Cloudflare Images binding; the dashboard serves plain static images.
    imageService: 'passthrough',
  }),
  // Auth sessions live in D1 (see src/server/auth), so Astro's KV-backed sessions are off.
  session: false,
  security: { checkOrigin: true },
  // The toolbar injects its own headings and overlays, which confuse layout checks.
  devToolbar: { enabled: false },
});
