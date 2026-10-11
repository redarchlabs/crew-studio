import { CircleCheck, CircleX, RefreshCw, TriangleAlert } from 'lucide-react'
import type { CrewValidation } from '../../api/types'
import { IconButton } from '../../components/Button'
import { Panel } from '../../components/PageHeader'
import { errorMessage } from '../../api/client'

type ValidationPanelProps = {
  validation: CrewValidation | undefined
  loading: boolean
  error: unknown
  onRefresh: () => void
  isNew: boolean
  dirty: boolean
  draftWarnings: string[]
}

/** Server validation (GET /crews/{id}/validate) plus live checks on the unsaved draft. */
export function ValidationPanel({ validation, loading, error, onRefresh, isNew, dirty, draftWarnings }: ValidationPanelProps) {
  return (
    <Panel
      title="Validation"
      actions={!isNew && <IconButton label="Re-validate" icon={<RefreshCw className={`size-3.5 ${loading ? 'animate-spin' : ''}`} />} onClick={onRefresh} />}
      bodyClassName="p-3 flex flex-col gap-2"
    >
      {isNew ? (
        <p className="text-[12px] text-faint">Save the crew to run server validation.</p>
      ) : error ? (
        <p className="text-[12px] text-danger">Could not validate: {errorMessage(error)}</p>
      ) : validation ? (
        validation.valid ? (
          <div className="flex items-center gap-2 text-ok"><CircleCheck className="size-4" /> Ready to run{dirty && <span className="text-faint">(saved version)</span>}</div>
        ) : (
          <ul className="flex flex-col gap-1">
            {validation.errors.map((e) => (
              <li key={e} className="flex items-start gap-2 text-[12.5px]"><CircleX className="mt-0.5 size-3.5 shrink-0 text-danger" />{e}</li>
            ))}
          </ul>
        )
      ) : (
        <p className="text-[12px] text-faint">Checking…</p>
      )}
      {!isNew && !error && (validation?.warnings ?? []).length > 0 && (
        <ul className="flex flex-col gap-1" data-testid="validation-warnings">
          {validation!.warnings!.map((w) => (
            <li key={w} className="flex items-start gap-2 text-[12px] text-warn"><TriangleAlert className="mt-0.5 size-3.5 shrink-0" />{w}</li>
          ))}
        </ul>
      )}
      {draftWarnings.length > 0 && (
        <div className="border-t border-line pt-2">
          <div className="eyebrow mb-1">Draft checks</div>
          <ul className="flex flex-col gap-1">
            {draftWarnings.map((w) => (
              <li key={w} className="flex items-start gap-2 text-[12px] text-muted"><TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-warn" />{w}</li>
            ))}
          </ul>
        </div>
      )}
    </Panel>
  )
}
