import CloudflareWidget from './CloudflareWidget';
import type { WidgetProps } from '../registry';
export default function MemberSignupsWidget({ size, cloudflare }: WidgetProps) {
 return <CloudflareWidget id="member-signups" size={size} state={cloudflare} />;
}
