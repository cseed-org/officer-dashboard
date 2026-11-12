import CloudflareWidget from './CloudflareWidget';
import type { WidgetProps } from '../registry';
export default function WebAnalyticsWidget({ size, cloudflare, me }: WidgetProps) {
 return <CloudflareWidget id="web-analytics" size={size} state={cloudflare} me={me} />;
}
