import type { Notifier, NotificationEvent } from '../../core/ports';
const FIELD_LABELS: Record<string, string> = {
  totalCents: 'Amount',
  attemptedCents: 'Agent reported',
  approvedCents: 'Approved',
  requester: 'requester',
  director: 'director',
  status: 'status',
  comment: 'comment',
  reason: 'reason',
};
export class DiscordWebhookNotifier implements Notifier {
  constructor(
    private webhookUrl: string,
    // Wrapped: calling the global fetch as a method (this.fetcher) throws
    // "Illegal invocation" on Workers.
    private fetcher: typeof fetch = (input, init) => fetch(input, init),
  ) {}
  async notify(event: NotificationEvent, link: string) {
    try {
      const response = await this.fetcher(this.webhookUrl, {
        method: 'POST',
        signal: AbortSignal.timeout(5000),
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          allowed_mentions: { parse: [] },
          embeds: [
            {
              title: `${event.number}: ${event.title}`.slice(0, 256),
              url: link,
              description: event.type,
              fields: Object.entries(event)
                .filter(
                  ([k, v]) =>
                    v !== undefined &&
                    v !== null &&
                    String(v).trim().length > 0 &&
                    k in FIELD_LABELS,
                )
                .map(([name, value]) => ({
                  name: FIELD_LABELS[name]!,
                  value: (name.endsWith('Cents')
                    ? new Intl.NumberFormat('en-US', {
                        style: 'currency',
                        currency: 'USD',
                      }).format(Number(value) / 100)
                    : String(value)
                  ).slice(0, 1024),
                  inline: true,
                })),
            },
          ],
        }),
      });
      if (!response.ok)
        console.error('Discord notification failed', response.status);
    } catch {
      console.error('Discord notification delivery failed');
    }
  }
}
