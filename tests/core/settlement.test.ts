import { expect, it } from 'vitest';
import {
  maxAgentSettlementCents,
  settlementToleranceFromEnv,
} from '../../src/core/settlement';

const defaults = settlementToleranceFromEnv({});

it('allows the larger of estimate + 10% and estimate + $25 by default', () => {
  expect(maxAgentSettlementCents(10_000, defaults)).toBe(12_500); // $100 → $125
  expect(maxAgentSettlementCents(25_000, defaults)).toBe(27_500); // both give $275
  expect(maxAgentSettlementCents(100_000, defaults)).toBe(110_000); // $1,000 → $1,100
  expect(maxAgentSettlementCents(0, defaults)).toBe(2_500);
});

it('reads the tolerance from env vars and ignores invalid values', () => {
  const t = settlementToleranceFromEnv({
    AGENT_SETTLEMENT_TOLERANCE_PERCENT: '5',
    AGENT_SETTLEMENT_TOLERANCE_CENTS: '1000',
  });
  expect(maxAgentSettlementCents(100_000, t)).toBe(105_000);
  expect(maxAgentSettlementCents(10_000, t)).toBe(11_000);
  expect(
    settlementToleranceFromEnv({
      AGENT_SETTLEMENT_TOLERANCE_PERCENT: 'lots',
      AGENT_SETTLEMENT_TOLERANCE_CENTS: '-3',
    }),
  ).toEqual(defaults);
});
