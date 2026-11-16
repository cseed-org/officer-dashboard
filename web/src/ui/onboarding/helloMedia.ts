import { existsSync } from 'node:fs';
import { join } from 'node:path';
import type { HelloMedia } from './WelcomeIntro';

/**
 * The hello screen's background video (docs/frontend.md), for the .astro pages that show the intro
 * (onboarding, and Settings' replay). Drop `hello-loop.mp4` (and its first frame as `hello-poster.webp`
 * or `.jpg`) into web/public/media/. They are looked for when the site is built, so they appear after
 * the next build or deploy, and a missing file never costs a 404. Without them the stand-in plays.
 * Build time only (node:fs): import it from .astro frontmatter, never from client code.
 */
export function helloMedia(): HelloMedia {
  const dir = join(process.cwd(), 'public', 'media');
  const has = (f: string) => existsSync(join(dir, f));
  return {
    video: has('hello-loop.mp4') ? '/media/hello-loop.mp4' : undefined,
    poster: ['hello-poster.webp', 'hello-poster.jpg', 'hello-poster.jpeg', 'hello-poster.png'].filter(has).map((f) => `/media/${f}`)[0],
  };
}
