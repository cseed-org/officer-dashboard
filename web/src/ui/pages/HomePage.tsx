import HomeView from '../views/HomeView';
import { Gate } from '../shell/Gate';

export default function HomePage() {
  return <Gate label="home">{(me) => <HomeView me={me} />}</Gate>;
}
