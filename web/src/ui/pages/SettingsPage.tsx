import SettingsView from '../views/SettingsView';
import { Gate } from '../shell/Gate';

export default function SettingsPage() {
  return <Gate label="settings">{(me) => <SettingsView me={me} />}</Gate>;
}
