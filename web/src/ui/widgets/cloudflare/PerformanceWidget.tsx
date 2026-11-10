import CloudflareWidget from './CloudflareWidget';
import type { WidgetProps } from '../registry';
export default function PerformanceWidget({ size, cloudflare, me }: WidgetProps) {
 return <CloudflareWidget id="performance" size={size} state={cloudflare} me={me} />;
}
