import CloudflareWidget from './CloudflareWidget';
import type { WidgetProps } from '../registry';
export default function TrafficSecurityWidget({ size, cloudflare, me }: WidgetProps) {
 return <CloudflareWidget id="traffic-security" size={size} state={cloudflare} me={me} />;
}
