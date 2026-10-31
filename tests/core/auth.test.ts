import { describe, expect, it } from 'vitest';
import { decideLogin, hasRole, isDevAuthEnabled, requiredRoleForPath } from '../../src/core/auth';

describe('hasRole', () => {
  it('lets a director do anything an officer can', () => {
    expect(hasRole('director', 'officer')).toBe(true);
    expect(hasRole('director', 'director')).toBe(true);
  });

  it('keeps officers out of director-only actions', () => {
    expect(hasRole('officer', 'officer')).toBe(true);
    expect(hasRole('officer', 'director')).toBe(false);
  });
});

describe('requiredRoleForPath', () => {
  it('leaves login and auth routes public', () => {
    expect(requiredRoleForPath('/login')).toBeNull();
    expect(requiredRoleForPath('/auth/callback')).toBeNull();
    expect(requiredRoleForPath('/api/health')).toBeNull();
  });

  it('requires a director for admin pages and admin APIs', () => {
    expect(requiredRoleForPath('/admin/users')).toBe('director');
    expect(requiredRoleForPath('/admin')).toBe('director');
    expect(requiredRoleForPath('/api/admin/tokens')).toBe('director');
  });

  it('does not treat a lookalike prefix as admin', () => {
    expect(requiredRoleForPath('/administrivia')).toBe('officer');
  });

  it('requires an officer everywhere else', () => {
    expect(requiredRoleForPath('/')).toBe('officer');
    expect(requiredRoleForPath('/requests/new')).toBe('officer');
    expect(requiredRoleForPath('/api/overview')).toBe('officer');
  });

  it('leaves service-token APIs to their own token check', () => {
    expect(requiredRoleForPath('/api/ingest/luma')).toBeNull();
    expect(requiredRoleForPath('/api/agent/jobs')).toBeNull();
  });
});

describe('isDevAuthEnabled', () => {
  it('is on only in a dev build with DEV_AUTH=1', () => {
    expect(isDevAuthEnabled({ devBuild: true, flag: '1' })).toBe(true);
  });

  it('is off in a production build even if DEV_AUTH=1 is set', () => {
    expect(isDevAuthEnabled({ devBuild: false, flag: '1' })).toBe(false);
  });

  it('is off in dev unless explicitly enabled', () => {
    expect(isDevAuthEnabled({ devBuild: true, flag: undefined })).toBe(false);
    expect(isDevAuthEnabled({ devBuild: true, flag: '' })).toBe(false);
    expect(isDevAuthEnabled({ devBuild: true, flag: '0' })).toBe(false);
    expect(isDevAuthEnabled({ devBuild: true, flag: 'true' })).toBe(false);
  });
});

describe('decideLogin (Discord allowlist)', () => {
  const officer = { id: 'u1', role: 'officer' as const, active: true };

  it('signs in an active allowlisted user', () => {
    expect(decideLogin({ discordId: '42', user: officer, bootstrapDirectorId: undefined })).toEqual({
      kind: 'sign_in',
      userId: 'u1',
    });
  });

  it('refuses a deactivated user', () => {
    expect(
      decideLogin({ discordId: '42', user: { ...officer, active: false }, bootstrapDirectorId: '42' }),
    ).toEqual({ kind: 'deny', reason: 'inactive' });
  });

  it('refuses an unknown Discord account', () => {
    expect(decideLogin({ discordId: '42', user: null, bootstrapDirectorId: '7' })).toEqual({
      kind: 'deny',
      reason: 'not_allowlisted',
    });
  });

  it('creates the bootstrap director on their first login', () => {
    expect(decideLogin({ discordId: '7', user: null, bootstrapDirectorId: '7' })).toEqual({
      kind: 'create_director',
    });
  });

  it('ignores an empty bootstrap setting', () => {
    expect(decideLogin({ discordId: '', user: null, bootstrapDirectorId: '' })).toEqual({
      kind: 'deny',
      reason: 'not_allowlisted',
    });
  });
});
