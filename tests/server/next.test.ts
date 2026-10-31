import { describe, expect, it } from 'vitest';
import { safeNext } from '../../src/server/auth/next';

describe('safeNext (post-login redirect target)', () => {
  it.each([
    ['/', '/'],
    ['/requests', '/requests'],
    ['/requests/new?event=e1#form', '/requests/new?event=e1#form'],
    ['/events/a%20b', '/events/a%20b'],
  ])('keeps same-origin path %j', (input, out) => {
    expect(safeNext(input)).toBe(out);
  });

  it.each([
    ['protocol-relative', '//example.com'],
    ['backslash', '/\\example.com'],
    ['backslash later', '/\\/example.com'],
    ['tab', '/\t/example.com'],
    ['newline', '/\n/example.com'],
    ['carriage return', '/\r/example.com'],
    ['leading space', ' /requests'],
    ['NUL', '/\0/example.com'],
    ['DEL', '/\x7f/x'],
    ['absolute URL', 'https://example.com/'],
    ['scheme', 'javascript:alert(1)'],
    ['relative path', 'requests'],
    ['empty', ''],
  ])('falls back to / for %s', (_name, input) => {
    expect(safeNext(input)).toBe('/');
  });

  // Re-review: normalisation turned these into '//example.com' (protocol-relative).
  it.each([
    ['dot segment', '/.//example.com'],
    ['parent segment', '/a/..//example.com'],
    ['encoded dot', '/%2e//example.com'],
    ['encoded dot, upper case', '/%2E//example.com'],
    ['encoded dot-dot', '/a/%2e%2e//example.com'],
    ['placeholder origin', '//app.invalid//example.com'],
    ['placeholder origin, https', 'https://app.invalid//example.com'],
  ])('falls back to / when normalising would leave the site: %s', (_name, input) => {
    expect(safeNext(input)).toBe('/');
  });

  it.each([
    ['unknown section', '/evil'],
    ['api route', '/api/admin/users'],
    ['auth route', '/auth/dev?as=director'],
    ['lookalike prefix', '/eventsfoo'],
  ])('only allows the app\'s own pages: %s', (_name, input) => {
    expect(safeNext(input)).toBe('/');
  });

  it('never returns anything a browser would read as another host', () => {
    const tricky = ['/.//x.com', '/./\\x.com', '/..//x.com', '/%2f/x.com', '/%2F%2Fx.com', '/.%2f/x.com', '///x.com'];
    for (const input of tricky) {
      const out = safeNext(input);
      expect(new URL(out, 'https://officers.cseed.co').host, `${input} → ${out}`).toBe('officers.cseed.co');
      expect(out.startsWith('//') || out.startsWith('/\\')).toBe(false);
    }
  });

  it('falls back to / for null and undefined', () => {
    expect(safeNext(null)).toBe('/');
    expect(safeNext(undefined)).toBe('/');
  });

  it('normalises dot segments without leaving the origin', () => {
    expect(safeNext('/a/../../requests')).toBe('/requests');
  });
});
