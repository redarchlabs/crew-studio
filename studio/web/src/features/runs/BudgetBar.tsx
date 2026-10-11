import { usd } from '../../lib/format'

type BudgetBarProps = { spent: number; budget: number; hardStopMultiplier?: number; state: string | null }

/** Spend vs. budget; the track extends to the hard-stop limit when known. */
export function BudgetBar({ spent, budget, hardStopMultiplier, state }: BudgetBarProps) {
  const limit = budget * Math.max(1, hardStopMultiplier ?? 1)
  const pct = limit > 0 ? Math.min(100, (spent / limit) * 100) : 0
  const budgetPct = limit > 0 ? (budget / limit) * 100 : 100
  const ratio = budget > 0 ? spent / budget : 0
  const color = ratio >= 1 ? 'bg-danger' : ratio >= 0.8 ? 'bg-warn' : 'bg-ok'
  return (
    <div className="min-w-0">
      <div className="mb-1 flex items-baseline justify-between gap-2">
        <span className="eyebrow">Spend</span>
        <span className="font-mono text-[12px] tabular">
          <span className="text-fg">{usd(spent)}</span>
          <span className="text-faint"> / {usd(budget)}</span>
          {state && <span className="ml-1.5 text-warn">{state === 'hard_stopped' ? 'hard stop' : 'searches stopped'}</span>}
        </span>
      </div>
      <div className="relative h-1.5 overflow-hidden rounded-full bg-panel-2" role="progressbar" aria-valuemin={0} aria-valuemax={budget} aria-valuenow={spent} aria-label="Spend versus budget">
        <div className={`h-full ${color} transition-[width] duration-500`} style={{ width: `${pct}%` }} />
        {budgetPct < 100 && <div className="absolute inset-y-0 w-px bg-fg/60" style={{ left: `${budgetPct}%` }} title="Budget" />}
      </div>
      {budgetPct < 100 && <div className="mt-0.5 text-right font-mono text-[10px] text-faint">hard stop {usd(limit)}</div>}
    </div>
  )
}
