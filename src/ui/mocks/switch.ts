/**
 * THE mock switch. While it is on, endpoints that have a handler in ./handlers.ts are
 * answered in the browser from seed-shaped data instead of the network; every other
 * endpoint (e.g. /api/me) still goes to the real API.
 *
 * Phase 1 runs the UI and the backend in parallel, so most /api/* handlers don't exist yet.
 * Once the backend lands: set PUBLIC_UI_MOCKS=off (or flip the default below to false),
 * check the pages against the real API, then delete src/ui/mocks/ and this import in
 * src/ui/api/client.ts.
 */
export const UI_MOCKS: boolean = (import.meta.env.PUBLIC_UI_MOCKS ?? 'on') !== 'off';
