import { useState } from 'react'
import { DollarSign, Plus } from 'lucide-react'
import { llmsApi, pricingApi } from '../../api/endpoints'
import type { LLM, ModelPrice, ModelPriceInput } from '../../api/types'
import { Button } from '../../components/Button'
import { FormErrors } from '../../components/Field'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { HistoryLink } from '../../components/misc'
import { ReadOnlyNote } from '../../components/ReadOnlyNote'
import { useAuth } from '../../lib/auth'
import { parseFieldErrors, type FieldErrors, NO_ERRORS } from '../../lib/fieldErrors'
import { notifyError, notifySuccess } from '../../lib/notify'
import { matchPrice } from '../../lib/pricing'
import { useAsync } from '../../lib/useAsync'
import { EMPTY_PRICE, PRICE_FIELDS, PriceEditRow, PriceViewRow, toPriceInput, validatePrice } from './PriceRow'

const NEW_ROW = '__new__'

function sortPrices(prices: readonly ModelPrice[]): ModelPrice[] {
  return [...prices].sort((a, b) => a.model.localeCompare(b.model))
}

function usersOf(price: ModelPrice, prices: readonly ModelPrice[], llms: readonly LLM[]): string[] {
  return llms.filter((l) => matchPrice(l.model, prices)?.price.id === price.id).map((l) => l.name)
}

export function PricingPage() {
  const { can } = useAuth()
  const canManage = can('pricing.manage')
  // "Used by" needs the LLM list, which only llms.view may read
  const showUsage = can('llms.view')
  const res = useAsync(async (signal) => {
    const [prices, llms] = await Promise.all([pricingApi.list(signal), showUsage ? llmsApi.list(signal) : Promise.resolve<LLM[]>([])])
    return { prices: sortPrices(prices), llms }
  }, [showUsage])
  const [editing, setEditing] = useState<string | null>(null)
  const [errors, setErrors] = useState<FieldErrors>(NO_ERRORS)
  const [saving, setSaving] = useState(false)

  const startEdit = (id: string) => {
    setErrors(NO_ERRORS)
    setEditing(id)
  }

  const save = async (id: string, value: ModelPriceInput) => {
    const local = validatePrice(value)
    if (Object.keys(local).length > 0) {
      setErrors({ fields: local, general: Object.entries(local).map(([k, m]) => `${k}: ${m}`) })
      return
    }
    setSaving(true)
    const body = { ...value, model: value.model.trim(), notes: value.notes.trim() }
    try {
      const saved = id === NEW_ROW ? await pricingApi.create(body) : await pricingApi.update(id, body)
      res.setData((prev) => prev && { ...prev, prices: sortPrices([...prev.prices.filter((p) => p.id !== saved.id), saved]) })
      notifySuccess(id === NEW_ROW ? `Added price for ${saved.model}` : `Updated ${saved.model}`)
      setEditing(null)
      setErrors(NO_ERRORS)
    } catch (err) {
      setErrors(parseFieldErrors(err, PRICE_FIELDS))
      notifyError('Could not save price', err)
    } finally {
      setSaving(false)
    }
  }

  const remove = async (price: ModelPrice) => {
    try {
      await pricingApi.remove(price.id)
      res.setData((prev) => prev && { ...prev, prices: prev.prices.filter((p) => p.id !== price.id) })
      notifySuccess(`Deleted ${price.model}`)
    } catch (err) {
      notifyError(`Could not delete ${price.model}`, err)
    }
  }

  const editRow = (id: string, initial: ModelPriceInput) => (
    <PriceEditRow key={id} initial={initial} errors={errors.fields} saving={saving} onSave={(v) => void save(id, v)} onCancel={() => setEditing(null)} />
  )

  return (
    <>
      <PageHeader
        eyebrow="Configure"
        title="Model pricing"
        description="USD per 1M tokens. Prices drive every cost estimate and budget stop. LLMs match by exact model id first, then by the id without its provider prefix."
        actions={
          <>
            {can('history.view') && <HistoryLink type="pricing" />}
            {canManage && <Button variant="primary" icon={<Plus className="size-4" />} disabled={editing === NEW_ROW} onClick={() => startEdit(NEW_ROW)}>Add price</Button>}
          </>
        }
      />
      {!canManage && <ReadOnlyNote perm="pricing.manage" />}
      <AsyncView {...res}>
        {({ prices, llms }) => (
          <div className="flex flex-col gap-3">
            <FormErrors messages={errors.general} />
            {prices.length === 0 && editing !== NEW_ROW ? (
              <EmptyState
                icon={<DollarSign className="size-6" />}
                title="No prices yet"
                hint="Without prices, run costs show as $0 and budgets cannot stop runaway spend."
                action={canManage ? <Button variant="primary" onClick={() => startEdit(NEW_ROW)}>Add the first price</Button> : undefined}
              />
            ) : (
              <div className="overflow-x-auto rounded-lg border border-line bg-panel">
                <table className="w-full min-w-[860px] text-left">
                  <thead className="border-b border-line">
                    <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                      <th>Model</th>
                      <th className="text-right">Input / 1M</th>
                      <th className="text-right">Cached in / 1M</th>
                      <th className="text-right" title="Tokens written to the prompt cache; Anthropic charges 1.25× input">Cache write / 1M</th>
                      <th className="text-right">Output / 1M</th>
                      <th>Notes</th>
                      {showUsage && <th>Used by</th>}
                      {canManage && <th className="w-32" />}
                    </tr>
                  </thead>
                  <tbody>
                    {canManage && editing === NEW_ROW && editRow(NEW_ROW, EMPTY_PRICE)}
                    {prices.map((p) =>
                      canManage && editing === p.id ? (
                        editRow(p.id, toPriceInput(p))
                      ) : (
                        <PriceViewRow
                          key={p.id}
                          price={p}
                          usedBy={showUsage ? usersOf(p, prices, llms) : null}
                          onEdit={canManage ? () => startEdit(p.id) : undefined}
                          onDelete={canManage ? () => void remove(p) : undefined}
                        />
                      ),
                    )}
                  </tbody>
                </table>
              </div>
            )}
            {canManage && <p className="text-[12px] text-faint">Tip: double-click a row to edit; Enter saves, Escape cancels.</p>}
          </div>
        )}
      </AsyncView>
    </>
  )
}
