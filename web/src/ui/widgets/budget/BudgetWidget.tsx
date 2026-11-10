import { useApi } from '../../../lib/api/useApi';
import { dollars, pct } from '../../../lib/format';
import { href } from '../../../lib/routes';
import { usageHue } from '../../../lib/colors';
import { CategoryDot, DataTable, Progress } from '../../components/kit';
import { Async, Empty, Loading } from '../../components/states';

import Block from '../shell/WidgetCard';
import type { WidgetProps } from '../registry';
const wide = (size: import('../registry').BlockSize) => size.startsWith('4');
export default function BudgetWidget({ size, overview }: WidgetProps) {
 const fy = overview.status === 'ready' ? overview.data.fiscalYear : undefined;
  return (
              <Block id="budget" title="budget vs actual" action={<a href={href.finance('budgets')}>budgets →</a>}>
                {fy === undefined ? <Loading rows={4} label="Loading the budget" /> : <BudgetMini fiscalYear={fy} rows={wide(size) ? 8 : 5} />}
              </Block>
            );
}
function BudgetMini({ fiscalYear, rows: limit = 6 }: { fiscalYear: number; rows?: number }) {
  const state = useApi('financeSummary', { query: { fiscalYear } });
  return (
    <Async state={state} loading={<Loading rows={4} label="Loading the budget" />}>
      {(d) => {
        if (!d.byCategory.length) return <Empty title="No budget set">A director sets the year’s budget by category.</Empty>;
        const rows = [...d.byCategory].sort((a, b) => b.spentCents + b.committedCents - (a.spentCents + a.committedCents)).slice(0, limit);
        return (
          <DataTable
            dense
            caption={`Budget vs actual, FY${d.fiscalYear}`}
            rows={rows}
            rowKey={(c) => c.account.id}
            columns={[
              {
                key: 'a',
                label: 'category',
                primary: true,
                render: (c) => (
                  <span class="cat-name">
                    <CategoryDot code={c.account.code} />
                    {c.account.name}
                  </span>
                ),
              },
              {
                key: 'u',
                label: 'used',
                hideOnPhone: true,
                render: (c) => {
                  const rate = c.budgetCents ? (c.spentCents + c.committedCents) / c.budgetCents : null;
                  return <Progress value={rate} hue={usageHue(rate)} label={`${c.account.name}: ${pct(rate)} used`} />;
                },
              },
              { key: 'b', label: 'budget', num: true, render: (c) => dollars(c.budgetCents) },
              { key: 's', label: 'actual', num: true, render: (c) => dollars(c.spentCents + c.committedCents) },
              {
                key: 'r',
                label: 'left',
                num: true,
                render: (c) => <span class={c.remainingCents < 0 ? 'check-bad' : undefined}>{dollars(c.remainingCents)}</span>,
              },
            ]}
            foot={
              <tr>
                <td class="dt-primary">all categories</td>
                <td class="dt-hide-phone" data-label="used" />
                <td class="num" data-label="budget">{dollars(d.totals.budgetCents)}</td>
                <td class="num" data-label="actual">{dollars(d.totals.spentCents + d.totals.committedCents)}</td>
                <td class="num" data-label="left">{dollars(d.totals.remainingCents)}</td>
              </tr>
            }
          />
        );
      }}
    </Async>
  );
}

