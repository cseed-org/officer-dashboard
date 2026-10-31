import type { FileStore, Notifier } from '../core/ports';
import { D1LocalFileStore } from './files/d1-local';
import { GoogleDriveFileStore } from './files/drive';
import { ConsoleNotifier } from './notify/console';
import { DiscordWebhookNotifier } from './notify/discord';
import { getEnv } from './env';
let fileStore: FileStore | undefined;
export function getFileStore(): FileStore {
  const env = getEnv();
  const root = env.DRIVE_ROOT_FOLDER_ID ?? env.GOOGLE_DRIVE_FOLDER_ID;
  if (env.GOOGLE_SERVICE_ACCOUNT_JSON && !root)
    throw new Error('DRIVE_ROOT_FOLDER_ID is required with Google credentials');
  if (!import.meta.env.DEV && !env.GOOGLE_SERVICE_ACCOUNT_JSON)
    throw new Error(
      'Receipt storage requires configured Google Drive credentials',
    );
  return (fileStore ??=
    env.GOOGLE_SERVICE_ACCOUNT_JSON && root
      ? new GoogleDriveFileStore(env.GOOGLE_SERVICE_ACCOUNT_JSON, root)
      : new D1LocalFileStore(env.DB));
}
let notifier: Notifier | undefined;
export function getNotifier(): Notifier {
  return (notifier ??= getEnv().DISCORD_WEBHOOK_URL
    ? new DiscordWebhookNotifier(getEnv().DISCORD_WEBHOOK_URL!)
    : new ConsoleNotifier());
}
