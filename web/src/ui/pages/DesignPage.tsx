import DesignView from '../views/DesignView';
import { Gate } from '../shell/Gate';

export default function DesignPage() {
  return <Gate label="design requests">{(me) => <DesignView me={me} />}</Gate>;
}
