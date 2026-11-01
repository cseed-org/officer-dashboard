import EventsView from '../views/EventsView';
import { Gate } from '../shell/Gate';

export default function EventsPage() {
  return <Gate label="events">{() => <EventsView />}</Gate>;
}
