import CloudflareWidget from './CloudflareWidget';
import type { WidgetProps } from '../registry';
export default function WorkersD1Widget({ size, cloudflare, me }: WidgetProps) {
 return <CloudflareWidget id="workers-d1" size={size} state={cloudflare} me={me} />;
}
