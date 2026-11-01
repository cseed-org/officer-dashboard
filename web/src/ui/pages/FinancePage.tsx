import FinanceView from '../views/FinanceView';
import { Gate } from '../shell/Gate';

export default function FinancePage() {
  return <Gate label="finance">{(me) => <FinanceView me={me} />}</Gate>;
}
