import type { Run } from '../../../api/types'
import { EmptyState } from '../../../components/States'
import { Collapsible, JsonBlock } from '../../../components/misc'
import { usd } from '../../../lib/format'

/** The frozen RunSpec: exactly the configuration this run started with. */
export function SpecTab({ run }: { run: Run }) {
  const spec = run.spec
  if (!spec) return <EmptyState title="No spec recorded" hint="This run has no frozen configuration attached." />
  return (
    <div className="flex flex-col gap-2">
      <p className="text-[12px] text-muted">Frozen when the run started; later edits to agents, tasks or prices do not change it.</p>
      <Collapsible title="Crew" meta={`${spec.crew.name} · ${spec.crew.process}`} defaultOpen>
        <JsonBlock value={spec.crew} className="max-h-96" />
      </Collapsible>
      <Collapsible title="Inputs & budget" meta={`${Object.keys(spec.inputs).length} inputs · ${usd(spec.budget_usd)}`}>
        <JsonBlock value={{ inputs: spec.inputs, budget_usd: spec.budget_usd }} />
      </Collapsible>
      <Collapsible title="Agents" meta={`${spec.agents.length}`}>
        <div className="flex flex-col gap-2">
          {spec.agents.map((a) => (
            <Collapsible key={a.id} title={a.name} meta={a.role}>
              <JsonBlock value={a} className="max-h-96" />
            </Collapsible>
          ))}
        </div>
      </Collapsible>
      <Collapsible title="Tasks" meta={`${spec.tasks.length}, in crew order`}>
        <div className="flex flex-col gap-2">
          {spec.tasks.map((t, i) => (
            <Collapsible key={t.id} title={`${i + 1}. ${t.name}`}>
              <JsonBlock value={t} className="max-h-96" />
            </Collapsible>
          ))}
        </div>
      </Collapsible>
      <Collapsible title="LLMs" meta={`${spec.llms.length}`}>
        <JsonBlock value={spec.llms} className="max-h-96" />
      </Collapsible>
      <Collapsible title="Pricing" meta={`${spec.pricing.length} entries`}>
        <JsonBlock value={spec.pricing} className="max-h-96" />
      </Collapsible>
      <Collapsible title="Settings">
        <JsonBlock value={spec.settings} />
      </Collapsible>
    </div>
  )
}
