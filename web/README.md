# Astro + Preact front end

A static Astro site with Preact islands, served by the Go backend from `web/dist`. Data comes from the same-origin JSON API and is validated with zod.

From this directory, `npm ci && npm run build` produces the static site. For live development, run `API_ORIGIN=http://127.0.0.1:8090 npm run dev` with the seeded Go backend running.

- [Development and Playwright](../docs/development.md)
- [Front-end routes, preferences, proxy, and layout](../docs/frontend.md)
- [API reference](../docs/api.md) · [Architecture](../docs/architecture.md) · [All docs](../docs/README.md)
