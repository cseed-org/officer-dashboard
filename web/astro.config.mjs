import { defineConfig } from 'astro/config';
import preact from '@astrojs/preact';

/**
 * Static build (web/dist) served by the Go backend. All data comes from /api/* at runtime.
 * In dev, Vite on :4340 proxies /api and /auth to API_ORIGIN (default: the v1 app on :4330;
 * set API_ORIGIN=http://127.0.0.1:8090 for the Go server).
 */
const API_ORIGIN = process.env.API_ORIGIN ?? 'http://127.0.0.1:4330';

/** @type {import('vite').ProxyOptions} */
const proxy = {
  target: API_ORIGIN,
  changeOrigin: true,
  // Session cookies are host-only today; strip a Domain attribute if a backend sets one so
  // the browser keeps the cookie for localhost:4340.
  cookieDomainRewrite: '',
  // Redirects (e.g. /auth/dev → /) must stay on the dev server, not jump to the API origin.
  autoRewrite: true,
  // The backend's CSRF check compares Origin with its own host; present the request as
  // same-origin (the browser already enforced that against :4340).
  configure: (p) => {
    p.on('proxyReq', (req) => {
      if (req.getHeader('origin')) req.setHeader('origin', API_ORIGIN);
    });
  },
};

export default defineConfig({
  output: 'static',
  integrations: [preact()],
  // /events and /events/ both resolve; the Go server serves <path>/index.html.
  trailingSlash: 'ignore',
  build: { format: 'directory' },
  server: { port: 4340, host: '127.0.0.1' },
  devToolbar: { enabled: false },
  vite: {
    // Pin the tsconfig: without this the resolver walks up to the v1 app's tsconfig.json
    // (repo root), whose `extends` can't resolve from web/node_modules.
    tsconfig: './tsconfig.json',
    server: {
      strictPort: true,
      proxy: { '/api': proxy, '/auth': proxy },
    },
  },
});
