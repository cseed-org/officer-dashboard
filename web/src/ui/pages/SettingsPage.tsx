import SettingsView from '../views/SettingsView';
import { Gate } from '../shell/Gate';
import type { HelloMedia } from '../onboarding/WelcomeIntro';

/** `helloMedia`: the hello screen's video and poster, for Advanced › Replay welcome intro (settings.astro). */
export default function SettingsPage({ helloMedia }: { helloMedia?: HelloMedia }) {
  return <Gate label="settings">{(me) => <SettingsView me={me} helloMedia={helloMedia} />}</Gate>;
}
