import { expect, it, vi } from 'vitest';
vi.mock('../../src/server/env', () => ({ getEnv: () => ({}) }));
import { createFileStore, ReceiptStorageUnavailable } from '../../src/server/services';
import { LocalFileStore } from '../../src/server/files/local';
const env = { DB: {} as D1Database };
it('selects LocalFileStore for development and fails closed with 503 without production Drive', () => {
  expect(createFileStore(env, true)).toBeInstanceOf(LocalFileStore);
  for (const settings of [env, { ...env, GOOGLE_SERVICE_ACCOUNT_JSON: '{}' }, { ...env, DRIVE_ROOT_FOLDER_ID: 'root' }]) {
    try { createFileStore(settings, false); throw new Error('Expected storage configuration error'); }
    catch (e) { expect(e).toBeInstanceOf(ReceiptStorageUnavailable); expect((e as ReceiptStorageUnavailable).status).toBe(503); }
  }
});
