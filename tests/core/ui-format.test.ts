import { describe, expect, it } from 'vitest';
import { pacificIso, safeUrl } from '../../src/ui/format';

describe('safeUrl', () => {
  it('keeps http(s) URLs and same-origin paths', () => {
    expect(safeUrl('https://www.adafruit.com/')).toBe('https://www.adafruit.com/');
    expect(safeUrl('http://example.com/a?b=1')).toBe('http://example.com/a?b=1');
    expect(safeUrl('/api/attachments/att-1')).toBe('/api/attachments/att-1');
  });
  it('drops other schemes, protocol-relative and junk', () => {
    for (const bad of ['javascript:alert(1)', ' JavaScript:alert(1)', 'data:text/html,<b>x</b>', 'vbscript:x', '//evil.example', 'not a url', '', null, undefined]) {
      expect(safeUrl(bad)).toBeNull();
    }
  });
});

describe('pacificIso', () => {
  it('reads the date as Pacific wall time, with the right DST offset', () => {
    expect(pacificIso('2026-10-31', '18:00')).toBe('2026-10-31T18:00:00-07:00');
    expect(pacificIso('2026-12-01', '18:00')).toBe('2026-12-01T18:00:00-08:00');
    expect(new Date(pacificIso('2026-10-31')).toISOString()).toBe('2026-11-01T01:00:00.000Z');
  });
});
