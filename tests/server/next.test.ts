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

  it('falls back to / for null and undefined', () => {
    expect(safeNext(null)).toBe('/');
    expect(safeNext(undefined)).toBe('/');
  });

  it('normalises dot segments without leaving the origin', () => {
    expect(safeNext('/a/../../requests')).toBe('/requests');
  });
});
