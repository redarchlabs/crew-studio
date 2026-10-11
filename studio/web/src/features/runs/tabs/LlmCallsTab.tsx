import { Fragment, useEffect, useState } from 'react'
import { ChevronDown, ChevronRight, Cpu, RefreshCw } from 'lucide-react'
import { Button } from '../../../components/Button'
import { EmptyState, ErrorState, Loading } from '../../../components/States'
import { int, timeOfDay, usd } from '../../../lib/format'
import { useLlmCalls } from '../useLlmCalls'
import { LlmCallBody } from './MessageView'

/** refreshKey: a live count of LLM calls; when it changes during a run, the list is fetched again. */
export function LlmCallsTab({ live, refreshKey }: { live: boolean; refreshKey?: number }) {
  const { calls, loading, error, load } = useLlmCalls()
  const [open, setOpen] = useState<number | null>(null)
  useEffect(() => load({ force: live }), [load, live, refreshKey])

  if (error && !calls) return <ErrorState error={error} onRetry={() => load({ force: true })} title="Could not load LLM calls" />
  if (!calls) return <Loading label="Loading LLM calls…" />

  const total = calls.reduce((acc, c) => acc + (c.cost_usd ?? 0), 0)
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between text-[12px] text-muted">
        <span>{calls.length} calls · {usd(total)}</span>
        <Button size="sm" variant="ghost" icon={<RefreshCw className={`size-3.5 ${loading ? 'animate-spin' : ''}`} />} onClick={() => load({ force: true })}>Refresh</Button>
      </div>
      {calls.length === 0 ? (
        <EmptyState icon={<Cpu className="size-6" />} title="No LLM calls recorded" />
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-panel">
          <table className="w-full min-w-[760px] text-left">
            <thead className="border-b border-line">
              <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                <th className="w-6" /><th>#</th><th>Agent</th><th>Task</th><th>Model</th><th className="text-right">Prompt</th><th className="text-right">Completion</th><th className="text-right">Cost</th><th>Time</th>
              </tr>
            </thead>
            <tbody>
              {calls.map((c) => {
                const isOpen = open === c.seq
                return (
                  <Fragment key={c.seq}>
                    <tr className="cursor-pointer border-b border-line hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-1.5" onClick={() => setOpen(isOpen ? null : c.seq)} aria-expanded={isOpen}>
                      <td>{isOpen ? <ChevronDown className="size-3.5 text-faint" /> : <ChevronRight className="size-3.5 text-faint" />}</td>
                      <td className="font-mono text-[11.5px] text-faint">{c.seq}</td>
                      <td>{c.agent_role}</td>
                      <td className="text-muted">{c.task_name}</td>
                      <td className="font-mono text-[11.5px] text-muted">{c.model}</td>
                      <td className="text-right font-mono tabular">{int(c.usage?.prompt_tokens)}{c.usage?.cached_prompt_tokens ? <span className="text-faint" title="read from the prompt cache"> ({int(c.usage.cached_prompt_tokens)}c)</span> : null}{c.usage?.cache_write_tokens ? <span className="text-faint" title="written to the prompt cache"> ({int(c.usage.cache_write_tokens)}w)</span> : null}</td>
                      <td className="text-right font-mono tabular">{int(c.usage?.completion_tokens)}</td>
                      <td className="text-right font-mono tabular">{usd(c.cost_usd)}</td>
                      <td className="font-mono text-[11px] text-faint">{timeOfDay(c.created_at)}</td>
                    </tr>
                    {isOpen && (
                      <tr className="border-b border-line bg-bg/40">
                        <td colSpan={9} className="p-3"><LlmCallBody call={c} /></td>
                      </tr>
                    )}
                  </Fragment>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
