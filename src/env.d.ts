/// <reference types="astro/client" />

// Secrets and optional vars, on top of the bindings `wrangler types` generates from
// wrangler.jsonc into worker-configuration.d.ts. Set secrets with `wrangler secret put`,
// and locally in .dev.vars (see .dev.vars.example).
declare namespace Cloudflare {
  interface Env {
    DISCORD_CLIENT_ID?: string;
    DISCORD_CLIENT_SECRET?: string;
    /** Discord user id that is auto-created as the first director on first login. */
    BOOTSTRAP_DIRECTOR_DISCORD_ID?: string;
    /** Discord channel webhook for request notifications (Phase 1). */
    DISCORD_WEBHOOK_URL?: string;
    /** Google service account key JSON (Phase 1 Drive adapter). */
    GOOGLE_SERVICE_ACCOUNT_JSON?: string;
    /** Shared drive folder that receipts are filed under. */
    GOOGLE_DRIVE_FOLDER_ID?: string;
    DRIVE_ROOT_FOLDER_ID?: string;
    /** "1" enables /auth/dev in `astro dev` only. Ignored by production builds. */
    DEV_AUTH?: string;
  }
}

declare namespace App {
  interface Locals {
    /** The signed-in user, set by src/middleware.ts; null on public routes when signed out. */
    user: import('./core/contracts').CurrentUser | null;
    /** Hash id of the current session, for logout. */
    sessionId: string | null;
  }
}
