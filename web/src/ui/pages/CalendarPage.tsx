import CalendarView from '../views/calendar/CalendarView';
import { Gate } from '../shell/Gate';

export default function CalendarPage() {
  return <Gate label="the calendar">{(me) => <CalendarView me={me} />}</Gate>;
}
