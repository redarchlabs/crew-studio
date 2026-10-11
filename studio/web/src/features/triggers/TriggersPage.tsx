import { Link } from 'react-router'
import { Plus, Send, Webhook } from 'lucide-react'
import { triggersApi } from '../../api/endpoints'
import { AreaChips } from '../../components/AreaChips'
import { LinkButton } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { HistoryLink } from '../../components/misc'
import { Can, useAuth } from '../../lib/auth'
import { dateTime, int, relative } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'
import { CrewLabel } from '../workorders/workOrderLabels'

export function TriggersPage() {
  const res = useAsync((signal) => triggersApi.list(signal), [])
  const { can } = useAuth()
  return (
    <>
      <PageHeader
        eyebrow="Automate"
        title="Triggers"
        description="Webhook URLs other systems POST JSON to. Each call creates and submits a work order, and can call back when it finishes."
        actions={<><HistoryLink type="trigger" /><Can perm="triggers.manage"><LinkButton to="/triggers/new" variant="primary" icon={<Plus className="size-4" />}>New trigger</LinkButton></Can></>}
      />
      <AsyncView {...res}>
        {(list) =>
          list.length === 0 ? (
            <EmptyState icon={<Webhook className="size-6" />} title="No triggers yet" hint="A trigger gives another system a URL that starts a crew." action={can('triggers.manage') && <LinkButton to="/triggers/new" variant="primary">Create a trigger</LinkButton>} />
          ) : (
            <div className="overflow-x-auto rounded-lg border border-line bg-panel">
              <table className="w-full min-w-[860px] text-left">
                <thead className="border-b border-line">
                  <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                    <th>Trigger</th><th>Crew</th><th>Status</th><th>Last fired</th><th className="text-right">Fired</th><th>Callback</th><th>Token</th>
                  </tr>
                </thead>
                <tbody>
                  {[...list].sort((a, b) => a.name.localeCompare(b.name)).map((t) => (
                    <tr key={t.id} className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
                      <td className="max-w-xs">
                        <Link to={`/triggers/${t.id}`} className="font-medium hover:text-accent">{t.name}</Link>
                        {t.description && <div className="truncate text-[12px] text-muted">{t.description}</div>}
                        <div className="mt-1"><AreaChips areaIds={t.area_ids} max={2} /></div>
                      </td>
                      <td className="max-w-[200px] text-[12.5px]"><CrewLabel name={t.target_name} crewId={t.crew_id} link /></td>
                      <td><Chip tone={t.enabled ? 'ok' : 'neutral'}>{t.enabled ? 'enabled' : 'disabled'}</Chip></td>
                      <td className="whitespace-nowrap text-[12.5px] text-muted" title={dateTime(t.last_fired_at)}>{t.last_fired_at ? relative(t.last_fired_at) : 'never'}</td>
                      <td className="text-right font-mono tabular">{int(t.fire_count)}</td>
                      <td>{t.callback_url ? <span className="inline-flex items-center gap-1 text-[12px] text-teal" title={t.callback_url}><Send className="size-3" /> set</span> : <span className="text-faint">—</span>}</td>
                      <td className="font-mono text-[11.5px] text-faint">cst_{t.token_prefix}_…</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        }
      </AsyncView>
    </>
  )
}
