import { useEffect } from 'preact/hooks';
import SettingsView from '../views/SettingsView';
import { Gate } from '../shell/Gate';
import type { HelloMedia } from '../onboarding/WelcomeIntro';

/** `helloMedia`: the hello screen's video and poster, for Advanced › Replay welcome intro (settings.astro). */
export default function SettingsPage({ helloMedia }: { helloMedia?: HelloMedia }) {
  useEffect(() => {
    const q = new URLSearchParams(location.search);
    if (q.has('drive') || q.has('orgGoogle') || ['#drive', '#org-google'].includes(location.hash)) {
      location.replace(`/setup${location.search}${location.hash === '#org-google' ? '#org-calendar' : location.hash}`);
    }
  }, []);
  return <Gate label="settings">{(me) => <SettingsView me={me} helloMedia={helloMedia} />}</Gate>;
}
