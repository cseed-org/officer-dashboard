import { expect, test } from '@playwright/test';
import { h } from 'preact';
import { render } from 'preact-render-to-string';
import { BuildspaceDetail } from '../src/ui/views/buildspace/Detail';
import type { BuildspaceData, BuildspaceProject } from '../src/lib/contracts';

// Pure rendering regression: no browser or running API is needed.
test('project detail omits script links while retaining HTTPS links', () => {
  const project = { id: 'fixture', name: 'Project', track: 'Software', stage: 'Ideating', description: 'Test',
    links: ['javascript:alert(1)', 'https://example.org/project'], mentorIds: [], archivedAt: null } as unknown as BuildspaceProject;
  const b = { projects: [project], people: [], mentors: [], activity: [], cohorts: [], weekStart: '2026-01-01', today: '2026-01-02' } as unknown as BuildspaceData;
  const html = render(h(BuildspaceDetail, { b, project, reload: () => {}, onPerson: () => {}, readOnly: true }));
  expect(html).not.toContain('href="javascript:');
  expect(html).toContain('href="https://example.org/project"');
});
