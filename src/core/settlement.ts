/**
 * How far an automated (agent) settlement may exceed the director-approved
 * estimate before it is held for director review (N2). The director's own
 * transition path is trusted and is not limited.
 */
export interface SettlementTolerance {
  /** Allowed overrun as a percentage of the estimate, e.g. 10 for 10%. */
  percent: number;
  /** Allowed overrun as a flat amount, in cents. */
  cents: number;
}
export const DEFAULT_SETTLEMENT_TOLERANCE: SettlementTolerance = {
  percent: 10,
  cents: 2_500,
};
/** The largest actual total an agent may post: whichever allowance is larger. */
export function maxAgentSettlementCents(
  estimateCents: number,
  t: SettlementTolerance,
): number {
  return Math.max(
    Math.floor((estimateCents * (100 + t.percent)) / 100),
    estimateCents + t.cents,
  );
}
export function settlementToleranceFromEnv(env: {
  AGENT_SETTLEMENT_TOLERANCE_PERCENT?: string;
  AGENT_SETTLEMENT_TOLERANCE_CENTS?: string;
}): SettlementTolerance {
  const read = (v: string | undefined, fallback: number) => {
    const n = v === undefined || v.trim() === '' ? NaN : Number(v);
    return Number.isFinite(n) && n >= 0 ? n : fallback;
  };
  return {
    percent: read(
      env.AGENT_SETTLEMENT_TOLERANCE_PERCENT,
      DEFAULT_SETTLEMENT_TOLERANCE.percent,
    ),
    cents: Math.round(
      read(
        env.AGENT_SETTLEMENT_TOLERANCE_CENTS,
        DEFAULT_SETTLEMENT_TOLERANCE.cents,
      ),
    ),
  };
}
