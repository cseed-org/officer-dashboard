import type { Notifier, NotificationEvent } from '../../core/ports';
export class DiscordWebhookNotifier implements Notifier {
  constructor(
    private webhookUrl: string,
    private fetcher: typeof fetch = fetch,
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
                    [
                      'totalCents',
                      'requester',
                      'director',
                      'status',
                      'comment',
                    ].includes(k),
                )
                .map(([name, value]) => ({
                  name: name === 'totalCents' ? 'Amount' : name,
                  value: (name === 'totalCents'
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
