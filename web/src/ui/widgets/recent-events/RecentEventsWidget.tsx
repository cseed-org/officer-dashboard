import { href } from '../../../lib/routes';
import { DataTable } from '../../components/kit';
import { Async, Empty, Loading } from '../../components/states';
import { eventColumns } from '../../views/columns';

import Block from '../shell/WidgetCard';
import type { WidgetProps } from '../registry';
const wide = (size: import('../registry').BlockSize) => size.startsWith('4');
export default function RecentEventsWidget({ size, overview }: WidgetProps) {
  return (
              <Block id="recent-events" title="recent events" action={<a href={href.events()}>all events →</a>}>
                <Async state={overview} loading={<Loading rows={4} label="Loading events" />}>
                  {(d) =>
                    d.recentEvents.length === 0 ? (
                      <Empty title="No events yet" action={<a href={href.eventImport()}>import a Luma guest export →</a>}>
                        Events appear here once a Luma guest list is imported.
                      </Empty>
                    ) : (
                      <DataTable rows={d.recentEvents.slice(0, wide(size) ? 5 : 3)} columns={eventColumns(Date.now(), { compact: !wide(size) })} rowKey={(e) => e.id} caption="Recent events" dense />
                    )
                  }
                </Async>
              </Block>
            );
}
