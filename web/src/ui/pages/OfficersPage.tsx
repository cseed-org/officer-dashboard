import OfficersView from '../views/OfficersView';
import { Gate } from '../shell/Gate';

export default function OfficersPage() {
  return <Gate label="officers">{(me) => <OfficersView me={me} />}</Gate>;
}
