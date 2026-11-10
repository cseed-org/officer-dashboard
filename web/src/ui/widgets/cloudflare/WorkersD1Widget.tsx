import CloudflareWidget from './CloudflareWidget';
import type { WidgetProps } from '../registry';
export default function WorkersD1Widget({ size, cloudflare }: WidgetProps) {
 return <CloudflareWidget id="workers-d1" size={size} state={cloudflare} />;
}
