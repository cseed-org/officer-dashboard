import type { NotificationEvent, Notifier } from '../../core/ports';

/** Logs notifications instead of posting them. The Discord webhook adapter is Phase 1. */
export class ConsoleNotifier implements Notifier {
  async notify(event: NotificationEvent, link: string): Promise<void> {
    console.info('[notify]', event.type, JSON.stringify(event), link);
  }
}
