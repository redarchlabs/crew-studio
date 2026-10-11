import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router'
import { Cpu, Pencil, Plus, Trash2 } from 'lucide-react'
import { llmsApi, pricingApi } from '../../api/endpoints'
import type { LLM } from '../../api/types'
import { Button, IconButton } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { ConfirmButton, HistoryLink } from '../../components/misc'
import { ReadOnlyNote } from '../../components/ReadOnlyNote'
import { useAuth } from '../../lib/auth'
import { sortByName } from '../../lib/catalog'
import { useAsync } from '../../lib/useAsync'
import { notifyError, notifySuccess } from '../../lib/notify'
import { LlmForm } from './LlmForm'
import { PriceMatchBadge } from './PriceMatchBadge'

export function LlmsPage() {
  const { can } = useAuth()
  const canManage = can('llms.manage')
  // Price matching is only shown to people who can read prices
  const showPrices = can('pricing.view')
  const res = useAsync(async (signal) => {
    const [llms, prices] = await Promise.all([llmsApi.list(signal), showPrices ? pricingApi.list(signal) : Promise.resolve([])])
    return { llms: sortByName(llms), prices }
  }, [showPrices])
  const [params, setParams] = useSearchParams()
  const [creating, setCreating] = useState(false)
  const editId = canManage ? params.get('edit') : null
  const editing = useMemo(() => res.data?.llms.find((l) => l.id === editId) ?? null, [res.data, editId])

  const closeForm = () => {
    setCreating(false)
    if (editId) setParams((p) => { const next = new URLSearchParams(p); next.delete('edit'); return next })
  }

  const onSaved = (saved: LLM) => {
    res.setData((prev) => prev && { ...prev, llms: sortByName([...prev.llms.filter((l) => l.id !== saved.id), saved]) })
    closeForm()
  }

  const remove = async (llm: LLM) => {
    try {
      await llmsApi.remove(llm.id)
      res.setData((prev) => prev && { ...prev, llms: prev.llms.filter((l) => l.id !== llm.id) })
      notifySuccess('LLM deleted')
    } catch (err) {
      notifyError(`Could not delete ${llm.name}`, err)
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Configure"
        title="LLMs"
        description="Named model configurations that agents and crew managers use. Each model should have a price entry so runs can be costed."
        actions={
          <>
            {can('history.view') && <HistoryLink type="llm" />}
            {canManage && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>New LLM</Button>}
          </>
        }
      />
      {!canManage && <ReadOnlyNote perm="llms.manage" />}
      <AsyncView {...res}>
        {({ llms, prices }) =>
          llms.length === 0 ? (
            <EmptyState
              icon={<Cpu className="size-6" />}
              title="No LLMs yet"
              hint={canManage ? 'Add a model configuration, e.g. openai/gpt-6-luna, then assign it to agents.' : 'An administrator has not added any model configurations yet.'}
              action={canManage ? <Button variant="primary" onClick={() => setCreating(true)}>Add an LLM</Button> : undefined}
            />
          ) : (
            <div className="overflow-x-auto rounded-lg border border-line bg-panel">
              <table className="w-full min-w-[760px] text-left">
                <thead className="border-b border-line">
                  <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                    <th>Name</th><th>Model</th><th>Effort</th><th>Temp</th><th>Max tokens</th>{showPrices && <th>Price entry</th>}{canManage && <th className="w-32" />}
                  </tr>
                </thead>
                <tbody>
                  {llms.map((llm) => (
                    <tr key={llm.id} className="border-b border-line last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
                      <td className="font-medium">
                        {canManage ? <button type="button" className="hover:text-accent" onClick={() => setParams({ edit: llm.id })}>{llm.name}</button> : llm.name}
                      </td>
                      <td className="font-mono text-[12px] text-muted">{llm.model}</td>
                      <td className="font-mono text-[12px] text-muted">
                        {llm.reasoning_effort ?? 'default'}
                        {llm.api === 'responses' && <span className="ml-1.5 rounded border border-line px-1 text-[10px]">responses</span>}
                      </td>
                      <td className="font-mono tabular text-muted">{llm.temperature ?? 'default'}</td>
                      <td className="font-mono tabular text-muted">{llm.max_completion_tokens ?? 'default'}</td>
                      {showPrices && <td><PriceMatchBadge model={llm.model} prices={prices} /></td>}
                      {canManage && (
                        <td className="text-right whitespace-nowrap">
                          <IconButton label={`Edit ${llm.name}`} icon={<Pencil className="size-3.5" />} onClick={() => setParams({ edit: llm.id })} />
                          <ConfirmButton icon={<Trash2 className="size-3.5" />} onConfirm={() => void remove(llm)}>Delete</ConfirmButton>
                        </td>
                      )}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        }
      </AsyncView>
      {canManage && (creating || editing) && res.data && (
        <LlmForm key={editing?.id ?? 'new'} llm={creating ? null : editing} prices={res.data.prices} showPrices={showPrices} onClose={closeForm} onSaved={onSaved} />
      )}
    </>
  )
}
