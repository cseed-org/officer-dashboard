import type { FinanceSummaryResponse } from '../../../lib/contracts';
import { useApi } from '../../../lib/api/useApi';
import { dollars, pct } from '../../../lib/format';
import { categoryHue, usageHue } from '../../../lib/colors';
import { Progress } from '../../components/kit';
import WidgetCard, { WidgetEmpty, WidgetState } from '../shell/WidgetCard';
import { fitRows, useBox } from '../shell/fit';
import type { BlockSize, WidgetProps } from '../registry';
import { Donut } from './Donut';

type Category = FinanceSummaryResponse['byCategory'][number];
const used = (c: { spentCents: number; committedCents: number }) => c.spentCents + c.committedCents;

function Budget({ d, size }: { d: FinanceSummaryResponse; size: BlockSize }) {
  const wide = size === '4x2';
  const [wrap, box] = useBox();
  const [legendRef, legendBox] = useBox();
  const cats = [...d.byCategory].sort((a, b) => used(b) - used(a));
  const t = d.totals;
  const share = t.budgetCents ? used(t) / t.budgetCents : null;
  // The donut scales with the tile: the body height, capped by a share of its width.
  const donut = Math.round(Math.max(96, Math.min(box.height || (wide ? 176 : 128), (box.width || 320) * (wide ? 0.3 : 0.42), wide ? 200 : 168)));
  // 2×2 legend: one line per category (18px + 6px gap); 4×2 adds a progress bar (2 columns).
  // Sparse (few categories): a one-line summary fills the space the legend leaves.
  const sparse = !wide && cats.length <= 3;
  const shown = wide ? cats : cats.slice(0, fitRows({ height: legendBox.height && Math.max(18, legendBox.height - (sparse ? 34 : 0)), total: cats.length, row: 18, gap: 6, more: 0, fallback: 5 }));
  const fits = shown.length === cats.length;
  const legendShown = fits || wide ? shown : shown.slice(0, Math.max(1, shown.length - 1));
  const hidden = cats.length - legendShown.length;
  const totals = <div class="blegend-total"><span>All categories</span><span><b>{dollars(used(t))}</b> used · <span class={t.remainingCents < 0 ? 'over' : undefined}>{dollars(t.remainingCents)}</span> left of {dollars(t.budgetCents)}</span></div>;
  const row = (c: Category) => {
    const rate = c.budgetCents ? used(c) / c.budgetCents : null;
    return <li key={c.account.id}>
      <span class="dot" data-hue={categoryHue(c.account.code)} />
      <b>{c.account.name}</b>
      <span><span class={c.remainingCents < 0 ? 'over' : undefined}>{dollars(used(c))}</span> / {dollars(c.budgetCents)}</span>
      {wide && <Progress value={rate} hue={usageHue(rate)} label={`${c.account.name}: ${pct(rate)} used`} />}
    </li>;
  };
  return (
    <div class={`donut-wrap${wide ? ' donut-wrap--wide' : ''}`} ref={wrap}>
      <Donut size={donut} total={t.budgetCents} label={`Budget FY${d.fiscalYear}: ${pct(share)} used, ${dollars(used(t))} of ${dollars(t.budgetCents)}`}
        center={pct(share)} sub={`used of ${dollars(t.budgetCents)}`}
        segments={cats.map((c) => ({ key: c.account.id, value: used(c), hue: categoryHue(c.account.code) }))} />
      <div class="blegend-col" ref={legendRef}>
        <ul class={`blegend${wide ? ' blegend--2' : ''}`}>
          {legendShown.map(row)}
          {hidden > 0 && <li class="blegend-more"><span /><span>+{hidden} more</span><span /></li>}
        </ul>
        {(wide || sparse) && totals}
      </div>
    </div>
  );
}

export default function BudgetWidget({ me, size, overview }: WidgetProps) {
  const fy = overview.status === 'ready' ? overview.data.fiscalYear : undefined;
  return (
    <WidgetCard id="budget" me={me} aside={fy ? `FY${fy}` : null}>
      {fy === undefined ? <WidgetState state={overview} label="the budget" /> : <BudgetBody fiscalYear={fy} size={size} />}
    </WidgetCard>
  );
}

function BudgetBody({ fiscalYear, size }: { fiscalYear: number; size: BlockSize }) {
  const state = useApi('financeSummary', { query: { fiscalYear } });
  if (state.status !== 'ready') return <WidgetState state={state} label="the budget" />;
  if (!state.data.byCategory.length || !state.data.totals.budgetCents) return <WidgetEmpty icon="pie" size={size}>No budget set for FY{fiscalYear}</WidgetEmpty>;
  return <Budget d={state.data} size={size} />;
}
