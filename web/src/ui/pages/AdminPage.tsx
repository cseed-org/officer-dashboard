import AdminView from '../views/AdminView';
import { Gate } from '../shell/Gate';

export default function AdminPage() {
  return <Gate label="admin">{(me) => <AdminView me={me} />}</Gate>;
}
